package qr

import (
	"bytes"
	"testing"

	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
)

func TestPNGRoundTripViaCameraDecode(t *testing.T) {
	id, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	uri, err := protocol.Encode(protocol.Offer{
		HubURL:  "http://127.0.0.1:7780",
		Code:    "ScanCode01",
		HostPub: id.Public(),
	})
	if err != nil {
		t.Fatal(err)
	}
	pngb, err := PNG(uri, 256)
	if err != nil {
		t.Fatal(err)
	}
	if len(pngb) < 100 || !bytes.HasPrefix(pngb, []byte("\x89PNG")) {
		t.Fatalf("not a png (%d bytes)", len(pngb))
	}
	got, err := DecodePNG(pngb)
	if err != nil {
		t.Fatal(err)
	}
	if got != uri {
		t.Fatalf("decoded %q want %q", got, uri)
	}
	offer, err := protocol.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if offer.Code != "ScanCode01" {
		t.Fatalf("code %q", offer.Code)
	}
}

func TestPNGRoundTripKeepsLANQuery(t *testing.T) {
	id, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	uri, err := protocol.Encode(protocol.Offer{
		HubURL:  "http://127.0.0.1:54321",
		Code:    "Ab3xYz9Qmn",
		HostPub: id.Public(),
		LAN:     []string{"127.0.0.1:40000", "10.0.0.8:40000"},
	})
	if err != nil {
		t.Fatal(err)
	}
	pngb, err := PNG(uri, 256)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodePNG(pngb)
	if err != nil {
		t.Fatal(err)
	}
	if got != uri {
		t.Fatalf("decoded %q want %q", got, uri)
	}
}

func TestPNGRejectsJunk(t *testing.T) {
	if _, err := PNG("https://example.invalid/not-an-offer", 256); err == nil {
		t.Fatal("expected reject")
	}
	if _, err := DecodePNG([]byte("not png")); err == nil {
		t.Fatal("expected reject")
	}
}
