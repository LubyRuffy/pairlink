package main

import (
	"crypto/tls"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestHostLabel(t *testing.T) {
	got, err := hostLabel("  box one  ")
	if err != nil || got != "box one" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := hostLabel("\x01"); err == nil {
		t.Fatal("control-only label")
	}
}

func TestHubClientRejectsBoth(t *testing.T) {
	if _, err := hubClient("x", true); err == nil {
		t.Fatal("expected error")
	}
}

func TestHubClientInsecure(t *testing.T) {
	c, err := hubClient("", true)
	if err != nil {
		t.Fatal(err)
	}
	tr := c.Transport.(*http.Transport)
	if tr.TLSClientConfig == nil || !tr.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("insecure")
	}
	if tr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatal("min version")
	}
}

func TestHubClientRejectsGarbageCA(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := hubClient(path, false); err == nil {
		t.Fatal("expected error")
	}
}
