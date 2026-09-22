// Package relay is the DERP-style hub: pairing, presence, blind ciphertext forward.
package relay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/LubyRuffy/pairlink/store"
	"github.com/gorilla/websocket"
)

const (
	maxBindings = store.MaxBindingsPerHost
	pairingTTL  = 10 * time.Minute
	wsWait      = 60 * time.Second
)

// Hub is an embeddable pairlink server. It never decrypts TypeData payloads.
type Hub struct {
	Store   store.Store
	Log     *slog.Logger
	now     func() time.Time
	udpConn *net.UDPConn

	mu    sync.Mutex
	conns map[string]*peerConn // pubkey hex -> ws
	paths map[string]pathObs   // bound pair -> latest path announcement
	sniff [][]byte             // test-only intercepted payloads

	// Idle is the quiet-WebSocket deadline. Zero means 60s. Tests shorten it.
	Idle time.Duration
}

type peerConn struct {
	pub  []byte
	ws   *websocket.Conn
	wmu  sync.Mutex
	addr string
}

func (h *Hub) SetNow(now func() time.Time) {
	if now != nil {
		h.now = now
	}
}

func (h *Hub) wsIdle() time.Duration {
	if h.Idle > 0 {
		return h.Idle
	}
	return wsWait
}

// ClosePeers closes every upgraded relay socket. Shutdown does not: hijacked
// WebSockets outlive the HTTP server, and a graceful proxy reload keeps the
// upstream until the client stops writing. Callers that still serve HTTP use
// this to make both ends redial into the process that owns the peer table.
func (h *Hub) ClosePeers() {
	if h == nil {
		return
	}
	h.mu.Lock()
	peers := h.conns
	h.conns = map[string]*peerConn{}
	h.mu.Unlock()
	for _, pc := range peers {
		if pc == nil || pc.ws == nil {
			continue
		}
		_ = pc.ws.Close()
	}
}

func New(st store.Store) *Hub {
	if st == nil {
		st = store.NewMemory()
	}
	return &Hub{
		Store: st,
		Log:   slog.Default(),
		now:   time.Now,
		conns: map[string]*peerConn{},
	}
}

// SniffedPayloads is test-only: copies of forwarded TypeData payloads so a
// test can prove they do not contain plaintext and cannot be opened without keys.
func (h *Hub) SniffedPayloads() [][]byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([][]byte, len(h.sniff))
	for i, p := range h.sniff {
		out[i] = append([]byte(nil), p...)
	}
	return out
}

func (h *Hub) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /pairlink/v1/hosts", h.handleRegister)
	mux.HandleFunc("POST /pairlink/v1/pairings", h.handleCreatePairing)
	mux.HandleFunc("POST /pairlink/v1/pairings/redeem", h.handleRedeem)
	mux.HandleFunc("GET /pairlink/v1/info", h.handleInfo)
	mux.HandleFunc("GET /pairlink/v1/ws", h.handleWS)
	mux.HandleFunc("GET /pairlink/v1/trace/", h.handleTrace)
	mux.HandleFunc("POST /pairlink/v1/bindings/{id}/revoke", h.handleRevoke)
	mux.HandleFunc("GET /pairlink/v1/bindings", h.handleListBindings)
	return withBrowserCORS(mux)
}

// Capacitor / browser redeem uses fetch() from a WebView origin (https://localhost
// or capacitor://localhost). Chromium sends OPTIONS first; a 405 here is
// "Failed to fetch" on the phone, not a pairing bug.
func withBrowserCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Hub) SetUDP(conn *net.UDPConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.udpConn = conn
}

func (h *Hub) udp() *net.UDPConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.udpConn
}

func (h *Hub) handleInfo(w http.ResponseWriter, r *http.Request) {
	stun := ""
	if u := h.udp(); u != nil {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		_, port, _ := net.SplitHostPort(u.LocalAddr().String())
		if host != "" && port != "" {
			stun = net.JoinHostPort(host, port)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"stun": stun})
}

