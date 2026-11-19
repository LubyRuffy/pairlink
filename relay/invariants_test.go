package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/LubyRuffy/pairlink/store"
	"github.com/gorilla/websocket"
)

func registerNamed(t *testing.T, h *Hub, srv *httptest.Server, name string) (string, *crypto.Identity) {
	t.Helper()
	token, err := IssueHostToken(context.Background(), h.Store)
	if err != nil {
		t.Fatal(err)
	}
	id, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := postRegister(srv.URL, token, id, name); err != nil {
		t.Fatal(err)
	}
	return token, id
}

func redeemRaw(t *testing.T, hub, code, devicePub string) (int, map[string]string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"code": code, "device_pub": devicePub})
	res, err := http.Post(hub+"/pairlink/v1/pairings/redeem", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]string
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func TestOfflineRedeemDoesNotBurnCode(t *testing.T) {
	h := New(nil)
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	token, _ := registerNamed(t, h, srv, "pc")
	code := mintPairing(t, srv.URL, token)
	dev, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	status, _ := redeemRaw(t, srv.URL, code, crypto.PublicBase64(dev.Public()))
	if status != http.StatusConflict {
		t.Fatalf("offline status %d", status)
	}
	_ = dialRelay(t, srv, token)
	status, out := redeemRaw(t, srv.URL, code, crypto.PublicBase64(dev.Public()))
	if status != http.StatusOK || out["ticket"] == "" {
		t.Fatalf("code was consumed: %d", status)
	}
}

func TestFullTableRedeemDoesNotBurnCode(t *testing.T) {
	h := New(nil)
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	token, _ := registerNamed(t, h, srv, "pc")
	_ = dialRelay(t, srv, token)
	var first string
	for i := 0; i < store.MaxBindingsPerHost; i++ {
		dev, err := crypto.Generate()
		if err != nil {
			t.Fatal(err)
		}
		code := mintPairing(t, srv.URL, token)
		status, out := redeemRaw(t, srv.URL, code, crypto.PublicBase64(dev.Public()))
		if status != http.StatusOK {
			t.Fatalf("bind %d status %d", i, status)
		}
		if i == 0 {
			first = out["binding_id"]
		}
	}
	extraDev, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	extra := mintPairing(t, srv.URL, token)
	status, _ := redeemRaw(t, srv.URL, extra, crypto.PublicBase64(extraDev.Public()))
	if status != http.StatusConflict {
		t.Fatalf("over cap %d", status)
	}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/pairlink/v1/bindings/"+first+"/revoke", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("revoke %d", res.StatusCode)
	}
	status, out := redeemRaw(t, srv.URL, extra, crypto.PublicBase64(extraDev.Public()))
	if status != http.StatusOK || out["ticket"] == "" {
		t.Fatalf("freed slot still rejected %d", status)
	}
}

func TestRedeemHostKeyDoesNotReplaceHost(t *testing.T) {
	h := New(nil)
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	token, hostID := registerNamed(t, h, srv, "pc")
	_ = dialRelay(t, srv, token)
	waitPeer(t, h, hostID.Public())
	code := mintPairing(t, srv.URL, token)
	status, _ := redeemRaw(t, srv.URL, code, crypto.PublicBase64(hostID.Public()))
	if status == http.StatusOK {
		t.Fatal("host key redeemed")
	}
	if !h.hostConnected(hostID.Public()) {
		t.Fatal("host socket was replaced")
	}
	dev, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	status, out := redeemRaw(t, srv.URL, code, crypto.PublicBase64(dev.Public()))
	if status != http.StatusOK || out["ticket"] == "" {
		t.Fatalf("real device could not use the code: %d", status)
	}
}

