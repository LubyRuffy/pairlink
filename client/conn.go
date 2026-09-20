// Package client is the pairlink endpoint: host or device, relay-first magicsock.
package client

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/gorilla/websocket"
)

type Config struct {
	HubURL     string
	Identity   *crypto.Identity
	Token      string
	DisableUDP bool
	HTTPClient *http.Client
	// KeepAlive is how often we write a dummy relay frame so the hub does
	// not idle-drop the host WebSocket. Zero means 15s. Must be less than
	// the hub's quiet-WS deadline (60s) and typical nginx read timeouts.
	KeepAlive time.Duration
}

type Conn struct {
	cfg   Config
	http  *http.Client
	ws    *websocket.Conn
	wsMu  sync.Mutex
	udp   *net.UDPConn
	stun  string
	local []string

	mu        sync.Mutex
	links     map[string]*Link // peer pub hex
	pending   map[string]*crypto.Handshake
	onLink    func(*Link)
	closed    chan struct{}
	closeOnce sync.Once
}

type Link struct {
	ID       string
	PeerPub  []byte
	sessID   [protocol.SessionIDSize]byte
	crypt    *crypto.Session
	inbox    chan []byte
	path     atomic.Value // string
	peerUDP  atomic.Value // *net.UDPAddr
	lastPong atomic.Int64
	conn     *Conn
}

func (l *Link) Path() string {
	if v, ok := l.path.Load().(string); ok && v != "" {
		return v
	}
	return protocol.PathRelay
}

func (l *Link) SessionID() string { return hex.EncodeToString(l.sessID[:]) }

func (l *Link) Recv() <-chan []byte { return l.inbox }

func Dial(ctx context.Context, cfg Config) (*Conn, error) {
	if cfg.Identity == nil || strings.TrimSpace(cfg.HubURL) == "" || strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("client: incomplete config")
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	c := &Conn{
		cfg:     cfg,
		http:    hc,
		links:   map[string]*Link{},
		pending: map[string]*crypto.Handshake{},
		closed:  make(chan struct{}),
	}
	if err := c.connectWS(ctx); err != nil {
		return nil, err
	}
	if !cfg.DisableUDP {
		if err := c.bindUDP(); err != nil {
			_ = c.Close()
			return nil, err
		}
		go c.readUDP()
		go c.punchLoop()
	}
	go c.readWS()
	go c.keepAlive()
	return c, nil
}

func (c *Conn) OnLink(fn func(*Link)) {
	c.mu.Lock()
	c.onLink = fn
	c.mu.Unlock()
}

func (c *Conn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	c.wsMu.Lock()
	if c.ws != nil {
		_ = c.ws.Close()
		c.ws = nil
	}
	c.wsMu.Unlock()
	if c.udp != nil {
		_ = c.udp.Close()
	}
	return nil
}

func (c *Conn) KillUDP() {
	if c.udp != nil {
		_ = c.udp.Close()
		c.udp = nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, l := range c.links {
		l.path.Store(protocol.PathRelay)
		l.peerUDP.Store((*net.UDPAddr)(nil))
	}
}

func httpToWS(h string) string {
	u, err := url.Parse(h)
	if err != nil {
		return h
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/pairlink/v1/ws"
	u.RawQuery = ""
	return u.String()
}

func originURL(h string) string {
	return strings.TrimRight(h, "/")
}

func (c *Conn) getInfo(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, originURL(c.cfg.HubURL)+"/pairlink/v1/info", nil)
	if err != nil {
		return "", err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var body struct {
		Stun string `json:"stun"`
	}
	_ = json.NewDecoder(res.Body).Decode(&body)
	return body.Stun, nil
}

func (c *Conn) bindUDP() error {
	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return err
	}
	c.udp = pc
	port := pc.LocalAddr().(*net.UDPAddr).Port
	c.local = localEndpoints(port)
	if c.stun != "" {
		if obs, err := stunOnce(pc, c.stun, c.cfg.Identity.Public()); err == nil && obs != "" {
			c.local = append(c.local, obs)
		}
	}
	return nil
}

func localEndpoints(port int) []string {
	var out []string
	out = append(out, net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port)))
	ifs, _ := net.InterfaceAddrs()
	for _, a := range ifs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.To4() == nil || ipn.IP.IsLoopback() {
			continue
		}
		out = append(out, net.JoinHostPort(ipn.IP.String(), fmt.Sprintf("%d", port)))
	}
	return out
}

