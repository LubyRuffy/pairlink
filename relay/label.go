package relay

import (
	"context"

	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/LubyRuffy/pairlink/store"
)

// observeLabel stores a TypeLabel announcement from the authenticated socket.
// A host socket updates that host's name and version. A device socket updates
// device_name, device_model, and device_version on bindings for that device
// public key. Empty sanitized fields do not clear a stored value. The frame
// is not forwarded. Version is the string the endpoint sent; the hub does
// not invent one.
func (h *Hub) observeLabel(ctx context.Context, pub []byte, device bool, payload []byte) {
	if h == nil || h.Store == nil || len(pub) != protocol.KeySize {
		return
	}
	name, model, version, ok := protocol.ParseLabel(payload)
	if !ok {
		return
	}
	if device {
		if name == "" && model == "" && version == "" {
			return
		}
		_ = h.Store.SetDeviceLabels(ctx, pub, name, model, version)
		return
	}
	if name == "" && version == "" {
		return
	}
	_ = h.Store.PutHost(ctx, store.Host{Pub: append([]byte(nil), pub...), Name: name, Version: version})
}
