// Package qr renders and reads pairlink offer URIs as QR images.
package qr

import (
	"bytes"
	"fmt"
	"image"
	"image/png"

	"github.com/LubyRuffy/pairlink/protocol"
	qrcode "github.com/skip2/go-qrcode"
)

// PNG encodes an offer URI. size is the pixel edge; 256 is readable on a
// desktop pairing panel.
func PNG(uri string, size int) ([]byte, error) {
	if _, err := protocol.Parse(uri); err != nil {
		return nil, fmt.Errorf("qr: %w", err)
	}
	if size < 128 {
		size = 256
	}
	q, err := qrcode.New(uri, qrcode.Medium)
	if err != nil {
		return nil, fmt.Errorf("qr: %w", err)
	}
	real := len(q.Bitmap())
	scale := size / real
	if scale < 4 {
		scale = 4
	}
	return q.PNG(-scale)
}

// DecodePNG reads the offer URI a camera would see. Production phones use
// the OS scanner; this exists so tests prove the pixels are actually a QR.
func DecodePNG(raw []byte) (string, error) {
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("qr: png: %w", err)
	}
	return DecodeImage(img)
}

// DecodeImage is the scanner path without the PNG container.
func DecodeImage(img image.Image) (string, error) {
	uri, err := decodeQR(img)
	if err != nil {
		return "", err
	}
	if _, err := protocol.Parse(uri); err != nil {
		return "", fmt.Errorf("qr: %w", err)
	}
	return uri, nil
}
