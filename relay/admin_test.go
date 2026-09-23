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
	"github.com/gorilla/websocket"
)

func TestAdminDisabled(t *testing.T) {
	h := New(nil)
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	for _, path := range []string{"/pairlink/admin", "/pairlink/v1/admin/snapshot"} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("%s %d", path, res.StatusCode)
		}
	}
}

func TestAdminSnapshotShowsLabelsAndPath(t *testing.T) {
	h := New(nil)
	const admin = "admin-secret"
	h.SetAdminToken(admin)
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	ctx := context.Background()

	res, err := http.Get(srv.URL + "/pairlink/admin")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !bytes.Contains(page, []byte("snapshot")) {
		t.Fatalf("page %d", res.StatusCode)
	}

	if res, err = http.Get(srv.URL + "/pairlink/v1/admin/snapshot"); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("snapshot %d", res.StatusCode)
	}

	token, err := IssueHostToken(ctx, h.Store)
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := postRegister(srv.URL, token, hostID, "box-a"); err != nil {
		t.Fatal(err)
	}
	hostConn := dialHost(t, srv.URL, token)
	code := mintPairing(t, srv.URL, token)
	devID, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	ticket := redeemMeta(t, srv.URL, code, devID, "dev-a", "mod-a")
	devConn := dialDeviceTicket(t, srv.URL, ticket)
	writePath(t, devConn, devID.Public(), hostID.Public(), protocol.PathCodeDirect)

	snap := waitSnapshot(t, srv.URL, admin, func(s snapshotBody) bool {
		return len(s.Bindings) == 1 && s.Bindings[0].Path == protocol.PathDirect && s.Bindings[0].Online
	})
	if len(snap.Hosts) != 1 || snap.Hosts[0].Name != "box-a" || !snap.Hosts[0].Online || !snap.Hosts[0].Registered {
		t.Fatalf("hosts %+v", snap.Hosts)
	}
	b := snap.Bindings[0]
	if b.DeviceName != "dev-a" || b.DeviceModel != "mod-a" || b.HostName != "box-a" || b.Path != protocol.PathDirect {
		t.Fatalf("binding %+v", b)
	}
	raw := mustSnapshotRaw(t, srv.URL, admin)
	if strings.Contains(raw, token) || strings.Contains(raw, ticket) || strings.Contains(raw, code) {
		t.Fatal("snapshot leaked a secret")
	}

	writeData(t, devConn, devID.Public(), hostID.Public(), []byte("not-a-label"))
	time.Sleep(30 * time.Millisecond)
	got, err := h.Store.HostByPub(ctx, hostID.Public())
	if err != nil || got.Name != "box-a" {
		t.Fatalf("type data changed label %q %v", got.Name, err)
	}

	writePath(t, devConn, devID.Public(), hostID.Public(), protocol.PathCodeRelay)
	snap = waitSnapshot(t, srv.URL, admin, func(s snapshotBody) bool {
		return len(s.Bindings) == 1 && s.Bindings[0].Path == protocol.PathRelay
	})
	if snap.Bindings[0].Path != protocol.PathRelay {
		t.Fatalf("path %+v", snap.Bindings[0])
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/pairlink/v1/admin/bindings/"+b.ID+"/revoke", nil)
	req.Header.Set("Authorization", "Bearer "+admin)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("revoke %d", res.StatusCode)
	}
	snap = waitSnapshot(t, srv.URL, admin, func(s snapshotBody) bool {
		return len(s.Bindings) == 1 && s.Bindings[0].Revoked
	})
	if !snap.Bindings[0].Revoked {
		t.Fatal("not revoked")
	}

	evRes := adminGet(t, srv.URL, admin, "/pairlink/v1/admin/trace/"+b.SessionID)
	if evRes.StatusCode != http.StatusOK {
		t.Fatalf("trace %d", evRes.StatusCode)
	}
	evRes.Body.Close()

	issued := adminPost(t, srv.URL, admin, "/pairlink/v1/admin/hosts")
	var issuedBody struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(issued.Body).Decode(&issuedBody); err != nil {
		t.Fatal(err)
	}
	issued.Body.Close()
	if issued.StatusCode != http.StatusOK || issuedBody.Token == "" {
		t.Fatalf("issue %d", issued.StatusCode)
	}
	after := mustSnapshotRaw(t, srv.URL, admin)
	if strings.Contains(after, issuedBody.Token) {
		t.Fatal("snapshot stored the new host token")
	}
	_ = hostConn
}

