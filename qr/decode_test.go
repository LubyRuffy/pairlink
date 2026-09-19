package qr

import (
	"bytes"
	"image/png"
	"testing"

	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
	qrc "github.com/skip2/go-qrcode"
)

func TestDecodeGridFromSkip2Bitmap(t *testing.T) {
	id, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	uri, err := protocol.Encode(protocol.Offer{
		HubURL: "http://127.0.0.1:7780", Code: "ScanCode01", HostPub: id.Public(),
	})
	if err != nil {
		t.Fatal(err)
	}
	q, err := qrc.New(uri, qrc.Medium)
	if err != nil {
		t.Fatal(err)
	}
	bm := q.Bitmap()
	real := len(bm)
	n := real - 8
	ver := (n-21)/4 + 1
	grid := stripQuiet(bm, 4)
	if !finderOK(grid) {
		t.Fatal("finder")
	}
	got, err := decodeGrid(grid, ver)
	if err != nil {
		t.Fatalf("ver=%d n=%d: %v", ver, n, err)
	}
	if got != uri {
		t.Fatalf("got %q", got)
	}

	pngb, err := PNG(uri, 256)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(pngb))
	if err != nil {
		t.Fatal(err)
	}
	mods := sampleModules(img, real)
	grid2 := stripQuiet(mods, 4)
	if !finderOK(grid2) {
		t.Fatal("sampled finder")
	}
	got2, err := decodeGrid(grid2, ver)
	if err != nil {
		t.Fatal(err)
	}
	if got2 != uri {
		t.Fatalf("sampled %q", got2)
	}
}
