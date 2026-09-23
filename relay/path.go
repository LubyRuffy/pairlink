package relay

import (
	"bytes"
	"context"
	"encoding/hex"
	"time"

	"github.com/LubyRuffy/pairlink/protocol"
)

// PathFreshness is how long a path announcement stays visible. Clients
// re-announce on every keepalive (default 15s) and whenever the data plane
// changes. Three missed keepalives expire the observation; callers then
// show the pair as offline instead of guessing direct.
const PathFreshness = 45 * time.Second

type pathObs struct {
	path string
	at   time.Time
}

func (h *Hub) clock() time.Time {
	if h == nil {
		return time.Now()
	}
	h.mu.Lock()
	now := h.now
	h.mu.Unlock()
	if now == nil {
		return time.Now()
	}
	return now()
}

func pathKey(a, b []byte) string {
	left, right := a, b
	if bytes.Compare(a, b) > 0 {
		left, right = b, a
	}
	return hex.EncodeToString(left) + "/" + hex.EncodeToString(right)
}

func (h *Hub) observePath(ctx context.Context, src, dst, payload []byte) {
	if h == nil || h.Store == nil || len(payload) != 1 {
		return
	}
	path, ok := protocol.PathFromCode(payload[0])
	if !ok {
		return
	}
	if _, err := h.Store.BindingByPeers(ctx, src, dst); err != nil {
		if _, err2 := h.Store.BindingByPeers(ctx, dst, src); err2 != nil {
			return
		}
	}
	at := h.clock()
	key := pathKey(src, dst)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.paths == nil {
		h.paths = map[string]pathObs{}
	}
	for existing, obs := range h.paths {
		if at.Sub(obs.at) > PathFreshness {
			delete(h.paths, existing)
		}
	}
	h.paths[key] = pathObs{path: path, at: at}
}

// LinkPath returns relay or direct when this process has a fresh announcement
// for the bound pair. An empty string means there is no live observation.
func (h *Hub) LinkPath(a, b []byte) string {
	if h == nil || len(a) == 0 || len(b) == 0 {
		return ""
	}
	h.mu.Lock()
	obs, ok := h.paths[pathKey(a, b)]
	h.mu.Unlock()
	if !ok || h.clock().Sub(obs.at) > PathFreshness {
		return ""
	}
	return obs.path
}
