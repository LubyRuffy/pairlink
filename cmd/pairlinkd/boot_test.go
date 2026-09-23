package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LubyRuffy/pairlink/store"
)

func TestEnsureHostTokenOnce(t *testing.T) {
	st := store.NewMemory()
	ctx := context.Background()
	raw, created, n, err := ensureHostToken(ctx, st)
	if err != nil || !created || n != 1 || raw == "" {
		t.Fatalf("first %q created=%v n=%d %v", raw, created, n, err)
	}
	again, created, n, err := ensureHostToken(ctx, st)
	if err != nil || created || n != 1 || again != "" {
		t.Fatalf("second %q created=%v n=%d %v", again, created, n, err)
	}
}

func TestOpenStoreMemoryAndFile(t *testing.T) {
	st, closer, err := openStore("")
	if err != nil || closer != nil || st == nil {
		t.Fatalf("memory %+v %v", closer, err)
	}
	path := filepath.Join(t.TempDir(), "pairlink.db")
	st, closer, err = openStore(path)
	if err != nil || closer == nil {
		t.Fatal(err)
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	_ = st
}

func TestPrintReadyHidesConfiguredAdminToken(t *testing.T) {
	var buf bytes.Buffer
	printReady(&buf, options{Listen: "127.0.0.1:7780", AdminToken: "super-secret-value"}, false, "", false, 2)
	out := buf.String()
	if strings.Contains(out, "super-secret-value") {
		t.Fatal(out)
	}
	if !strings.Contains(out, "admin token configured") || !strings.Contains(out, "hosts=2") {
		t.Fatal(out)
	}
	if !strings.Contains(out, "http://127.0.0.1:7780/pairlink/admin") {
		t.Fatal(out)
	}
}

func TestPrintReadyShowsIssuedTokens(t *testing.T) {
	var buf bytes.Buffer
	printReady(&buf, options{Listen: "127.0.0.1:9", TLS: true, TLSCert: "c.pem", AdminToken: "adm-once"}, true, "host-once", true, 1)
	out := buf.String()
	if !strings.Contains(out, "adm-once") || !strings.Contains(out, "host-once") || !strings.Contains(out, "https://127.0.0.1:9/demo/mobile") {
		t.Fatal(out)
	}
}

func TestRandomToken(t *testing.T) {
	a, err := randomToken()
	if err != nil || len(a) != 64 {
		t.Fatalf("%q %v", a, err)
	}
	b, err := randomToken()
	if err != nil || a == b {
		t.Fatal("token collision")
	}
}
