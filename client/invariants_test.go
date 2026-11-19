package client

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
)

func TestUDPHandshakeDoesNotReplaceLink(t *testing.T) {
	hub, url, cancel := startHub(t)
	defer cancel()
	hostC, _, hostL, devL := pairWith(t, hub, url, true)
	var fr protocol.Frame
	fr.Type = protocol.TypeHandshake
	copy(fr.Src[:], hostL.PeerPub)
	copy(fr.Dst[:], hostC.cfg.Identity.Public())
	fr.Payload = bytes.Repeat([]byte{9}, 32)
	hostC.handleFrame(fr, protocol.PathDirect, nil)
	if err := hostL.Send([]byte("kept")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-devL.Recv():
		if string(got) != "kept" {
			t.Fatalf("got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("live link stopped delivering")
	}
}

func TestForgedPongDoesNotRedirect(t *testing.T) {
	hub, url, cancel := startHub(t)
	defer cancel()
	hostC, _, hostL, devL := pairWith(t, hub, url, false)
	port := loopbackPort(hostC)
	if port == 0 {
		t.Fatal("host has no loopback udp port")
	}
	atk, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer atk.Close()
	var fr protocol.Frame
	fr.Type = protocol.TypePunchPong
	copy(fr.Src[:], hostL.PeerPub)
	copy(fr.Dst[:], hostC.cfg.Identity.Public())
	raw, err := fr.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	dst := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	for i := 0; i < 8; i++ {
		_, _ = atk.WriteToUDP(raw, dst)
		time.Sleep(20 * time.Millisecond)
	}
	if addr, _ := hostL.peerUDP.Load().(*net.UDPAddr); addr != nil && addr.Port == atk.LocalAddr().(*net.UDPAddr).Port {
		t.Fatal("forged pong installed")
	}
	if err := hostL.Send([]byte("still-here")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-devL.Recv():
		if string(got) != "still-here" {
			t.Fatalf("got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("forged pong ate the data")
	}
}

func TestObservedOnlyFromHubOnRelay(t *testing.T) {
	id, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	c := &Conn{cfg: Config{Identity: id}}
	payload, err := (protocol.Observed{Addr: "198.51.100.4:9", Via: "tcp"}).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var fr protocol.Frame
	fr.Type = protocol.TypeObserved
	fr.Payload = payload
	fr.Src[0] = 1
	c.handleFrame(fr, protocol.PathRelay, nil)
	if len(c.local) != 0 {
		t.Fatal("nonzero src was stored")
	}
	var zero [protocol.KeySize]byte
	fr.Src = zero
	c.handleFrame(fr, protocol.PathDirect, nil)
	if len(c.local) != 0 {
		t.Fatal("direct observed was stored")
	}
	c.handleFrame(fr, protocol.PathRelay, nil)
	if len(c.local) != 1 || c.local[0] != "198.51.100.4:9" {
		t.Fatalf("local %v", c.local)
	}
}

func TestStunOnceIgnoresForeignSource(t *testing.T) {
	silent, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	spoof, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer spoof.Close()
	go func() {
		buf := make([]byte, 256)
		_, from, err := silent.ReadFromUDP(buf)
		if err != nil || from == nil {
			return
		}
		msg := append([]byte(protocol.Magic), []byte("203.0.113.5:9")...)
		dst := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: pc.LocalAddr().(*net.UDPAddr).Port}
		_, _ = spoof.WriteToUDP(msg, dst)
	}()
	obs, err := stunOnce(pc, silent.LocalAddr().String(), bytes.Repeat([]byte{3}, 32))
	if err == nil || obs != "" {
		t.Fatalf("spoofed stun accepted %q %v", obs, err)
	}
}

func TestRedeemOfferRejectsHostMismatch(t *testing.T) {
	real, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	other, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	dev, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	sid := bytes.Repeat([]byte{7}, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"ticket":     "ticket-1",
			"host_pub":   crypto.PublicBase64(other.Public()),
			"session_id": hex.EncodeToString(sid),
		})
	}))
	defer srv.Close()
	offer := protocol.Offer{HubURL: srv.URL, Code: "code", HostPub: real.Public()}
	_, _, _, err = RedeemOfferInfo(context.Background(), nil, offer, dev, "", "", "")
	if err == nil || !strings.Contains(err.Error(), "host key mismatch") {
		t.Fatal(err)
	}
	c := &Conn{cfg: Config{HubURL: srv.URL, Identity: dev}, http: http.DefaultClient}
	_, _, _, err = c.Redeem(context.Background(), offer)
	if err == nil || !strings.Contains(err.Error(), "host key mismatch") {
		t.Fatal(err)
	}
}

func loopbackPort(c *Conn) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ep := range c.local {
		host, port, err := net.SplitHostPort(ep)
		if err != nil || host != "127.0.0.1" {
			continue
		}
		n, _ := strconv.Atoi(port)
		if n > 0 {
			return n
		}
	}
	return 0
}
