package protocol

import "strings"

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
