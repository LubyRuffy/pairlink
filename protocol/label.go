package protocol

import (
	"bytes"
	"encoding/json"
	"strings"
)

// LabelMaxRunes is the operator-visible name cap for a host or a device.
// The hub stores the sanitized string. It is not an application payload.
const LabelMaxRunes = 80

// SanitizeLabel collapses whitespace, drops control characters, and clips
// to LabelMaxRunes. An empty result means "no label"; callers must not
// overwrite a stored name with it.
func SanitizeLabel(raw string) string {
	var b strings.Builder
	space := false
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			space = true
			continue
		}
		if r == ' ' || r == '\u00a0' {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	runes := []rune(out)
	if len(runes) > LabelMaxRunes {
		out = strings.TrimSpace(string(runes[:LabelMaxRunes]))
	}
	return out
}

// Label fields are announced separately. A name that happens to contain a
// model string is still just a name; callers must not split it.
type labelBody struct {
	Name  string `json:"name"`
	Model string `json:"model"`
}

// MarshalLabel encodes a hub-visible label announcement. Name and model stay
// in their own fields. Empty fields are included so a missing model is not
// inferred from the name.
func MarshalLabel(name, model string) ([]byte, error) {
	return json.Marshal(labelBody{Name: SanitizeLabel(name), Model: SanitizeLabel(model)})
}

// ParseLabel reads a TypeLabel payload. The bool is false when the payload is
// not a JSON object, including a TypePath code. Returned strings are already
// sanitized; empty means that field was not provided.
func ParseLabel(payload []byte) (name, model string, ok bool) {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 || payload[0] != '{' {
		return "", "", false
	}
	var in labelBody
	if err := json.Unmarshal(payload, &in); err != nil {
		return "", "", false
	}
	return SanitizeLabel(in.Name), SanitizeLabel(in.Model), true
}
