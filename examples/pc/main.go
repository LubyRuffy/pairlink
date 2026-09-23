// Command pc registers this machine and shows a pairing QR.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"

	"github.com/LubyRuffy/pairlink/client"
	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/LubyRuffy/pairlink/qr"
)

//go:embed page.html
var page []byte

func main() {
	hub := flag.String("hub", "", "hub base URL")
	token := flag.String("token", "", "host token")
	name := flag.String("name", "", "display name; default is the OS hostname")
	listen := flag.String("listen", "127.0.0.1:7790", "local page")
	ca := flag.String("ca", "", "PEM of the hub certificate")
	insecure := flag.Bool("insecure", false, "skip TLS verification")
	flag.Parse()
	if strings.TrimSpace(*hub) == "" || strings.TrimSpace(*token) == "" {
		log.Fatal("-hub and -token are required")
	}
	label, err := hostLabel(*name)
	if err != nil {
		log.Fatal(err)
	}
	hc, err := hubClient(*ca, *insecure)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	id, err := crypto.Generate()
	if err != nil {
		log.Fatal(err)
	}
	if err := client.RegisterHostLabelHTTP(ctx, hc, *hub, *token, label, id); err != nil {
		log.Fatal(err)
	}
	conn, err := client.Dial(ctx, client.Config{
		HubURL: *hub, Identity: id, Token: *token, HTTPClient: hc, Name: label,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	var mu sync.Mutex
	state := status{Name: label, Hub: strings.TrimRight(*hub, "/")}
	conn.OnLink(func(l *client.Link) {
		go func() {
			peer := crypto.Fingerprint(l.PeerPub)
			mu.Lock()
			state.Path = l.Path()
			state.Peer = peer
			mu.Unlock()
			fmt.Println("link", peer, "path="+l.Path())
			for range l.Recv() {
				mu.Lock()
				state.Path = l.Path()
				mu.Unlock()
			}
		}()
	})
	refresh := func() error {
		uri, _, err := conn.CreateOffer(ctx)
		if err != nil {
			return err
		}
		png, err := qr.PNG(uri, 256)
		if err != nil {
			return err
		}
		mu.Lock()
		state.PNG = png
		mu.Unlock()
		fmt.Println(uri)
		return nil
	}
	if err := refresh(); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	})
	mux.HandleFunc("GET /qr.png", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		png := append([]byte(nil), state.PNG...)
		mu.Unlock()
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(png)
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		body := status{Name: state.Name, Hub: state.Hub, Path: state.Path, Peer: state.Peer}
		mu.Unlock()
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(body)
	})
	mux.HandleFunc("POST /offer", func(w http.ResponseWriter, r *http.Request) {
		if err := refresh(); err != nil {
			http.Error(w, "offer failed", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	srv := &http.Server{Addr: *listen, Handler: mux}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	fmt.Fprintf(os.Stderr, "pc page http://%s/\n", *listen)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

type status struct {
	Name string `json:"name"`
	Hub  string `json:"hub"`
	Path string `json:"path"`
	Peer string `json:"peer,omitempty"`
	PNG  []byte `json:"-"`
}

func hostLabel(flagName string) (string, error) {
	n := strings.TrimSpace(flagName)
	if n == "" {
		hn, err := os.Hostname()
		if err != nil {
			return "", err
		}
		n = strings.TrimSpace(hn)
	}
	out := protocol.SanitizeLabel(n)
	if out == "" {
		return "", fmt.Errorf("empty hostname; pass -name")
	}
	return out, nil
}
