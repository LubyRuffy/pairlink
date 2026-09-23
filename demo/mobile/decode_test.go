package mobile

import (
	"bytes"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/LubyRuffy/pairlink/qr"
)

func TestDecodeOfferRoundTrip(t *testing.T) {
	id, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	uri, err := protocol.Encode(protocol.Offer{
		HubURL:  "http://127.0.0.1:7780",
		Code:    "abc",
		HostPub: id.Public(),
	})
	if err != nil {
		t.Fatal(err)
	}
	pngBytes, err := qr.PNG(uri, 256)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeOffer(img)
	if err != nil {
		t.Fatal(err)
	}
	if got != uri {
		t.Fatalf("got %s", got)
	}

	mux := http.NewServeMux()
	Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	res, err := http.Post(srv.URL+"/demo/qr", "image/png", bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("post %d", res.StatusCode)
	}
	page, err := http.Get(srv.URL + "/demo/mobile")
	if err != nil {
		t.Fatal(err)
	}
	page.Body.Close()
	if page.StatusCode != http.StatusOK {
		t.Fatalf("page %d", page.StatusCode)
	}

	tiny := image.NewGray(image.Rect(0, 0, 8, 8))
	var buf bytes.Buffer
	if err := png.Encode(&buf, tiny); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("image", "offer.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(pngBytes); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	res, err = http.Post(srv.URL+"/demo/qr", mw.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("multipart %d", res.StatusCode)
	}

	res, err = http.Post(srv.URL+"/demo/qr", "image/png", bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("junk %d", res.StatusCode)
	}
}