type snapshotBody struct {
	Hosts []struct {
		Name       string `json:"name"`
		Online     bool   `json:"online"`
		Registered bool   `json:"registered"`
	} `json:"hosts"`
	Bindings []struct {
		ID          string `json:"id"`
		HostName    string `json:"host_name"`
		DeviceName  string `json:"device_name"`
		DeviceModel string `json:"device_model"`
		Online      bool   `json:"online"`
		Path        string `json:"path"`
		Revoked     bool   `json:"revoked"`
		SessionID   string `json:"session_id"`
	} `json:"bindings"`
}

func waitSnapshot(t *testing.T, hub, admin string, ok func(snapshotBody) bool) snapshotBody {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last snapshotBody
	for {
		last = readSnapshot(t, hub, admin)
		if ok(last) {
			return last
		}
		if time.Now().After(deadline) {
			t.Fatalf("snapshot %+v", last)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func readSnapshot(t *testing.T, hub, admin string) snapshotBody {
	t.Helper()
	res := adminGet(t, hub, admin, "/pairlink/v1/admin/snapshot")
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("snapshot %d", res.StatusCode)
	}
	var body snapshotBody
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

func mustSnapshotRaw(t *testing.T, hub, admin string) string {
	t.Helper()
	res := adminGet(t, hub, admin, "/pairlink/v1/admin/snapshot")
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return string(b)
}

func adminGet(t *testing.T, hub, admin, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, hub+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+admin)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func adminPost(t *testing.T, hub, admin, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, hub+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+admin)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func dialHost(t *testing.T, hub, token string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(hub, "http") + "/pairlink/v1/ws"
	hdr := http.Header{}
	hdr.Set("Authorization", "Bearer "+token)
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, hdr)
	if err != nil {
		t.Fatal(err)
	}
	if resp != nil {
		resp.Body.Close()
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func dialDeviceTicket(t *testing.T, hub, ticket string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(hub, "http") + "/pairlink/v1/ws"
	d := websocket.Dialer{Subprotocols: []string{"pairlink.ticket." + ticket}}
	conn, resp, err := d.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp != nil {
		resp.Body.Close()
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func redeemMeta(t *testing.T, hub, code string, dev *crypto.Identity, name, model string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"code": code, "device_pub": crypto.PublicBase64(dev.Public()),
		"name": name, "model": model,
	})
	res, err := http.Post(hub+"/pairlink/v1/pairings/redeem", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		Ticket string `json:"ticket"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK || out.Ticket == "" {
		t.Fatalf("redeem %d", res.StatusCode)
	}
	return out.Ticket
}

func writePath(t *testing.T, conn *websocket.Conn, src, dst []byte, code byte) {
	t.Helper()
	writeAdminFrame(t, conn, protocol.TypePath, src, dst, []byte{code})
}

func writeData(t *testing.T, conn *websocket.Conn, src, dst, payload []byte) {
	t.Helper()
	writeAdminFrame(t, conn, protocol.TypeData, src, dst, payload)
}

func writeAdminFrame(t *testing.T, conn *websocket.Conn, typ byte, src, dst, payload []byte) {
	t.Helper()
	var fr protocol.Frame
	fr.Type = typ
	copy(fr.Src[:], src)
	copy(fr.Dst[:], dst)
	fr.Payload = payload
	raw, err := fr.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, raw); err != nil {
		t.Fatal(err)
	}
}
