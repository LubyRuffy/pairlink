package client

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/LubyRuffy/pairlink/relay"
	"github.com/LubyRuffy/pairlink/store"
	"github.com/gorilla/websocket"
)

func TestFirstSocketFrameIsLabel(t *testing.T) {
	got := make(chan protocol.Frame, 1)
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pairlink/v1/ws" {
			http.NotFound(w, r)
			return
		}
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		_, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		fr, err := protocol.UnmarshalFrame(data)
		if err != nil {
			return
		}
		got <- fr
	}))
	t.Cleanup(srv.Close)
	id, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, Config{
		HubURL: srv.URL, Identity: id, Token: "tok", DisableUDP: true,
		Name: "  station\t7 ", Model: "m-1", Version: " 1.2.3 ",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	select {
	case fr := <-got:
		if fr.Type != protocol.TypeLabel {
			t.Fatalf("first frame type %d", fr.Type)
		}
		if fr.Type == protocol.TypeHandshake {
			t.Fatal("handshake was sent before the label")
		}
		name, model, version, ok := protocol.ParseLabel(fr.Payload)
		if !ok || name != "station 7" || model != "m-1" || version != "1.2.3" {
			t.Fatalf("label name=%q model=%q ok=%v payload=%s", name, model, ok, fr.Payload)
		}
		if bytes.Contains(fr.Payload, []byte("station 7 m-1")) {
			t.Fatal("name and model were glued into one field")
		}
	case <-ctx.Done():
		t.Fatal("socket opened without a label frame")
	}
}

func TestDialAndSetLabelsReachHubWithoutHandshake(t *testing.T) {
	st := store.NewMemory()
	hub := relay.New(st)
	srv := httptest.NewServer(hub.Handler())
	t.Cleanup(srv.Close)
	ctx := context.Background()
	token, err := relay.IssueHostToken(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := RegisterHostLabel(ctx, srv.URL, token, "kept", hostID); err != nil {
		t.Fatal(err)
	}
	hostC, err := Dial(ctx, Config{HubURL: srv.URL, Identity: hostID, Token: token, DisableUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hostC.Close() })
	waitStoreHost(t, st, token, "kept")
	hostC.SetLabels("station-7", "ignored-on-host", "1.2.3")
	waitStoreHost(t, st, token, "station-7")

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
	ticket, hostPub, sid, err := RedeemOffer(ctx, offer, devID)
	if err != nil {
		t.Fatal(err)
	}
	uri2, _, err := hostC.CreateOffer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	offer2, err := protocol.Parse(uri2)
	if err != nil {
		t.Fatal(err)
	}
	otherID, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := RedeemOfferInfo(ctx, nil, offer2, otherID, "other-unit", "other-model", ""); err != nil {
		t.Fatal(err)
	}
	devC, err := Dial(ctx, Config{
		HubURL: srv.URL, Identity: devID, Token: ticket, DisableUDP: true,
		Name: "pocket", Model: "m-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = devC.Close() })
	waitStoreDevice(t, st, hostID.Public(), devID.Public(), "pocket", "m-1")
	waitStoreDevice(t, st, hostID.Public(), otherID.Public(), "other-unit", "other-model")

	devC.SetLabels("glued system model", "", "0.1.10")
	waitStoreDevice(t, st, hostID.Public(), devID.Public(), "glued system model", "m-1")
	waitStoreDevice(t, st, hostID.Public(), otherID.Public(), "other-unit", "other-model")
	if got := mustHost(t, st, token).Name; got != "station-7" {
		t.Fatalf("device label overwrote host %q", got)
	}

	if _, err := devC.OpenInitiator(ctx, hostPub, sid); err != nil {
		t.Fatal(err)
	}
	for _, payload := range hub.SniffedPayloads() {
		if bytes.Contains(payload, []byte("station-7")) || bytes.Contains(payload, []byte("glued system model")) {
			t.Fatal("plaintext label was forwarded as application data")
		}
	}
	if got := mustHost(t, st, token).Name; got != "station-7" {
		t.Fatalf("handshake rewrote host label %q", got)
	}
}

func waitStoreHost(t *testing.T, st store.Store, token, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	var got string
	for time.Now().Before(deadline) {
		got = mustHost(t, st, token).Name
		if got == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("host label %q, want %q", got, want)
}

func mustHost(t *testing.T, st store.Store, token string) store.Host {
	t.Helper()
	got, err := st.HostByTokenHash(context.Background(), store.HashSecret(token))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func waitStoreDevice(t *testing.T, st store.Store, hostPub, devPub []byte, name, model string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	var got store.Binding
	for time.Now().Before(deadline) {
		row, err := st.BindingByPeers(context.Background(), hostPub, devPub)
		if err != nil {
			t.Fatal(err)
		}
		got = row
		if got.DeviceName == name && got.DeviceModel == model {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("device name=%q model=%q, want %q %q", got.DeviceName, got.DeviceModel, name, model)
}
