package relay

import (
	"context"

	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/LubyRuffy/pairlink/store"
)

// observeLabel stores a TypeLabel announcement from the authenticated socket.
// A host socket updates that host's name. A device socket updates device_name
// and device_model on bindings for that device public key. Empty sanitized
// fields do not clear a stored label. The frame is not forwarded.
func (h *Hub) observeLabel(ctx context.Context, pub []byte, device bool, payload []byte) {
	if h == nil || h.Store == nil || len(pub) != protocol.KeySize {
		return
	}
	name, model, ok := protocol.ParseLabel(payload)
	if !ok {
		return
	}
	if device {
		if name == "" && model == "" {
			return
		}
		_ = h.Store.SetDeviceLabels(ctx, pub, name, model)
		return
	}
	if name == "" {
		return
	}
	_ = h.Store.PutHost(ctx, store.Host{Pub: append([]byte(nil), pub...), Name: name})
}
