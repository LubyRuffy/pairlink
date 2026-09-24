package client

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/LubyRuffy/pairlink/relay"
	"github.com/LubyRuffy/pairlink/store"
)

func TestClientSendsHostAndDeviceLabels(t *testing.T) {
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
	if err := RegisterHostLabelHTTP(ctx, nil, srv.URL, token, "  desk-one  ", hostID); err != nil {
		t.Fatal(err)
	}
	if err := RegisterHost(ctx, srv.URL, token, hostID); err != nil {
		t.Fatal(err)
	}
	got, err := st.HostByTokenHash(ctx, store.HashSecret(token))
	if err != nil || got.Name != "desk-one" {
		t.Fatalf("host label %q %v", got.Name, err)
	}

	hostC, err := Dial(ctx, Config{HubURL: srv.URL, Identity: hostID, Token: token, DisableUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hostC.Close() })
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
	if _, _, _, err := RedeemOfferInfo(ctx, nil, offer, devID, "pocket unit", "mod-z", "0.1.10"); err != nil {
		t.Fatal(err)
	}
	rows, err := ListBindings(ctx, srv.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].DeviceName != "pocket unit" || rows[0].DeviceModel != "mod-z" || rows[0].DeviceVersion != "0.1.10" {
		t.Fatalf("bindings %+v", rows)
	}
	if rows[0].DeviceName == "127.0.0.1" {
		t.Fatal("device label must not become the hub host")
	}
}
