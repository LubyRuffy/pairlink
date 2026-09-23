package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"

	"github.com/LubyRuffy/pairlink/relay"
	"github.com/LubyRuffy/pairlink/store"
)

func main() {
	opt, err := loadOptions(os.Args[1:], os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	st, closer, err := openStore(opt.Database)
	if err != nil {
		log.Fatal(err)
	}
	if closer != nil {
		defer closer.Close()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	adminGenerated := false
	if opt.AdminToken == "" {
		opt.AdminToken, err = randomToken()
		if err != nil {
			log.Fatal(err)
		}
		adminGenerated = true
	}
	hub := relay.New(st)
	hub.SetAdminToken(opt.AdminToken)

	hostToken, created, n, err := ensureHostToken(ctx, st)
	if err != nil {
		log.Fatal(err)
	}
	printReady(os.Stderr, opt, adminGenerated, hostToken, created, n)

	pc, err := net.ListenPacket("udp", opt.UDP)
	if err != nil {
		log.Fatal(err)
	}
	defer pc.Close()
	go func() {
		_ = hub.ServeUDP(ctx, pc.(*net.UDPConn))
	}()

	srv := &http.Server{Addr: opt.Listen, Handler: newMux(hub)}
	if opt.TLS {
		pair, pemBytes, err := buildCert(certIPs(opt.Listen))
		if err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(opt.TLSCert, pemBytes, 0o644); err != nil {
			log.Fatal(err)
		}
		srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	var serveErr error
	if opt.TLS {
		serveErr = srv.ListenAndServeTLS("", "")
	} else {
		serveErr = srv.ListenAndServe()
	}
	if serveErr != nil && serveErr != http.ErrServerClosed {
		log.Fatal(serveErr)
	}
}

func openStore(path string) (store.Store, io.Closer, error) {
	if path == "" {
		return store.NewMemory(), nil, nil
	}
	s, err := store.OpenSQLite(path)
	if err != nil {
		return nil, nil, err
	}
	return s, s, nil
}

func ensureHostToken(ctx context.Context, st store.Store) (raw string, created bool, n int, err error) {
	hosts, err := st.ListHosts(ctx)
	if err != nil {
		return "", false, 0, err
	}
	if len(hosts) > 0 {
		return "", false, len(hosts), nil
	}
	raw, err = relay.IssueHostToken(ctx, st)
	return raw, err == nil, 1, err
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func printReady(w io.Writer, opt options, adminGenerated bool, hostToken string, hostCreated bool, hosts int) {
	db := "memory"
	if opt.Database != "" {
		db = opt.Database
	}
	slog.Info("listen", "addr", opt.Listen, "udp", opt.UDP, "database", db, "tls", opt.TLS)
	scheme := "http"
	if opt.TLS {
		scheme = "https"
	}
	for _, host := range listenHosts(opt.Listen) {
		fmt.Fprintf(w, "admin  %s://%s/pairlink/admin\n", scheme, host)
		fmt.Fprintf(w, "mobile %s://%s/demo/mobile\n", scheme, host)
	}
	if hostCreated {
		fmt.Fprintf(w, "host token (shown once): %s\n", hostToken)
	} else {
		fmt.Fprintf(w, "hosts=%d (raw host tokens are not stored)\n", hosts)
	}
	if adminGenerated {
		fmt.Fprintf(w, "admin token (shown once): %s\n", opt.AdminToken)
	} else {
		fmt.Fprintln(w, "admin token configured")
	}
	if opt.TLS {
		fmt.Fprintf(w, "tls cert written to %s\n", opt.TLSCert)
	}
}