func (h *Hub) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Pub   string `json:"pub"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	pub, err := crypto.ParsePublic(req.Pub)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad pub")
		return
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		httpError(w, http.StatusUnauthorized, "token required")
		return
	}
	host, err := h.Store.HostByTokenHash(r.Context(), store.HashSecret(token))
	if err != nil {
		httpError(w, http.StatusUnauthorized, "unknown token")
		return
	}
	host.Pub = pub
	if err := h.Store.PutHost(r.Context(), host); err != nil {
		httpError(w, http.StatusInternalServerError, "store")
		return
	}
	_ = h.Store.AppendTrace(r.Context(), store.TraceEvent{Ref: crypto.Fingerprint(pub), Kind: "register", PeerFP: crypto.Fingerprint(pub)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "fp": crypto.Fingerprint(pub)})
}

func (h *Hub) handleCreatePairing(w http.ResponseWriter, r *http.Request) {
	host, err := h.authHost(r)
	if err != nil {
		httpError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if len(host.Pub) == 0 {
		httpError(w, http.StatusConflict, "host must register a public key first")
		return
	}
	code, err := randomCode(10)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "rng")
		return
	}
	sid, err := randomID(protocol.SessionIDSize)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "rng")
		return
	}
	id, err := randomHex(16)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "rng")
		return
	}
	p := store.Pairing{
		ID:        id,
		HostPub:   host.Pub,
		CodeHash:  store.HashSecret(code),
		Expires:   h.now().Add(pairingTTL),
		SessionID: sid,
	}
	if err := h.Store.PutPairing(r.Context(), p); err != nil {
		httpError(w, http.StatusInternalServerError, "store")
		return
	}
	_ = h.Store.AppendTrace(r.Context(), store.TraceEvent{Ref: id, Kind: "pair", PeerFP: crypto.Fingerprint(host.Pub), Note: "created"})
	writeJSON(w, http.StatusOK, map[string]any{
		"pairing_id": id,
		"code":       code,
		"expires_at": p.Expires.UTC().Format(time.RFC3339),
		"session_id": hex.EncodeToString(sid),
	})
}

func (h *Hub) handleRedeem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code      string `json:"code"`
		DevicePub string `json:"device_pub"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	dev, err := crypto.ParsePublic(req.DevicePub)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad pub")
		return
	}
	p, err := h.Store.ConsumePairing(r.Context(), store.HashSecret(strings.TrimSpace(req.Code)))
	if err != nil {
		httpError(w, http.StatusNotFound, "invalid or expired code")
		return
	}
	if _, ok := h.lookup(p.HostPub); !ok {
		httpError(w, http.StatusConflict, "host offline")
		return
	}
	n, err := h.Store.CountBindings(r.Context(), p.HostPub)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "store")
		return
	}
	if n >= maxBindings {
		httpError(w, http.StatusConflict, "binding limit")
		return
	}
	ticket, err := randomHex(32)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "rng")
		return
	}
	bid, err := randomHex(16)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "rng")
		return
	}
	b := store.Binding{
		ID:         bid,
		HostPub:    p.HostPub,
		DevicePub:  dev,
		TicketHash: store.HashSecret(ticket),
		Created:    h.now(),
		SessionID:  p.SessionID,
	}
	if err := h.Store.PutBinding(r.Context(), b); err != nil {
		httpError(w, http.StatusInternalServerError, "store")
		return
	}
	ref := hex.EncodeToString(p.SessionID)
	_ = h.Store.AppendTrace(r.Context(), store.TraceEvent{Ref: ref, Kind: "bind", PeerFP: crypto.Fingerprint(dev)})
	_ = h.Store.AppendTrace(r.Context(), store.TraceEvent{Ref: p.ID, Kind: "bind", PeerFP: crypto.Fingerprint(dev)})
	writeJSON(w, http.StatusOK, map[string]any{
		"binding_id": bid,
		"ticket":     ticket,
		"host_pub":   crypto.PublicBase64(p.HostPub),
		"session_id": ref,
	})
}

func (h *Hub) handleListBindings(w http.ResponseWriter, r *http.Request) {
	host, err := h.authHost(r)
	if err != nil {
		httpError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	list, err := h.Store.ListBindings(r.Context(), host.Pub)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "store")
		return
	}
	type row struct {
		ID        string `json:"id"`
		DeviceFP  string `json:"device_fp"`
		CreatedAt string `json:"created_at"`
		SessionID string `json:"session_id"`
	}
	out := make([]row, 0, len(list))
	for _, b := range list {
		out = append(out, row{
			ID: b.ID, DeviceFP: crypto.Fingerprint(b.DevicePub),
			CreatedAt: b.Created.UTC().Format(time.RFC3339),
			SessionID: hex.EncodeToString(b.SessionID),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"bindings": out})
}

