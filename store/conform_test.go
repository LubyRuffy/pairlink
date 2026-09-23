package store

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestMemoryConformance(t *testing.T) {
	runStoreConformance(t, func(t *testing.T) Store { return NewMemory() })
}

func TestTwoEmptyPubsDoNotCollapse(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	if err := m.PutHost(ctx, Host{TokenHash: HashSecret("a"), Created: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := m.PutHost(ctx, Host{TokenHash: HashSecret("b"), Created: time.Now()}); err != nil {
		t.Fatal(err)
	}
	list, err := m.ListHosts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("hosts=%d", len(list))
	}
}

func runStoreConformance(t *testing.T, open func(t *testing.T) Store) {
	t.Helper()
	st := open(t)
	ctx := context.Background()
	hostPub := bytes.Repeat([]byte{1}, 32)
	devPub := bytes.Repeat([]byte{2}, 32)
	tok := HashSecret("token-a")
	if err := st.PutHost(ctx, Host{TokenHash: tok, Created: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutHost(ctx, Host{TokenHash: HashSecret("token-b"), Created: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	h, err := st.HostByTokenHash(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	h.Pub = hostPub
	h.Name = "box-a"
	if err := st.PutHost(ctx, h); err != nil {
		t.Fatal(err)
	}
	h, err = st.HostByPub(ctx, hostPub)
	if err != nil || h.Name != "box-a" {
		t.Fatalf("host %+v %v", h, err)
	}
	if _, err := st.HostByPub(ctx, bytes.Repeat([]byte{9}, 32)); err != ErrNotFound {
		t.Fatalf("missing pub %v", err)
	}
	if err := st.PutHost(ctx, Host{Pub: hostPub, Name: "via-pub"}); err != nil {
		t.Fatal(err)
	}
	h, err = st.HostByTokenHash(ctx, tok)
	if err != nil || h.Name != "via-pub" || h.Created.IsZero() {
		t.Fatalf("pub update %+v %v", h, err)
	}
	if err := st.PutHost(ctx, Host{TokenHash: HashSecret("token-c")}); err != nil {
		t.Fatal(err)
	}
	list, err := st.ListHosts(ctx)
	if err != nil || len(list) != 3 {
		t.Fatalf("list %d %v", len(list), err)
	}
	if _, err := st.HostByTokenHash(ctx, HashSecret("missing")); err != ErrNotFound {
		t.Fatalf("missing token %v", err)
	}

	sid := bytes.Repeat([]byte{9}, 16)
	code := HashSecret("code-1")
	if err := st.PutPairing(ctx, Pairing{
		ID: "pair-1", HostPub: hostPub, CodeHash: code,
		Expires: time.Now().Add(time.Minute), SessionID: sid,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.ConsumePairing(ctx, code)
	if err != nil || got.ID != "pair-1" || !got.Consumed {
		t.Fatalf("consume %+v %v", got, err)
	}
	if _, err := st.ConsumePairing(ctx, code); err != ErrConsumed {
		t.Fatalf("second consume %v", err)
	}
	found, err := st.PairingByID(ctx, "pair-1")
	if err != nil || found.ID != "pair-1" || len(found.SessionID) != 16 {
		t.Fatalf("pairing by id %+v %v", found, err)
	}
	if _, err := st.PairingByID(ctx, "nope"); err != ErrNotFound {
		t.Fatalf("pairing %v", err)
	}
	exp := HashSecret("expired")
	if err := st.PutPairing(ctx, Pairing{ID: "pair-x", CodeHash: exp, Expires: time.Now().Add(-time.Second)}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ConsumePairing(ctx, exp); err != ErrExpired {
		t.Fatalf("expired %v", err)
	}
	if _, err := st.ConsumePairing(ctx, HashSecret("absent")); err != ErrNotFound {
		t.Fatalf("absent %v", err)
	}

	b := Binding{
		ID: "bind-1", HostPub: hostPub, DevicePub: devPub, TicketHash: HashSecret("ticket"),
		DeviceName: "dev-a", DeviceModel: "mod-a", Created: time.Now().UTC(), SessionID: sid,
	}
	if err := st.PutBinding(ctx, b); err != nil {
		t.Fatal(err)
	}
	byT, err := st.BindingByTicketHash(ctx, HashSecret("ticket"))
	if err != nil || byT.DeviceName != "dev-a" || byT.DeviceModel != "mod-a" {
		t.Fatalf("ticket %+v %v", byT, err)
	}
	byP, err := st.BindingByPeers(ctx, hostPub, devPub)
	if err != nil || byP.ID != "bind-1" {
		t.Fatalf("peers %+v %v", byP, err)
	}
	n, err := st.CountBindings(ctx, hostPub)
	if err != nil || n != 1 {
		t.Fatalf("count %d %v", n, err)
	}
	rows, err := st.ListBindings(ctx, hostPub)
	if err != nil || len(rows) != 1 {
		t.Fatalf("bindings %d %v", len(rows), err)
	}
	if err := st.RevokeBinding(ctx, "bind-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BindingByTicketHash(ctx, HashSecret("ticket")); err != ErrRevoked {
		t.Fatalf("revoked ticket %v", err)
	}
	if _, err := st.BindingByPeers(ctx, hostPub, devPub); err != ErrRevoked {
		t.Fatalf("revoked peers %v", err)
	}
	if _, err := st.BindingByTicketHash(ctx, HashSecret("missing-ticket")); err != ErrNotFound {
		t.Fatalf("missing ticket %v", err)
	}
	if _, err := st.BindingByPeers(ctx, hostPub, bytes.Repeat([]byte{7}, 32)); err != ErrNotFound {
		t.Fatalf("missing peers %v", err)
	}
	if err := st.RevokeBinding(ctx, "missing"); err != ErrNotFound {
		t.Fatalf("revoke missing %v", err)
	}
	left, err := st.ListBindings(ctx, hostPub)
	if err != nil || len(left) != 0 {
		t.Fatalf("active after revoke %d %v", len(left), err)
	}
	all, err := st.ListAllBindings(ctx)
	if err != nil || len(all) != 1 || !all[0].Revoked || !all[0].LastConnected.IsZero() {
		t.Fatalf("all %+v %v", all, err)
	}
	otherHost := bytes.Repeat([]byte{5}, 32)
	other := bytes.Repeat([]byte{4}, 32)
	if err := st.PutBinding(ctx, Binding{
		ID: "bind-other", HostPub: otherHost, DevicePub: other, TicketHash: HashSecret("ticket-other"),
		Created: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	seen := time.Date(2026, 9, 23, 1, 2, 3, 0, time.UTC)
	if err := st.NoteDeviceSeen(ctx, devPub, seen); err != nil {
		t.Fatal(err)
	}
	if err := st.NoteDeviceSeen(ctx, hostPub, seen.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := st.NoteDeviceSeen(ctx, devPub, seen.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := st.NoteDeviceSeen(ctx, devPub, time.Time{}); err != nil {
		t.Fatal(err)
	}
	all, err = st.ListAllBindings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	gotSeen := map[string]time.Time{}
	for _, row := range all {
		gotSeen[row.ID] = row.LastConnected
	}
	if !gotSeen["bind-1"].Equal(seen) {
		t.Fatalf("revoked device seen %s", gotSeen["bind-1"])
	}
	if !gotSeen["bind-other"].IsZero() {
		t.Fatalf("other device was stamped %s", gotSeen["bind-other"])
	}
	later := seen.Add(time.Minute)
	if err := st.NoteDeviceSeen(ctx, devPub, later); err != nil {
		t.Fatal(err)
	}
	all, err = st.ListAllBindings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range all {
		if row.ID == "bind-1" && !row.LastConnected.Equal(later) {
			t.Fatalf("later seen %s", row.LastConnected)
		}
	}

	for i := 0; i < MaxBindingsPerHost; i++ {
		err := st.PutBinding(ctx, Binding{ID: string(rune('A'+i)) + "x", HostPub: hostPub, DevicePub: []byte{byte(i)}})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := st.PutBinding(ctx, Binding{ID: "overflow", HostPub: hostPub, DevicePub: []byte{255}}); err != ErrLimit {
		t.Fatalf("limit %v", err)
	}

	if err := st.AppendTrace(ctx, TraceEvent{Ref: "s1", Kind: "bind", PeerFP: "fp", Bytes: 3, Path: "relay"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendTrace(ctx, TraceEvent{Ref: "s2", Kind: "forward"}); err != nil {
		t.Fatal(err)
	}
	ev, err := st.Trace(ctx, "s1")
	if err != nil || len(ev) != 1 || ev[0].Kind != "bind" || ev[0].Bytes != 3 || ev[0].At.IsZero() {
		t.Fatalf("trace %+v %v", ev, err)
	}
}
