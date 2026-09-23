package protocol

import (
	"strings"
	"testing"
)

func TestSanitizeLabel(t *testing.T) {
	if got := SanitizeLabel("  alpha\tbeta\n "); got != "alpha beta" {
		t.Fatalf("collapsed %q", got)
	}
	if got := SanitizeLabel(" \n\t "); got != "" {
		t.Fatalf("blank must stay empty, got %q", got)
	}
	long := strings.Repeat("n", LabelMaxRunes+1)
	if got := SanitizeLabel(long); got != strings.Repeat("n", LabelMaxRunes) {
		t.Fatalf("clip len=%d", len([]rune(got)))
	}
	// A hub hostname is a label if the client sent it. Nothing rewrites it.
	if got := SanitizeLabel("hub.example"); got != "hub.example" {
		t.Fatalf("literal %q", got)
	}
}

func TestLabelAnnouncementKeepsNameAndModelApart(t *testing.T) {
	raw, err := MarshalLabel("  Android\tPixel  ", " \n ")
	if err != nil {
		t.Fatal(err)
	}
	name, model, ok := ParseLabel(raw)
	if !ok || name != "Android Pixel" || model != "" {
		t.Fatalf("parsed name=%q model=%q ok=%v", name, model, ok)
	}
	if strings.Contains(string(raw), "Android Pixel ") {
		t.Fatal("sanitizer left trailing space in the announcement")
	}
	name, model, ok = ParseLabel([]byte(`{"name":"unit","model":"m1"}`))
	if !ok || name != "unit" || model != "m1" {
		t.Fatalf("separate fields name=%q model=%q ok=%v", name, model, ok)
	}
	if _, _, ok = ParseLabel([]byte{1}); ok {
		t.Fatal("path code parsed as a label")
	}
	if _, _, ok = ParseLabel([]byte("unit")); ok {
		t.Fatal("bare string parsed as a label")
	}
	long, err := MarshalLabel(strings.Repeat("n", LabelMaxRunes+4), "m")
	if err != nil {
		t.Fatal(err)
	}
	name, model, ok = ParseLabel(long)
	if !ok || name != strings.Repeat("n", LabelMaxRunes) || model != "m" {
		t.Fatalf("clipped name=%q model=%q ok=%v", name, model, ok)
	}
}
