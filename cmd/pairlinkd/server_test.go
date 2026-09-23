package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LubyRuffy/pairlink/relay"
	"github.com/LubyRuffy/pairlink/store"
)

func TestMuxRoutes(t *testing.T) {
	h := relay.New(store.NewMemory())
	h.SetAdminToken("adm")
	srv := httptest.NewServer(newMux(h))
	t.Cleanup(srv.Close)
	for _, path := range []string{"/", "/demo/mobile", "/pairlink/admin", "/pairlink/v1/info"} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s %d %s", path, res.StatusCode, body)
		}
	}
}
