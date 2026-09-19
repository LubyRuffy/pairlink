package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"

	"github.com/LubyRuffy/pairlink/relay"
	"github.com/LubyRuffy/pairlink/store"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:7780", "HTTP listen address")
	udp := flag.String("udp", "127.0.0.1:7781", "STUN-lite UDP listen address")
	flag.Parse()
	st := store.NewMemory()
	hub := relay.New(st)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	token, err := relay.IssueHostToken(ctx, st)
	if err != nil {
		log.Fatal(err)
	}
	pc, err := net.ListenPacket("udp", *udp)
	if err != nil {
		log.Fatal(err)
	}
	defer pc.Close()
	go func() {
		_ = hub.ServeUDP(ctx, pc.(*net.UDPConn))
	}()
	fmt.Fprintf(os.Stderr, "pairlinkd listen=%s udp=%s\n", *listen, *udp)
	fmt.Fprintf(os.Stderr, "host token (shown once): %s\n", token)
	srv := &http.Server{Addr: *listen, Handler: hub.Handler()}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