func (h *Hub) handleRevoke(w http.ResponseWriter, r *http.Request) {
	host, err := h.authHost(r)
	if err != nil {
		httpError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := r.PathValue("id")
	list, err := h.Store.ListBindings(r.Context(), host.Pub)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "store")
		return
	}
	ok := false
	for _, b := range list {
		if b.ID == id {
			ok = true
			break
		}
	}
	if !ok {
		httpError(w, http.StatusNotFound, "not found")
		return
	}
	if err := h.Store.RevokeBinding(r.Context(), id); err != nil {
		httpError(w, http.StatusInternalServerError, "store")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Hub) handleTrace(w http.ResponseWriter, r *http.Request) {
	ref := strings.TrimPrefix(r.URL.Path, "/pairlink/v1/trace/")
	if ref == "" {
		httpError(w, http.StatusBadRequest, "missing id")
		return
	}
	// Trace is metadata-only; still require a host token so random internet
	// cannot enumerate sessions.
	if _, err := h.authHost(r); err != nil {
		httpError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	ev, err := h.Store.Trace(r.Context(), ref)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "store")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": ev})
}

func (h *Hub) authHost(r *http.Request) (store.Host, error) {
	tok := bearer(r)
	if tok == "" {
		return store.Host{}, errors.New("missing token")
	}
	return h.Store.HostByTokenHash(r.Context(), store.HashSecret(tok))
}

func bearer(r *http.Request) string {
	a := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(a), "bearer ") {
		return strings.TrimSpace(a[7:])
	}
	return ""
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true },
}

// ticketProtoPrefix lets a browser WebSocket prove the device ticket without
// an Authorization header (browsers cannot set that on WS) and without putting
// the secret in the URL (access logs).
const ticketProtoPrefix = "pairlink.ticket."

