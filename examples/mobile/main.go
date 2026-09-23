// Command mobile serves the scan page on its own port.
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/LubyRuffy/pairlink/demo/mobile"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:7791", "page listen address")
	flag.Parse()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/demo/mobile", http.StatusFound)
	})
	mobile.Register(mux)
	log.Printf("mobile page http://%s/demo/mobile", *listen)
	if err := http.ListenAndServe(*listen, mux); err != nil {
		log.Fatal(err)
	}
}
