package protocol

import (
	"bytes"
	"testing"
)

func TestOfferRoundTripKeepsColonInHubURL(t *testing.T) {
	pub := bytes.Repeat([]byte{0x11}, 32)
	uri, err := Encode(Offer{
		HubURL:  "wss://hub.example.test:2440/pairlink",
		Code:    "Ab3xYz9Q",
		HostPub: pub,
		LAN:     []string{"10.8.0.2:9100", "127.0.0.1:9100"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	if got.HubURL != "wss://hub.example.test:2440/pairlink" {
		t.Fatalf("hub url: %q", got.HubURL)
	}
	if got.Code != "Ab3xYz9Q" {
		t.Fatalf("code: %q", got.Code)
	}
	if !bytes.Equal(got.HostPub, pub) {
		t.Fatalf("spk mismatch")
	}
	if len(got.LAN) != 2 {
		t.Fatalf("lan: %#v", got.LAN)
	}
}

func TestParseRejectsUnknownScheme(t *testing.T) {
	if _, err := Parse("https://example.test/not-this"); err == nil {
		t.Fatal("expected error")
	}
}

func TestEncodeRejectsColonInCode(t *testing.T) {
	_, err := Encode(Offer{HubURL: "http://h", Code: "a:b", HostPub: bytes.Repeat([]byte{1}, 32)})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseRejectsTruncatedHostPub(t *testing.T) {
	pub := bytes.Repeat([]byte{0x11}, 32)
	uri, err := Encode(Offer{HubURL: "http://127.0.0.1:7780", Code: "ScanCode01", HostPub: pub})
	if err != nil {
		t.Fatal(err)
	}
	cut := uri[:len(uri)-8]
	if _, err := Parse(cut); err == nil {
		t.Fatal("truncated host key must not parse")
	}
}
