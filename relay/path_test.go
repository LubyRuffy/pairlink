package relay

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/LubyRuffy/pairlink/store"
	"github.com/gorilla/websocket"
)

func writeFrame(t *testing.T, conn *websocket.Conn, typ byte, src, dst, payload []byte) {
	t.Helper()
	var fr protocol.Frame
	fr.Type = typ
	copy(fr.Src[:], src)
	copy(fr.Dst[:], dst)
	fr.Payload = payload
	raw, err := fr.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, raw); err != nil {
		t.Fatal(err)
	}
}

func waitLinkPath(t *testing.T, h *Hub, a, b []byte, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	var got string
	for time.Now().Before(deadline) {
		got = h.LinkPath(a, b)
		if got == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("link path %q, want %q", got, want)
}

func TestPathAnnouncementIsNotApplicationData(t *testing.T) {
	h := New(nil)
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	_, ticket, hostConn := issueBoundTicket(t, h, srv)
	devConn := dialDevice(t, srv, ticket)
	t.Cleanup(func() { _ = devConn.Close() })
	b, err := h.Store.BindingByTicketHash(context.Background(), store.HashSecret(ticket))
	if err != nil {
		t.Fatal(err)
	}
	waitPeer(t, h, b.HostPub)
	waitPeer(t, h, b.DevicePub)

	writeFrame(t, hostConn, protocol.TypeData, b.HostPub, b.DevicePub, []byte{protocol.PathCodeDirect})
	time.Sleep(50 * time.Millisecond)
	if got := h.LinkPath(b.HostPub, b.DevicePub); got != "" {
		t.Fatalf("data payload became a path %q", got)
	}
	sniffed := false
	for _, payload := range h.SniffedPayloads() {
		if len(payload) == 1 && payload[0] == protocol.PathCodeDirect {
			sniffed = true
		}
	}
	if !sniffed {
		t.Fatal("expected the data frame to be forwarded, not interpreted")
	}

	writeFrame(t, hostConn, protocol.TypePath, b.HostPub, b.DevicePub, []byte{protocol.PathCodeDirect, 0})
	time.Sleep(50 * time.Millisecond)
	if got := h.LinkPath(b.HostPub, b.DevicePub); got != "" {
		t.Fatalf("overlong path payload accepted %q", got)
	}

	other := make([]byte, len(b.DevicePub))
	copy(other, b.DevicePub)
	other[0] ^= 0xff
	writeFrame(t, hostConn, protocol.TypePath, b.HostPub, other, []byte{protocol.PathCodeDirect})
	time.Sleep(50 * time.Millisecond)
	if got := h.LinkPath(b.HostPub, other); got != "" {
		t.Fatalf("unbound pair recorded %q", got)
	}

	cur := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	h.SetNow(func() time.Time { return cur })
	writeFrame(t, hostConn, protocol.TypePath, b.HostPub, b.DevicePub, []byte{protocol.PathCodeDirect})
	waitLinkPath(t, h, b.HostPub, b.DevicePub, protocol.PathDirect)
	if h.LinkPath(b.DevicePub, b.HostPub) != protocol.PathDirect {
		t.Fatal("path key depends on argument order")
	}

	cur = cur.Add(PathFreshness + time.Second)
	if got := h.LinkPath(b.HostPub, b.DevicePub); got != "" {
		t.Fatalf("stale direct stayed visible %q", got)
	}

	writeFrame(t, hostConn, protocol.TypePath, b.HostPub, b.DevicePub, []byte{protocol.PathCodeRelay})
	waitLinkPath(t, h, b.HostPub, b.DevicePub, protocol.PathRelay)
}
