package store

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"
)

func TestLiveRowWinsAndSamePairDoesNotCountTwice(t *testing.T) {
	for _, tc := range []struct {
		name string
		open func(t *testing.T) Store
	}{
		{name: "memory", open: func(t *testing.T) Store { return NewMemory() }},
		{name: "sqlite", open: func(t *testing.T) Store { return openTempSQLite(t) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := tc.open(t)
			ctx := context.Background()
			host := bytes.Repeat([]byte{4}, 32)
			first := bytes.Repeat([]byte{1}, 32)
			for i := 0; i < MaxBindingsPerHost; i++ {
				dev := bytes.Repeat([]byte{byte(i + 1)}, 32)
				err := st.PutBinding(ctx, Binding{
					ID: fmt.Sprintf("b-%d", i), HostPub: host, DevicePub: dev,
					TicketHash: HashSecret(fmt.Sprintf("tix-%d", i)),
					Created:    time.Unix(int64(i+1), 0).UTC(),
				})
				if err != nil {
					t.Fatalf("bind %d: %v", i, err)
				}
			}
			other := bytes.Repeat([]byte{0xff}, 32)
			if err := st.PutBinding(ctx, Binding{
				ID: "overflow", HostPub: host, DevicePub: other, TicketHash: HashSecret("overflow"),
			}); err != ErrLimit {
				t.Fatalf("cap %v", err)
			}
			if err := st.PutBinding(ctx, Binding{
				ID: "b-new", HostPub: host, DevicePub: first, TicketHash: HashSecret("tix-new"),
				Created: time.Unix(100, 0).UTC(),
			}); err != nil {
				t.Fatal(err)
			}
			got, err := st.BindingByPeers(ctx, host, first)
			if err != nil || got.ID != "b-new" {
				t.Fatalf("newest %+v %v", got, err)
			}
			if _, err := st.BindingByTicketHash(ctx, HashSecret("tix-0")); err != ErrRevoked {
				t.Fatalf("old ticket %v", err)
			}
			n, err := st.CountBindings(ctx, host)
			if err != nil || n != MaxBindingsPerHost {
				t.Fatalf("count %d %v", n, err)
			}
			if err := st.RevokeBinding(ctx, "b-new"); err != nil {
				t.Fatal(err)
			}
			if _, err := st.BindingByPeers(ctx, host, first); err != ErrRevoked {
				t.Fatalf("revoked-only %v", err)
			}
		})
	}
}

func TestNewTokenCannotTakeExistingPub(t *testing.T) {
	for _, tc := range []struct {
		name string
		open func(t *testing.T) Store
	}{
		{name: "memory", open: func(t *testing.T) Store { return NewMemory() }},
		{name: "sqlite", open: func(t *testing.T) Store { return openTempSQLite(t) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := tc.open(t)
			ctx := context.Background()
			pub := bytes.Repeat([]byte{3}, 32)
			owner := HashSecret("owner")
			if err := st.PutHost(ctx, Host{TokenHash: owner, Pub: pub, Name: "owner"}); err != nil {
				t.Fatal(err)
			}
			err := st.PutHost(ctx, Host{TokenHash: HashSecret("other"), Pub: pub, Name: "stolen"})
			if err != ErrConflict {
				t.Fatalf("conflict %v", err)
			}
			got, err := st.HostByTokenHash(ctx, owner)
			if err != nil || got.Name != "owner" || !bytes.Equal(got.Pub, pub) {
				t.Fatalf("owner %+v %v", got, err)
			}
			if err := st.PutHost(ctx, Host{Pub: pub, Name: "via-pub"}); err != nil {
				t.Fatal(err)
			}
			got, err = st.HostByTokenHash(ctx, owner)
			if err != nil || got.Name != "via-pub" {
				t.Fatalf("empty token merge %+v %v", got, err)
			}
		})
	}
}
