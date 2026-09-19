package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/gorilla/websocket"
)

func TestDeviceTicketSubprotocolOpensWS(t *testing.T) {
	// Browser / Capacitor cannot set Authorization on WebSocket. The ticket
	// must travel as a subprotocol, never as a query string that lands in
	// access logs.
	st := New(nil).Store
	h := New(st)
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	ctx := context.Background()

	token, err := IssueHostToken(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	devID, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{
		"pub":   crypto.PublicBase64(hostID.Public()),
		"token": token,
	})
	res, err := http.Post(srv.URL+"/pairlink/v1/hosts", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("register %s", res.Status)
	}

	hostWS := "ws" + srv.URL[len("http"):] + "/pairlink/v1/ws"
	hostHdr := http.Header{}
	hostHdr.Set("Authorization", "Bearer "+token)
	hostConn, hostResp, err := websocket.DefaultDialer.Dial(hostWS, hostHdr)
	if err != nil {
		t.Fatalf("host ws: %v", err)
	}
	if hostResp != nil {
		hostResp.Body.Close()
	}
	t.Cleanup(func() { _ = hostConn.Close() })

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/pairlink/v1/pairings", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var pairing struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(res.Body).Decode(&pairing); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("pairing %s", res.Status)
	}

	redeem, _ := json.Marshal(map[string]string{
		"code":       pairing.Code,
		"device_pub": crypto.PublicBase64(devID.Public()),
	})
	res, err = http.Post(srv.URL+"/pairlink/v1/pairings/redeem", "application/json", bytes.NewReader(redeem))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Ticket string `json:"ticket"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if out.Ticket == "" {
		t.Fatal("empty ticket")
	}

	wsURL := "ws" + srv.URL[len("http"):] + "/pairlink/v1/ws"
	d := websocket.Dialer{Subprotocols: []string{"pairlink.ticket." + out.Ticket}}
	conn, resp, err := d.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("subprotocol dial: %v", err)
	}
	if resp != nil {
		resp.Body.Close()
	}
	t.Cleanup(func() { _ = conn.Close() })
	if conn.Subprotocol() != "pairlink.ticket."+out.Ticket {
		t.Fatalf("echoed protocol %q", conn.Subprotocol())
	}

	bad := websocket.Dialer{Subprotocols: []string{"pairlink.ticket.deadbeef"}}
	_, resp, err = bad.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("forged ticket must not open")
	}
	if resp != nil {
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status %d", resp.StatusCode)
		}
	}
}

func TestRedeemOPTIONSAllowsCapacitorWebView(t *testing.T) {
	// Android WebView fetch() preflights POST JSON. No CORS used to 405 OPTIONS
	// and the phone showed "Failed to fetch" with a valid pairing URI in the box.
	h := New(nil)
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodOptions, srv.URL+"/pairlink/v1/pairings/redeem", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://localhost")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("OPTIONS status %d", res.StatusCode)
	}
	if got := res.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Allow-Origin %q", got)
	}
	if !strings.Contains(res.Header.Get("Access-Control-Allow-Methods"), "POST") {
		t.Fatalf("Allow-Methods %q", res.Header.Get("Access-Control-Allow-Methods"))
	}

	post, err := http.Post(srv.URL+"/pairlink/v1/pairings/redeem", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	post.Body.Close()
	if post.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("POST redeem must still carry CORS so the WebView can read the error body")
	}
}
