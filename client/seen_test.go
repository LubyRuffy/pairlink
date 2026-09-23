package client

import (
	"bytes"
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/LubyRuffy/pairlink/relay"
	"github.com/LubyRuffy/pairlink/store"
)

func TestDeviceSocketStampsLastConnected(t *testing.T) {
	hub, hubURL, cancel := startHub(t)
	defer cancel()
	var step atomic.Int64
	// Pairing expiry is compared to the wall clock inside the store. A frozen
	// hub clock older than the pairing TTL makes a fresh code look expired.
	base := time.Now().UTC().Truncate(time.Second)
	hub.SetNow(func() time.Time {
		return base.Add(time.Duration(step.Load()) * time.Second)
	})
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
	otherID, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := RegisterHost(ctx, hubURL, token, hostID); err != nil {
		t.Fatal(err)
	}
	hostC, err := Dial(ctx, Config{HubURL: hubURL, Identity: hostID, Token: token, DisableUDP: true})
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
	ticket, hostPub, _, err := RedeemOffer(ctx, offer, devID)
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.Store.PutBinding(ctx, store.Binding{
		ID: "other-phone", HostPub: hostPub, DevicePub: otherID.Public(), Created: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if seen := deviceSeen(t, hub.Store, devID.Public()); !seen.IsZero() {
		t.Fatalf("pc socket stamped the phone %s", seen)
	}

	devC, err := Dial(ctx, Config{HubURL: hubURL, Identity: devID, Token: ticket, DisableUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	waitDeviceSeenEqual(t, hub.Store, devID.Public(), base)
	if seen := deviceSeen(t, hub.Store, otherID.Public()); !seen.IsZero() {
		t.Fatalf("other phone moved when this phone connected %s", seen)
	}
	step.Store(1)
	if err := devC.Close(); err != nil {
		t.Fatal(err)
	}
	waitDeviceSeenEqual(t, hub.Store, devID.Public(), base.Add(time.Second))
	if seen := deviceSeen(t, hub.Store, otherID.Public()); !seen.IsZero() {
		t.Fatalf("other phone stamped on disconnect %s", seen)
	}
}

func deviceSeen(t *testing.T, st store.Store, devicePub []byte) time.Time {
	t.Helper()
	rows, err := st.ListAllBindings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if bytes.Equal(row.DevicePub, devicePub) {
			return row.LastConnected
		}
	}
	t.Fatal("binding missing")
	return time.Time{}
}

func waitDeviceSeenEqual(t *testing.T, st store.Store, devicePub []byte, want time.Time) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var seen time.Time
	for time.Now().Before(deadline) {
		seen = deviceSeen(t, st, devicePub)
		if seen.Equal(want) {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("last connection %s, want %s", seen, want)
}
