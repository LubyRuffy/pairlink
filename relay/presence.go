package relay

import (
	"bytes"
	"context"
	"encoding/hex"

	"github.com/LubyRuffy/pairlink/protocol"
)

func hostPresenceKey(pub []byte) string { return "h:" + hex.EncodeToString(pub) }

func devicePresenceKey(bindingID string) string { return "d:" + bindingID }

func (h *Hub) connected(key string) (*peerConn, bool) {
	if h == nil || key == "" {
		return nil, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	pc, ok := h.conns[key]
	if !ok || pc == nil || pc.ws == nil {
		return nil, false
	}
	return pc, true
}

func (h *Hub) hostConnected(pub []byte) bool {
	if len(pub) != protocol.KeySize {
		return false
	}
	_, ok := h.connected(hostPresenceKey(pub))
	return ok
}

func (h *Hub) bindingConnected(id string) bool {
	_, ok := h.connected(devicePresenceKey(id))
	return ok
}

// lookup finds the host socket for a public key, or any device socket using it.
func (h *Hub) lookup(pub []byte) (*peerConn, bool) {
	if pc, ok := h.connected(hostPresenceKey(pub)); ok {
		return pc, true
	}
	if h == nil {
		return nil, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, pc := range h.conns {
		if pc != nil && pc.device && bytes.Equal(pc.pub, pub) {
			return pc, true
		}
	}
	return nil, false
}

func (h *Hub) dropBinding(id string) {
	pc, ok := h.connected(devicePresenceKey(id))
	if !ok {
		return
	}
	_ = pc.ws.Close()
}

// boundPair reports whether this socket may speak for dst.
// A device socket only speaks for the binding that authenticated it.
func (h *Hub) boundPair(ctx context.Context, from *peerConn, dst []byte) bool {
	if from == nil || h == nil || h.Store == nil || len(dst) != protocol.KeySize || len(from.pub) != protocol.KeySize {
		return false
	}
	if from.device {
		b, err := h.Store.BindingByPeers(ctx, dst, from.pub)
		return err == nil && b.ID == from.bindingID
	}
	_, err := h.Store.BindingByPeers(ctx, from.pub, dst)
	return err == nil
}

func (h *Hub) relayTarget(ctx context.Context, from *peerConn, dst []byte) (*peerConn, bool) {
	if !h.boundPair(ctx, from, dst) {
		return nil, false
	}
	if from.device {
		return h.connected(hostPresenceKey(dst))
	}
	b, err := h.Store.BindingByPeers(ctx, from.pub, dst)
	if err != nil {
		return nil, false
	}
	return h.connected(devicePresenceKey(b.ID))
}
