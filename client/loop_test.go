package client

import (
	"bytes"
	"context"
	"encoding/hex"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/LubyRuffy/pairlink/qr"
	"github.com/LubyRuffy/pairlink/relay"
	"github.com/LubyRuffy/pairlink/store"
)

func startHub(t *testing.T) (*relay.Hub, string, context.CancelFunc) {
	t.Helper()
	st := store.NewMemory()
	h := relay.New(st)
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithCancel(context.Background())
	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	go func() { _ = h.ServeUDP(ctx, pc) }()
	return h, srv.URL, cancel
}

func pairWith(t *testing.T, hub *relay.Hub, hubURL string, disableUDP bool) (hostC, devC *Conn, hostL, devL *Link) {
	t.Helper()
	ctx := context.Background()
	token, err := relay.IssueHostToken(ctx, hub.Store)
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	devID, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := RegisterHost(ctx, hubURL, token, hostID); err != nil {
		t.Fatal(err)
	}
	hostC, err = Dial(ctx, Config{HubURL: hubURL, Identity: hostID, Token: token, DisableUDP: disableUDP})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hostC.Close() })
	uri, pairingID, err := hostC.CreateOffer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if pairingID == "" {
		t.Fatal("missing pairing id")
	}
	png, err := qr.PNG(uri, 256)
	if err != nil {
		t.Fatal(err)
	}
	if len(png) < 80 {
		t.Fatal("qr too small")
	}
	offer, err := protocol.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	ticket, hostPub, sid, err := RedeemOffer(ctx, offer, devID)
	if err != nil {
		t.Fatal(err)
	}
	devC, err = Dial(ctx, Config{HubURL: hubURL, Identity: devID, Token: ticket, DisableUDP: disableUDP})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = devC.Close() })
	devL, err = devC.OpenInitiator(ctx, hostPub, sid)
	if err != nil {
		t.Fatal(err)
	}
	wctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	hostL, err = hostC.WaitLink(wctx, devID.Public())
	if err != nil {
		t.Fatal(err)
	}
	return hostC, devC, hostL, devL
}

func waitHubPath(t *testing.T, hub *relay.Hub, a, b []byte, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		got = hub.LinkPath(a, b)
		if got == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("hub path %q, want %q", got, want)
}