func TestSecondTokenCannotRegisterTakenPub(t *testing.T) {
	h := New(nil)
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	token, hostID := registerNamed(t, h, srv, "victim-name")
	other, err := IssueHostToken(context.Background(), h.Store)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{
		"pub":   crypto.PublicBase64(hostID.Public()),
		"token": other,
		"name":  "stolen",
	})
	res, err := http.Post(srv.URL+"/pairlink/v1/hosts", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("status %d", res.StatusCode)
	}
	got, err := h.Store.HostByTokenHash(context.Background(), store.HashSecret(token))
	if err != nil || got.Name != "victim-name" {
		t.Fatalf("owner %+v %v", got, err)
	}
}

func TestTraceIsScopedToTheCaller(t *testing.T) {
	h := New(nil)
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	token, hostID := registerNamed(t, h, srv, "owner")
	other, otherID := registerNamed(t, h, srv, "other")
	fp := crypto.Fingerprint(hostID.Public())
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/pairlink/v1/trace/"+fp, nil)
	req.Header.Set("Authorization", "Bearer "+other)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("other host %d", res.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/pairlink/v1/trace/"+fp, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(raw), "register") {
		t.Fatalf("owner %d", res.StatusCode)
	}
	_ = otherID
}

func TestRepairRetiresOldTicketAndDelivers(t *testing.T) {
	h := New(nil)
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	token, oldTicket, hostConn := issueBoundTicket(t, h, srv)
	oldConn := dialDevice(t, srv, oldTicket)
	b, err := h.Store.BindingByTicketHash(context.Background(), store.HashSecret(oldTicket))
	if err != nil {
		t.Fatal(err)
	}
	code := mintPairing(t, srv.URL, token)
	status, out := redeemRaw(t, srv.URL, code, crypto.PublicBase64(b.DevicePub))
	if status != http.StatusOK || out["ticket"] == "" {
		t.Fatalf("re-pair %d", status)
	}
	readClosedWithin(t, oldConn, time.Second)
	if _, err := h.Store.BindingByTicketHash(context.Background(), store.HashSecret(oldTicket)); err != store.ErrRevoked {
		t.Fatalf("old ticket %v", err)
	}
	d := websocket.Dialer{Subprotocols: []string{"pairlink.ticket." + oldTicket}}
	if conn, resp, err := d.Dial("ws"+srv.URL[len("http"):]+"/pairlink/v1/ws", nil); err == nil {
		_ = conn.Close()
		t.Fatal("revoked ticket connected")
	} else if resp != nil {
		resp.Body.Close()
	}
	newConn := dialDevice(t, srv, out["ticket"])
	waitPeer(t, h, b.DevicePub)
	writeFrame(t, hostConn, protocol.TypeData, b.HostPub, b.DevicePub, []byte("alive"))
	deadline := time.Now().Add(time.Second)
	for {
		fr, ok := readFrameOptional(newConn, time.Until(deadline))
		if !ok {
			t.Fatal("new binding got no data")
		}
		if fr.Type == protocol.TypeData && string(fr.Payload) == "alive" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("new binding got no data")
		}
	}
}

func TestRevokeClosesDeviceSocket(t *testing.T) {
	h := New(nil)
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	token, ticket, _ := issueBoundTicket(t, h, srv)
	devConn := dialDevice(t, srv, ticket)
	b, err := h.Store.BindingByTicketHash(context.Background(), store.HashSecret(ticket))
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/pairlink/v1/bindings/"+b.ID+"/revoke", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("revoke %d", res.StatusCode)
	}
	readClosedWithin(t, devConn, time.Second)
}

func TestPingsDoNotHoldTheIdleDeadline(t *testing.T) {
	h := New(nil)
	h.Idle = 300 * time.Millisecond
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	token, hostID := registerNamed(t, h, srv, "pc")
	conn := dialRelay(t, srv, token)
	waitPeer(t, h, hostID.Public())
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.WriteControl(websocket.PingMessage, []byte("x"), time.Now().Add(time.Second))
		time.Sleep(100 * time.Millisecond)
		if !h.hostConnected(hostID.Public()) {
			return
		}
	}
	t.Fatal("pings kept the host socket")
}
