package mobile

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"

	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/LubyRuffy/pairlink/qr"
	"github.com/makiuchi-d/gozxing"
	zxingqr "github.com/makiuchi-d/gozxing/qrcode"
)

// DecodeOffer reads a pairlink offer from a QR image. Clean PNGs use the
// in-repo decoder; camera photos fall through to a general QR reader.
// The URI is returned to the caller and not logged.
func DecodeOffer(img image.Image) (string, error) {
	if img == nil {
		return "", fmt.Errorf("mobile: empty image")
	}
	if s, err := qr.DecodeImage(img); err == nil {
		return s, nil
	}
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", fmt.Errorf("mobile: bitmap: %w", err)
	}
	hints := map[gozxing.DecodeHintType]interface{}{
		gozxing.DecodeHintType_TRY_HARDER: true,
	}
	res, err := zxingqr.NewQRCodeReader().Decode(bmp, hints)
	if err != nil {
		return "", fmt.Errorf("mobile: qr: %w", err)
	}
	text := res.GetText()
	if _, err := protocol.Parse(text); err != nil {
		return "", fmt.Errorf("mobile: not an offer")
	}
	return text, nil
}