func stunOnce(pc *net.UDPConn, stun string, pub []byte) (string, error) {
	addr, err := net.ResolveUDPAddr("udp", stun)
	if err != nil {
		return "", err
	}
	msg := append([]byte(protocol.Magic), pub...)
	if _, err := pc.WriteToUDP(msg, addr); err != nil {
		return "", err
	}
	_ = pc.SetReadDeadline(time.Now().Add(800 * time.Millisecond))
	buf := make([]byte, 256)
	n, _, err := pc.ReadFromUDP(buf)
	_ = pc.SetReadDeadline(time.Time{})
	if err != nil {
		return "", err
	}
	if n < 4 || string(buf[:4]) != protocol.Magic {
		return "", fmt.Errorf("bad stun")
	}
	return string(buf[4:n]), nil
}

func (c *Conn) newFrame(typ byte, dst []byte, payload []byte) protocol.Frame {
	var fr protocol.Frame
	fr.Type = typ
	copy(fr.Dst[:], dst)
	copy(fr.Src[:], c.cfg.Identity.Public())
	fr.Payload = payload
	c.mu.Lock()
	if l := c.links[hex.EncodeToString(dst)]; l != nil {
		fr.SessionID = l.sessID
	}
	c.mu.Unlock()
	return fr
}

func (c *Conn) readUDP() {
	buf := make([]byte, 70<<10)
	for {
		if c.udp == nil {
			return
		}
		n, addr, err := c.udp.ReadFromUDP(buf)
		if err != nil {
			return
		}
		fr, err := protocol.UnmarshalFrame(buf[:n])
		if err != nil {
			continue
		}
		c.handleFrame(fr, protocol.PathDirect, addr)
	}
}

func (c *Conn) handleFrame(fr protocol.Frame, via string, from *net.UDPAddr) {
	switch fr.Type {
	case protocol.TypeObserved:
		obs, err := protocol.UnmarshalObserved(fr.Payload)
		if err != nil {
			return
		}
		if obs.Addr != "" {
			c.mu.Lock()
			c.local = appendUnique(c.local, obs.Addr)
			c.mu.Unlock()
		}
	case protocol.TypeHandshake:
		c.onHandshake(fr)
	case protocol.TypeData:
		c.onData(fr)
	case protocol.TypeDisco:
		c.onDisco(fr)
	case protocol.TypePunchPing:
		if from != nil {
			c.replyPong(fr, from)
		}
	case protocol.TypePunchPong:
		c.onPong(fr, from)
	}
	_ = via
}

func (c *Conn) onHandshake(fr protocol.Frame) {
	peer := append([]byte(nil), fr.Src[:]...)
	key := hex.EncodeToString(peer)
	c.mu.Lock()
	hs, waiting := c.pending[key]
	c.mu.Unlock()
	if waiting {
		sess, err := hs.Finish(fr.Payload)
		if err != nil {
			return
		}
		c.install(peer, fr.SessionID, sess)
		return
	}
	sess, reply, err := crypto.Respond(c.cfg.Identity, peer, fr.Payload)
	if err != nil {
		return
	}
	c.install(peer, fr.SessionID, sess)
	out := c.newFrame(protocol.TypeHandshake, peer, reply)
	out.SessionID = fr.SessionID
	_ = c.writeWS(out)
}

func (c *Conn) install(peer []byte, sid [protocol.SessionIDSize]byte, sess *crypto.Session) *Link {
	key := hex.EncodeToString(peer)
	l := &Link{
		ID:      hex.EncodeToString(sid[:]),
		PeerPub: append([]byte(nil), peer...),
		sessID:  sid,
		crypt:   sess,
		inbox:   make(chan []byte, 32),
		conn:    c,
	}
	l.path.Store(protocol.PathRelay)
	c.mu.Lock()
	c.links[key] = l
	delete(c.pending, key)
	cb := c.onLink
	c.mu.Unlock()
	_ = c.sendDisco(l)
	if cb != nil {
		cb(l)
	}
	return l
}

