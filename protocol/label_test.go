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