func TestRelayOnlyEchoAndHubCannotReadBody(t *testing.T) {
	hub, url, cancel := startHub(t)
	defer cancel()
	_, _, hostL, devL := pairWith(t, hub, url, true)
	plain := []byte("opaque-payload")
	if err := devL.Send(plain); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-hostL.Recv():
		if !bytes.Equal(got, plain) {
			t.Fatalf("got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
	if hostL.Path() != protocol.PathRelay || devL.Path() != protocol.PathRelay {
		t.Fatalf("path host=%s device=%s", hostL.Path(), devL.Path())
	}
	waitHubPath(t, hub, hostL.PeerPub, devL.PeerPub, protocol.PathRelay)
	found := false
	for _, p := range hub.SniffedPayloads() {
		if bytes.Contains(p, plain) {
			t.Fatal("hub forwarded plaintext")
		}
		if bytes.Equal(p, []byte{protocol.PathCodeRelay}) || bytes.Equal(p, []byte{protocol.PathCodeDirect}) {
			t.Fatal("path announcement sniffed as application data")
		}
		if len(p) > 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("hub saw no data frames")
	}
}

func TestDirectUpgradeThenFallback(t *testing.T) {
	hub, url, cancel := startHub(t)
	defer cancel()
	hostC, devC, hostL, devL := pairWith(t, hub, url, false)
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if hostL.Path() == protocol.PathDirect && devL.Path() == protocol.PathDirect {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if hostL.Path() != protocol.PathDirect {
		t.Fatalf("expected direct, host=%s device=%s", hostL.Path(), devL.Path())
	}
	waitHubPath(t, hub, hostL.PeerPub, devL.PeerPub, protocol.PathDirect)
	msg := []byte("after-upgrade")
	if err := hostL.Send(msg); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-devL.Recv():
		if !bytes.Equal(got, msg) {
			t.Fatalf("got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout after upgrade")
	}
	hostC.KillUDP()
	devC.KillUDP()
	deadline = time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if hostL.Path() == protocol.PathRelay {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if hostL.Path() != protocol.PathRelay {
		t.Fatalf("expected fallback relay, got %s", hostL.Path())
	}
	waitHubPath(t, hub, hostL.PeerPub, devL.PeerPub, protocol.PathRelay)
	msg2 := []byte("after-fallback")
	if err := devL.Send(msg2); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-hostL.Recv():
		if !bytes.Equal(got, msg2) {
			t.Fatalf("got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout after fallback")
	}
}

func TestTraceHasBindAndForward(t *testing.T) {
	hub, url, cancel := startHub(t)
	defer cancel()
	hostC, _, hostL, devL := pairWith(t, hub, url, true)
	_ = devL.Send([]byte("trace-me"))
	select {
	case <-hostL.Recv():
	case <-time.After(2 * time.Second):
		t.Fatal("no echo")
	}
	raw, err := hostC.Trace(context.Background(), hostL.SessionID())
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"kind":"forward"`) {
		t.Fatalf("trace missing forward: %s", s)
	}
	if strings.Contains(s, "trace-me") {
		t.Fatal("trace leaked payload")
	}
	if _, err := hex.DecodeString(hostL.SessionID()); err != nil {
		t.Fatal(err)
	}
}

func TestExpiredPairingRejected(t *testing.T) {
	st := store.NewMemory()
	h := relay.New(st)
	h.SetNow(func() time.Time { return time.Now().Add(-time.Hour) })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	ctx := context.Background()
	token, err := relay.IssueHostToken(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	hostID, _ := crypto.Generate()
	if err := RegisterHost(ctx, srv.URL, token, hostID); err != nil {
		t.Fatal(err)
	}
	hostC, err := Dial(ctx, Config{HubURL: srv.URL, Identity: hostID, Token: token, DisableUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hostC.Close() })
	// Reset clock so CreateOffer writes an already-expired pairing.
	h.SetNow(func() time.Time { return time.Unix(0, 0) })
	uri, _, err := hostC.CreateOffer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	h.SetNow(time.Now)
	offer, _ := protocol.Parse(uri)
	devID, _ := crypto.Generate()
	_, _, _, err = RedeemOffer(ctx, offer, devID)
	if err == nil {
		t.Fatal("expired pairing should fail")
	}
}

func dialHost(t *testing.T, hub *relay.Hub, hubURL string, keep time.Duration) *Conn {
	t.Helper()
	ctx := context.Background()
	token, err := relay.IssueHostToken(ctx, hub.Store)
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := RegisterHost(ctx, hubURL, token, hostID); err != nil {
		t.Fatal(err)
	}
	c, err := Dial(ctx, Config{
		HubURL: hubURL, Identity: hostID, Token: token, DisableUDP: true, KeepAlive: keep,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestKeepAliveHoldsTheHostThroughAShortIdleDeadline(t *testing.T) {
	// The live hub drops a quiet WS after 60s; nginx often matches that.
	// Without a data-frame keepalive the PC still mints QR codes (HTTP) and
	// the phone redeem returns host offline.
	st := store.NewMemory()
	h := relay.New(st)
	h.Idle = 200 * time.Millisecond
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	hostC := dialHost(t, h, srv.URL, 50*time.Millisecond)
	for i := 0; i < 8; i++ {
		time.Sleep(80 * time.Millisecond)
		if !hostC.Connected() {
			t.Fatalf("host dropped at tick %d", i)
		}
	}
	ctx := context.Background()
	uri, _, err := hostC.CreateOffer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	offer, err := protocol.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	devID, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := RedeemOffer(ctx, offer, devID); err != nil {
		t.Fatal(err)
	}
}

func TestHostReconnectsAfterTheHubDropsTheSocket(t *testing.T) {
	st := store.NewMemory()
	h := relay.New(st)
	h.Idle = 80 * time.Millisecond
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	hostC := dialHost(t, h, srv.URL, time.Hour)
	if !hostC.Connected() {
		t.Fatal("expected connected")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && hostC.Connected() {
		time.Sleep(10 * time.Millisecond)
	}
	if hostC.Connected() {
		t.Fatal("quiet socket should idle-drop without keepalive")
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !hostC.Connected() {
		time.Sleep(20 * time.Millisecond)
	}
	if !hostC.Connected() {
		t.Fatal("host must reconnect after the hub drops the socket")
	}
	ctx := context.Background()
	uri, _, err := hostC.CreateOffer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	offer, err := protocol.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	devID, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := RedeemOffer(ctx, offer, devID); err != nil {
		t.Fatal(err)
	}
	hostC.Close()
	if hostC.Connected() {
		t.Fatal("Close must clear Connected")
	}
}