func (c *Conn) onData(fr protocol.Frame) {
	l := c.linkByPeer(fr.Src[:])
	if l == nil || l.crypt == nil {
		return
	}
	plain, err := l.crypt.Open(fr.Payload)
	if err != nil {
		return
	}
	select {
	case l.inbox <- append([]byte(nil), plain...):
	default:
		// slow subscriber: drop newest app payload, keep session
	}
}

func (c *Conn) onDisco(fr protocol.Frame) {
	l := c.linkByPeer(fr.Src[:])
	if l == nil || l.crypt == nil {
		return
	}
	plain, err := l.crypt.Open(fr.Payload)
	if err != nil {
		return
	}
	d, err := protocol.UnmarshalDisco(plain)
	if err != nil {
		return
	}
	if c.udp == nil {
		return
	}
	for _, ep := range d.Endpoints {
		addr, err := net.ResolveUDPAddr("udp", ep)
		if err != nil {
			continue
		}
		c.sendPunch(l, addr, protocol.TypePunchPing)
	}
}

func (c *Conn) replyPong(fr protocol.Frame, from *net.UDPAddr) {
	l := c.linkByPeer(fr.Src[:])
	if l == nil {
		return
	}
	l.peerUDP.Store(from)
	c.sendPunch(l, from, protocol.TypePunchPong)
}

func (c *Conn) onPong(fr protocol.Frame, from *net.UDPAddr) {
	l := c.linkByPeer(fr.Src[:])
	if l == nil {
		return
	}
	if from != nil {
		l.peerUDP.Store(from)
	}
	l.lastPong.Store(time.Now().UnixNano())
	l.path.Store(protocol.PathDirect)
}

func (c *Conn) sendPunch(l *Link, addr *net.UDPAddr, typ byte) {
	if c.udp == nil || addr == nil {
		return
	}
	fr := c.newFrame(typ, l.PeerPub, nil)
	fr.SessionID = l.sessID
	raw, err := fr.Marshal()
	if err != nil {
		return
	}
	_, _ = c.udp.WriteToUDP(raw, addr)
}

func (c *Conn) punchLoop() {
	t := time.NewTicker(400 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-c.closed:
			return
		case <-t.C:
			c.mu.Lock()
			links := make([]*Link, 0, len(c.links))
			for _, l := range c.links {
				links = append(links, l)
			}
			c.mu.Unlock()
			now := time.Now()
			for _, l := range links {
				if addr, _ := l.peerUDP.Load().(*net.UDPAddr); addr != nil {
					c.sendPunch(l, addr, protocol.TypePunchPing)
				}
				last := time.Unix(0, l.lastPong.Load())
				if l.Path() == protocol.PathDirect && (last.IsZero() || now.Sub(last) > 2*time.Second) {
					l.path.Store(protocol.PathRelay)
				}
			}
		}
	}
}

func (c *Conn) sendDisco(l *Link) error {
	c.mu.Lock()
	eps := append([]string(nil), c.local...)
	c.mu.Unlock()
	body, err := protocol.Disco{Endpoints: eps, CallMe: true}.Marshal()
	if err != nil {
		return err
	}
	sealed, err := l.crypt.Seal(body)
	if err != nil {
		return err
	}
	fr := c.newFrame(protocol.TypeDisco, l.PeerPub, sealed)
	fr.SessionID = l.sessID
	return c.writeWS(fr)
}

func (c *Conn) linkByPeer(pub []byte) *Link {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.links[hex.EncodeToString(pub)]
}

