package mobile

import (
	"strings"
	"testing"
)

func TestPageAnnouncesSeparateLabels(t *testing.T) {
	src := string(page)
	body := src[strings.Index(src, "function announceLabels"):]
	if !strings.Contains(body, "JSON.stringify({ name: lab.name, model: lab.model })") {
		t.Fatal("phone page must announce name and model as separate fields")
	}
	if strings.Contains(src[strings.Index(src, "function announceLabels"):strings.Index(src, "async function login")], "lab.name +") {
		t.Fatal("label frame must not glue the name and the model together")
	}
	open := src[strings.Index(src, "ws.onopen"):]
	labelAt := strings.Index(open, "announceLabels")
	pathAt := strings.Index(open, "announcePath")
	if labelAt < 0 || pathAt < 0 || labelAt > pathAt {
		t.Fatal("label announcement must be the first frame after the socket opens")
	}
	if !strings.Contains(src, `addEventListener("change"`) {
		t.Fatal("editing a label must announce again")
	}
}