func (h *Hub) handleWS(w http.ResponseWriter, r *http.Request) {
	pub, hostPub, device, err := h.identifyWS(r)
	if err != nil {
		httpError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	u := upgrader
	if p := ticketProtocol(r); p != "" {
		u.Subprotocols = []string{p}
	}
	ws, err := u.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	// A device 101 with no host in this process is a black hole: frames are
	// dropped and the client keeps its local "online" bit. Close now so it redials.
	if device {
		if _, ok := h.lookup(hostPub); !ok {
			_ = ws.Close()
			return
		}
	}
	pc := &peerConn{pub: pub, ws: ws, addr: r.RemoteAddr}
	key := hex.EncodeToString(pub)
	h.mu.Lock()
	if old, ok := h.conns[key]; ok {
		_ = old.ws.Close()
	}
	h.conns[key] = pc
	h.mu.Unlock()
	_ = h.Store.AppendTrace(r.Context(), store.TraceEvent{Ref: crypto.Fingerprint(pub), Kind: "connect", PeerFP: crypto.Fingerprint(pub), Note: "ws"})
	h.sendObserved(pc, r.RemoteAddr, "tcp")
	defer func() {
		h.mu.Lock()
		if h.conns[key] == pc {
			delete(h.conns, key)
		}
		h.mu.Unlock()
		_ = ws.Close()
		_ = h.Store.AppendTrace(context.Background(), store.TraceEvent{Ref: crypto.Fingerprint(pub), Kind: "disconnect", PeerFP: crypto.Fingerprint(pub)})
	}()
	ws.SetReadLimit(80 << 10)
	ws.SetPingHandler(func(appData string) error {
		_ = ws.SetReadDeadline(time.Now().Add(h.wsIdle()))
		return ws.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(time.Second))
	})
	for {
		_ = ws.SetReadDeadline(time.Now().Add(h.wsIdle()))
		_, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		fr, err := protocol.UnmarshalFrame(data)
		if err != nil {
			continue
		}
		if !bytesEqual(fr.Src[:], pub) {
			continue
		}
		// Path is endpoint metadata. It is not forwarded and not sniffed:
		// the hub never treats an application payload as a path.
		if fr.Type == protocol.TypePath {
			h.observePath(r.Context(), pub, fr.Dst[:], fr.Payload)
			continue
		}
		h.forward(r.Context(), pc, fr, data)
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func ticketProtocol(r *http.Request) string {
	for _, p := range websocket.Subprotocols(r) {
		if strings.HasPrefix(p, ticketProtoPrefix) {
			return p
		}
	}
	return ""
}

func wsToken(r *http.Request) string {
	if tok := bearer(r); tok != "" {
		return tok
	}
	p := ticketProtocol(r)
	if p == "" {
		return ""
	}
	return strings.TrimPrefix(p, ticketProtoPrefix)
}

func (h *Hub) identifyWS(r *http.Request) (pub, hostPub []byte, device bool, err error) {
	tok := wsToken(r)
	if tok == "" {
		return nil, nil, false, errors.New("missing token")
	}
	if host, herr := h.Store.HostByTokenHash(r.Context(), store.HashSecret(tok)); herr == nil && len(host.Pub) > 0 {
		return host.Pub, nil, false, nil
	}
	b, err := h.Store.BindingByTicketHash(r.Context(), store.HashSecret(tok))
	if err != nil {
		return nil, nil, false, err
	}
	return b.DevicePub, append([]byte(nil), b.HostPub...), true, nil
}

func (h *Hub) lookup(pub []byte) (*peerConn, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	pc, ok := h.conns[hex.EncodeToString(pub)]
	return pc, ok
}

func (h *Hub) forward(ctx context.Context, from *peerConn, fr protocol.Frame, raw []byte) {
	dst := fr.Dst[:]
	if _, err := h.Store.BindingByPeers(ctx, from.pub, dst); err != nil {
		if _, err2 := h.Store.BindingByPeers(ctx, dst, from.pub); err2 != nil {
			return
		}
	}
	to, ok := h.lookup(dst)
	if !ok {
		return
	}
	sid := hex.EncodeToString(fr.SessionID[:])
	if fr.Type == protocol.TypeData {
		h.mu.Lock()
		h.sniff = append(h.sniff, append([]byte(nil), fr.Payload...))
		if len(h.sniff) > 64 {
			h.sniff = h.sniff[len(h.sniff)-64:]
		}
		h.mu.Unlock()
	}
	_ = h.Store.AppendTrace(ctx, store.TraceEvent{
		Ref: sid, Kind: "forward", PeerFP: crypto.Fingerprint(from.pub),
		Bytes: len(fr.Payload), Path: protocol.PathRelay, Note: fmt.Sprintf("type=%d", fr.Type),
	})
	to.wmu.Lock()
	defer to.wmu.Unlock()
	_ = to.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_ = to.ws.WriteMessage(websocket.BinaryMessage, raw)
}

func (h *Hub) sendObserved(pc *peerConn, addr, via string) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return
	}
	ip := net.ParseIP(host)
	if ip != nil {
		host = ip.String()
	}
	obs := protocol.Observed{Addr: net.JoinHostPort(host, port), Via: via}
	body, err := obs.Marshal()
	if err != nil {
		return
	}
	var fr protocol.Frame
	fr.Type = protocol.TypeObserved
	copy(fr.Dst[:], pc.pub)
	raw, err := fr.Marshal()
	if err != nil {
		return
	}
	_ = body
	// Observed payload is the JSON; put it in the frame.
	fr.Payload = body
	raw, err = fr.Marshal()
	if err != nil {
		return
	}
	pc.wmu.Lock()
	defer pc.wmu.Unlock()
	_ = pc.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_ = pc.ws.WriteMessage(websocket.BinaryMessage, raw)
}

// ServeUDP answers STUN-lite pings: client sends its public key, hub replies
// with the observed IP:port. No application bytes.
func (h *Hub) ServeUDP(ctx context.Context, conn *net.UDPConn) error {
	h.SetUDP(conn)
	buf := make([]byte, 2048)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}
		if n < 4 || string(buf[:4]) != protocol.Magic {
			continue
		}
		reply := []byte(protocol.Magic + addr.String())
		_, _ = conn.WriteToUDP(reply, addr)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func randomCode(n int) (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b), nil
}

func randomID(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	return b, err
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// IssueHostToken stores a new host with a one-time token. The raw token is
// returned once and never logged.
func IssueHostToken(ctx context.Context, st store.Store) (raw string, err error) {
	raw, err = randomHex(32)
	if err != nil {
		return "", err
	}
	err = st.PutHost(ctx, store.Host{TokenHash: store.HashSecret(raw), Created: time.Now().UTC()})
	return raw, err
}
