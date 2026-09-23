package relay

import (
	"context"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/LubyRuffy/pairlink/store"
)

//go:embed admin.html
var adminHTML []byte

// SetAdminToken enables the management HTTP API. The raw token is hashed and
// dropped. An empty token disables the admin routes.
func (h *Hub) SetAdminToken(raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		h.adminHash = nil
		return
	}
	h.adminHash = store.HashSecret(raw)
}

func (h *Hub) adminOn() bool { return len(h.adminHash) > 0 }

func (h *Hub) authAdmin(r *http.Request) bool {
	if !h.adminOn() {
		return false
	}
	tok := bearer(r)
	if tok == "" || len(tok) > 256 {
		return false
	}
	sum := store.HashSecret(tok)
	return subtle.ConstantTimeCompare(sum, h.adminHash) == 1
}

func (h *Hub) rejectAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !h.adminOn() {
		httpError(w, http.StatusNotFound, "not found")
		return true
	}
	if !h.authAdmin(r) {
		httpError(w, http.StatusUnauthorized, "unauthorized")
		return true
	}
	return false
}

func (h *Hub) peerOnline(pub []byte) bool {
	if len(pub) != protocol.KeySize {
		return false
	}
	_, ok := h.lookup(pub)
	return ok
}

// PeerOnline reports whether this process still has that public key's
// websocket. It is presence, not the data-plane path.
func (h *Hub) PeerOnline(pub []byte) bool { return h.peerOnline(pub) }

type adminHost struct {
	FP         string `json:"fp,omitempty"`
	Name       string `json:"name"`
	Online     bool   `json:"online"`
	Registered bool   `json:"registered"`
	CreatedAt  string `json:"created_at"`
}

type adminBinding struct {
	ID          string `json:"id"`
	HostFP      string `json:"host_fp"`
	HostName    string `json:"host_name"`
	DeviceFP    string `json:"device_fp"`
	DeviceName  string `json:"device_name"`
	DeviceModel string `json:"device_model"`
	Online      bool   `json:"online"`
	Path        string `json:"path"`
	Revoked     bool   `json:"revoked"`
	CreatedAt   string `json:"created_at"`
	SessionID   string `json:"session_id"`
}

func (h *Hub) handleAdminPage(w http.ResponseWriter, r *http.Request) {
	if !h.adminOn() {
		httpError(w, http.StatusNotFound, "not found")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(adminHTML)
}

func (h *Hub) handleAdminSnapshot(w http.ResponseWriter, r *http.Request) {
	if h.rejectAdmin(w, r) {
		return
	}
	snap, err := h.adminSnapshot(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, "store")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, snap)
}

func (h *Hub) handleAdminIssueHost(w http.ResponseWriter, r *http.Request) {
	if h.rejectAdmin(w, r) {
		return
	}
	raw, err := IssueHostToken(r.Context(), h.Store)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "store")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"token": raw})
}

func (h *Hub) handleAdminRevoke(w http.ResponseWriter, r *http.Request) {
	if h.rejectAdmin(w, r) {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		httpError(w, http.StatusBadRequest, "missing id")
		return
	}
	if err := h.Store.RevokeBinding(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, "not found")
			return
		}
		httpError(w, http.StatusInternalServerError, "store")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Hub) handleAdminTrace(w http.ResponseWriter, r *http.Request) {
	if h.rejectAdmin(w, r) {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		httpError(w, http.StatusBadRequest, "missing id")
		return
	}
	ev, err := h.Store.Trace(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "store")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": ev})
}

func (h *Hub) adminSnapshot(ctx context.Context) (map[string]any, error) {
	hosts, err := h.Store.ListHosts(ctx)
	if err != nil {
		return nil, err
	}
	bindings, err := h.Store.ListAllBindings(ctx)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	hostRows := make([]adminHost, 0, len(hosts))
	for _, host := range hosts {
		row := adminHost{Name: host.Name, CreatedAt: host.Created.UTC().Format(time.RFC3339)}
		if len(host.Pub) == protocol.KeySize {
			fp := crypto.Fingerprint(host.Pub)
			row.FP = fp
			row.Registered = true
			row.Online = h.peerOnline(host.Pub)
			names[hex.EncodeToString(host.Pub)] = host.Name
		}
		hostRows = append(hostRows, row)
	}
	bindRows := make([]adminBinding, 0, len(bindings))
	for _, b := range bindings {
		row := adminBinding{
			ID: b.ID, HostName: names[hex.EncodeToString(b.HostPub)],
			DeviceName: b.DeviceName, DeviceModel: b.DeviceModel,
			Online: h.peerOnline(b.DevicePub), Path: h.LinkPath(b.HostPub, b.DevicePub),
			Revoked: b.Revoked, CreatedAt: b.Created.UTC().Format(time.RFC3339),
			SessionID: hex.EncodeToString(b.SessionID),
		}
		if len(b.HostPub) == protocol.KeySize {
			row.HostFP = crypto.Fingerprint(b.HostPub)
		}
		if len(b.DevicePub) == protocol.KeySize {
			row.DeviceFP = crypto.Fingerprint(b.DevicePub)
		}
		bindRows = append(bindRows, row)
	}
	return map[string]any{"hosts": hostRows, "bindings": bindRows}, nil
}
