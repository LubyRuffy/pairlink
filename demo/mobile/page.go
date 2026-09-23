package mobile

import (
	_ "embed"
	"encoding/json"
	"image"
	"io"
	"net/http"
	"strings"
)

//go:embed index.html
var page []byte

// Register mounts the phone page and the QR decode endpoint on mux.
func Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /demo/mobile", servePage)
	mux.HandleFunc("GET /demo/mobile/", servePage)
	mux.HandleFunc("POST /demo/qr", serveDecode)
}

func servePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(page)
}

func serveDecode(w http.ResponseWriter, r *http.Request) {
	img, err := imageFromRequest(r)
	if err != nil {
		http.Error(w, "bad image", http.StatusBadRequest)
		return
	}
	text, err := DecodeOffer(img)
	if err != nil {
		http.Error(w, "decode failed", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]string{"text": text})
}

func imageFromRequest(r *http.Request) (image.Image, error) {
	var src io.Reader
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/") {
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			return nil, err
		}
		f, _, err := r.FormFile("image")
		if err != nil {
			return nil, err
		}
		defer f.Close()
		src = io.LimitReader(f, 8<<20)
	} else {
		src = io.LimitReader(r.Body, 8<<20)
	}
	img, _, err := image.Decode(src)
	return img, err
}
