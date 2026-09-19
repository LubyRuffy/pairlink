package store

import (
	"context"
	"testing"
	"time"
)

func TestConsumeExpiredPairing(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	p := Pairing{ID: "p1", CodeHash: HashSecret("abc"), Expires: time.Now().Add(-time.Second)}
	if err := m.PutPairing(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ConsumePairing(ctx, HashSecret("abc")); err != ErrExpired {
		t.Fatalf("got %v", err)
	}
}

func TestBindingLimit(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	host := []byte("host-public-key-32-bytes-pad!!!!")
	for i := 0; i < MaxBindingsPerHost; i++ {
		if err := m.PutBinding(ctx, Binding{ID: string(rune('a' + i)), HostPub: host, DevicePub: []byte{byte(i)}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.PutBinding(ctx, Binding{ID: "overflow", HostPub: host, DevicePub: []byte{255}}); err != ErrLimit {
		t.Fatalf("got %v", err)
	}
}
