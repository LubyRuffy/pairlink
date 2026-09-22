package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/store"
	"github.com/gorilla/websocket"
)

func dialRelay(t *testing.T, srv *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	url := "ws" + srv.URL[len("http"):] + "/pairlink/v1/ws"
	hdr := http.Header{}
	hdr.Set("Authorization", "Bearer "+token)
	conn, resp, err := websocket.DefaultDialer.Dial(url, hdr)
	if resp != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial relay: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func dialDevice(t *testing.T, srv *httptest.Server, ticket string) *websocket.Conn {
	t.Helper()
	url := "ws" + srv.URL[len("http"):] + "/pairlink/v1/ws"
	d := websocket.Dialer{Subprotocols: []string{"pairlink.ticket." + ticket}}
	conn, resp, err := d.Dial(url, nil)
	if resp != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial device: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func issueBoundTicket(t *testing.T, h *Hub, srv *httptest.Server) (token, ticket string, hostConn *websocket.Conn) {
	t.Helper()
	ctx := context.Background()
	token, err := IssueHostToken(ctx, h.Store)
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
	if res.StatusCode != http.StatusOK {
		t.Fatalf("register %s", res.Status)
	}
	hostConn = dialRelay(t, srv, token)
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
	return token, out.Ticket, hostConn
}

func waitPeer(t *testing.T, h *Hub, pub []byte) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, ok := h.lookup(pub); ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("peer did not join the relay table")
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func readClosedWithin(t *testing.T, conn *websocket.Conn, bound time.Duration) {
	t.Helper()
	deadline := time.Now().Add(bound)
	for {
		_ = conn.SetReadDeadline(deadline)
		_, _, err := conn.ReadMessage()
		if err == nil {
			if time.Now().After(deadline) {
				t.Fatal("relay socket stayed open")
			}
			continue
		}
		if isTimeout(err) {
			t.Fatal("relay socket stayed open")
		}
		return
	}
}

func TestClosePeersDropsHostAndDevice(t *testing.T) {
	h := New(nil)
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	_, ticket, hostConn := issueBoundTicket(t, h, srv)
	devConn := dialDevice(t, srv, ticket)
	b, err := h.Store.BindingByTicketHash(context.Background(), store.HashSecret(ticket))
	if err != nil {
		t.Fatal(err)
	}
	waitPeer(t, h, b.HostPub)
	waitPeer(t, h, b.DevicePub)
	h.ClosePeers()
	readClosedWithin(t, hostConn, time.Second)
	readClosedWithin(t, devConn, time.Second)
}

func TestDeviceWithoutHostIsNotLeftUpgraded(t *testing.T) {
	h := New(nil)
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	_, ticket, hostConn := issueBoundTicket(t, h, srv)
	_ = hostConn.Close()
	deadline := time.Now().Add(time.Second)
	var hostPub []byte
	for time.Now().Before(deadline) {
		b, err := h.Store.BindingByTicketHash(context.Background(), store.HashSecret(ticket))
		if err != nil {
			t.Fatal(err)
		}
		hostPub = b.HostPub
		if _, ok := h.lookup(hostPub); !ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, ok := h.lookup(hostPub); ok {
		t.Fatal("host socket did not leave the table")
	}
	devConn := dialDevice(t, srv, ticket)
	readClosedWithin(t, devConn, time.Second)
}

func TestDeviceStaysUpWhileHostIsOnline(t *testing.T) {
	h := New(nil)
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	_, ticket, _ := issueBoundTicket(t, h, srv)
	devConn := dialDevice(t, srv, ticket)
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		_ = devConn.SetReadDeadline(deadline)
		_, _, err := devConn.ReadMessage()
		if err == nil {
			continue
		}
		if isTimeout(err) {
			return
		}
		t.Fatalf("host-online device must stay upgraded, got %v", err)
	}
}