func (l *Link) Send(plain []byte) error {
	if l.crypt == nil {
		return fmt.Errorf("client: no session")
	}
	sealed, err := l.crypt.Seal(plain)
	if err != nil {
		return err
	}
	fr := l.conn.newFrame(protocol.TypeData, l.PeerPub, sealed)
	fr.SessionID = l.sessID
	if l.Path() == protocol.PathDirect && l.conn.udp != nil {
		if addr, _ := l.peerUDP.Load().(*net.UDPAddr); addr != nil {
			raw, err := fr.Marshal()
			if err != nil {
				return err
			}
			_, err = l.conn.udp.WriteToUDP(raw, addr)
			return err
		}
	}
	return l.conn.writeWS(fr)
}

func appendUnique(in []string, v string) []string {
	for _, x := range in {
		if x == v {
			return in
		}
	}
	return append(in, v)
}

func (c *Conn) json(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, originURL(c.cfg.HubURL)+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return fmt.Errorf("client: %s: %s", res.Status, raw)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// Register publishes this host's public key against the Host Token.
func (c *Conn) Register(ctx context.Context) error {
	return c.json(ctx, http.MethodPost, "/pairlink/v1/hosts", map[string]string{
		"pub":   crypto.PublicBase64(c.cfg.Identity.Public()),
		"token": c.cfg.Token,
	}, nil)
}

func (c *Conn) CreateOffer(ctx context.Context) (uri, pairingID string, err error) {
	var resp struct {
		PairingID string `json:"pairing_id"`
		Code      string `json:"code"`
	}
	if err = c.json(ctx, http.MethodPost, "/pairlink/v1/pairings", nil, &resp); err != nil {
		return "", "", err
	}
	c.mu.Lock()
	lan := append([]string(nil), c.local...)
	c.mu.Unlock()
	uri, err = protocol.Encode(protocol.Offer{
		HubURL:  originURL(c.cfg.HubURL),
		Code:    resp.Code,
		HostPub: c.cfg.Identity.Public(),
		LAN:     lan,
	})
	return uri, resp.PairingID, err
}

func (c *Conn) Redeem(ctx context.Context, offer protocol.Offer) (hostPub []byte, sessionID []byte, ticket string, err error) {
	var resp struct {
		Ticket    string `json:"ticket"`
		HostPub   string `json:"host_pub"`
		SessionID string `json:"session_id"`
	}
	if err = c.json(ctx, http.MethodPost, "/pairlink/v1/pairings/redeem", map[string]string{
		"code":       offer.Code,
		"device_pub": crypto.PublicBase64(c.cfg.Identity.Public()),
	}, &resp); err != nil {
		return nil, nil, "", err
	}
	hostPub, err = crypto.ParsePublic(resp.HostPub)
	if err != nil {
		return nil, nil, "", err
	}
	sessionID, err = hex.DecodeString(resp.SessionID)
	return hostPub, sessionID, resp.Ticket, err
}

// OpenInitiator starts the encrypted session toward a bound host (device side).
func (c *Conn) OpenInitiator(ctx context.Context, hostPub, sessionID []byte) (*Link, error) {
	hs, msg, err := crypto.Initiate(c.cfg.Identity, hostPub)
	if err != nil {
		return nil, err
	}
	key := hex.EncodeToString(hostPub)
	c.mu.Lock()
	c.pending[key] = hs
	c.mu.Unlock()
	fr := c.newFrame(protocol.TypeHandshake, hostPub, msg)
	if len(sessionID) == protocol.SessionIDSize {
		copy(fr.SessionID[:], sessionID)
	}
	if err := c.writeWS(fr); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
			c.mu.Lock()
			l := c.links[key]
			c.mu.Unlock()
			if l != nil {
				return l, nil
			}
		}
	}
	return nil, fmt.Errorf("client: handshake timeout")
}

func (c *Conn) WaitLink(ctx context.Context, peer []byte) (*Link, error) {
	key := hex.EncodeToString(peer)
	for {
		c.mu.Lock()
		l := c.links[key]
		c.mu.Unlock()
		if l != nil {
			return l, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (c *Conn) Trace(ctx context.Context, ref string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, originURL(c.cfg.HubURL)+"/pairlink/v1/trace/"+url.PathEscape(ref), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return io.ReadAll(io.LimitReader(res.Body, 1<<20))
}
