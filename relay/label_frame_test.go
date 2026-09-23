package relay

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestLabelFrameIsStoredNotForwarded(t *testing.T) {
	h := New(nil)
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)

	token, hostID := registerBareHost(t, srv, h.Store)
	hostPub := hostID.Public()
	hostConn := dialRelay(t, srv, token)

	devA, ticketA := redeemBareDevice(t, srv, token)
	devB, ticketB := redeemBareDevice(t, srv, token)
	connA := dialDevice(t, srv, ticketA)
	connB := dialDevice(t, srv, ticketB)
	waitPeer(t, h, hostPub)
	waitPeer(t, h, devA.Public())
	waitPeer(t, h, devB.Public())
	// A short read deadline poisons a gorilla socket. Take the one Observed
	// frame the hub writes on accept, and leave the deadline in the future.
	if fr := readFrame(t, connA, time.Second); fr.Type != protocol.TypeObserved {
		t.Fatalf("device A first frame %d", fr.Type)
	}
	if fr := readFrame(t, connB, time.Second); fr.Type != protocol.TypeObserved {
		t.Fatalf("device B first frame %d", fr.Type)
	}

	created := bindingCreated(t, h.Store, hostPub, devA.Public())

	writeLabel(t, hostConn, hostPub, map[string]string{
		"name":  "  desk\tbox  ",
		"model": "not-a-host-field",
	})
	waitHostName(t, h.Store, token, "desk box")
	assertDevice(t, h.Store, hostPub, devA.Public(), "", "")
	assertDevice(t, h.Store, hostPub, devB.Public(), "", "")

	writeLabel(t, hostConn, hostPub, map[string]string{"name": " \n\t "})
	if got := hostName(t, h.Store, token); got != "desk box" {
		t.Fatalf("empty announcement wiped host label %q", got)
	}

	writeLabel(t, hostConn, hostPub, map[string]string{
		"name": strings.Repeat("n", protocol.LabelMaxRunes+5),
	})
	waitHostName(t, h.Store, token, strings.Repeat("n", protocol.LabelMaxRunes))

	// System and model glued into one name must leave the model column empty.
	writeLabel(t, connA, devA.Public(), map[string]string{"name": "  Android\tPixel  "})
	waitDevice(t, h.Store, hostPub, devA.Public(), "Android Pixel", "")
	assertDevice(t, h.Store, hostPub, devB.Public(), "", "")
	if got := hostName(t, h.Store, token); got != strings.Repeat("n", protocol.LabelMaxRunes) {
		t.Fatalf("device announcement rewrote host label %q", got)
	}
	if bindingCreated(t, h.Store, hostPub, devA.Public()) != created {
		t.Fatal("label announcement rewrote binding created time")
	}
	if got := deviceRow(t, h.Store, hostPub, devA.Public()); got.DeviceName == crypto.Fingerprint(devA.Public()) {
		t.Fatal("empty model was filled with the device fingerprint")
	}

	writeLabel(t, connA, devA.Public(), map[string]string{"name": "Android", "model": "Pixel"})
	waitDevice(t, h.Store, hostPub, devA.Public(), "Android", "Pixel")
	assertDevice(t, h.Store, hostPub, devB.Public(), "", "")

	writeLabel(t, connA, devA.Public(), map[string]string{"model": "Pixel 2"})
	waitDevice(t, h.Store, hostPub, devA.Public(), "Android", "Pixel 2")

	writeLabel(t, connA, devA.Public(), map[string]string{"name": "Android Pixel", "model": ""})
	waitDevice(t, h.Store, hostPub, devA.Public(), "Android Pixel", "Pixel 2")

	writeFrame(t, hostConn, protocol.TypePath, hostPub, devA.Public(), []byte{protocol.PathCodeDirect})
	waitLinkPath(t, h, hostPub, devA.Public(), protocol.PathDirect)
	assertDevice(t, h.Store, hostPub, devA.Public(), "Android Pixel", "Pixel 2")
	if got := hostName(t, h.Store, token); got != strings.Repeat("n", protocol.LabelMaxRunes) {
		t.Fatalf("path announcement became a label %q", got)
	}

	writeFrame(t, hostConn, protocol.TypeHandshake, hostPub, devA.Public(), []byte("from-handshake-hostname"))
	gotHS := readFrame(t, connA, time.Second)
	if gotHS.Type != protocol.TypeHandshake || string(gotHS.Payload) != "from-handshake-hostname" {
		t.Fatalf("handshake not forwarded: type=%d", gotHS.Type)
	}
	if got := hostName(t, h.Store, token); strings.Contains(got, "handshake") {
		t.Fatalf("handshake became a host label %q", got)
	}

	writeFrame(t, hostConn, protocol.TypeData, hostPub, devA.Public(), []byte(`{"name":"from-data","model":"from-data"}`))
	gotData := readFrame(t, connA, time.Second)
	if gotData.Type != protocol.TypeData {
		t.Fatalf("data frame not forwarded: type=%d", gotData.Type)
	}
	assertDevice(t, h.Store, hostPub, devA.Public(), "Android Pixel", "Pixel 2")
	if got := hostName(t, h.Store, token); strings.Contains(got, "from-data") {
		t.Fatalf("data payload became a host label %q", got)
	}

	// Destination is the other device. A regression that forwards TypeLabel
	// would deliver this frame; an unbound zero destination would not.
	renamed, err := json.Marshal(map[string]string{"name": "renamed-unit", "model": "m9"})
	if err != nil {
		t.Fatal(err)
	}
	writeFrame(t, connA, protocol.TypeLabel, devA.Public(), devB.Public(), renamed)
	waitDevice(t, h.Store, hostPub, devA.Public(), "renamed-unit", "m9")
	writeLabel(t, connA, devA.Public(), map[string]string{"name": "", "model": ""})
	time.Sleep(50 * time.Millisecond)
	assertDevice(t, h.Store, hostPub, devA.Public(), "renamed-unit", "m9")
	assertDevice(t, h.Store, hostPub, devB.Public(), "", "")
	if fr, ok := readFrameOptional(connB, 150*time.Millisecond); ok && fr.Type == protocol.TypeLabel {
		t.Fatal("label frame was forwarded")
	}
	for _, payload := range h.SniffedPayloads() {
		if strings.Contains(string(payload), "renamed-unit") {
			t.Fatal("label payload was treated as application data")
		}
	}

	var wrong [32]byte
	wrong[0] = 0xff
	writeFrame(t, connA, protocol.TypeLabel, wrong[:], hostPub, []byte(`{"name":"spoofed","model":"spoofed"}`))
	time.Sleep(50 * time.Millisecond)
	assertDevice(t, h.Store, hostPub, devA.Public(), "renamed-unit", "m9")

	writeFrame(t, connA, protocol.TypeLabel, devA.Public(), hostPub, []byte{protocol.PathCodeDirect})
	time.Sleep(50 * time.Millisecond)
	assertDevice(t, h.Store, hostPub, devA.Public(), "renamed-unit", "m9")
	if h.LinkPath(hostPub, devA.Public()) != protocol.PathDirect {
		t.Fatal("rejected label payload changed the path")
	}
}

func registerBareHost(t *testing.T, srv *httptest.Server, st store.Store) (string, *crypto.Identity) {
	t.Helper()
	ctx := context.Background()
	token, err := IssueHostToken(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	id, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := postRegister(srv.URL, token, id, ""); err != nil {
		t.Fatal(err)
	}
	return token, id
}

func redeemBareDevice(t *testing.T, srv *httptest.Server, token string) (*crypto.Identity, string) {
	t.Helper()
	id, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	code := mintPairing(t, srv.URL, token)
	body, _ := json.Marshal(map[string]string{
		"code":       code,
		"device_pub": crypto.PublicBase64(id.Public()),
	})
	res, err := http.Post(srv.URL+"/pairlink/v1/pairings/redeem", "application/json", bytes.NewReader(body))
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
		t.Fatalf("redeem %s", res.Status)
	}
	return id, out.Ticket
}

func writeLabel(t *testing.T, conn *websocket.Conn, src []byte, fields map[string]string) {
	t.Helper()
	payload, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	writeFrame(t, conn, protocol.TypeLabel, src, nil, payload)
}

func waitHostName(t *testing.T, st store.Store, token, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	var got string
	for time.Now().Before(deadline) {
		got = hostName(t, st, token)
		if got == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("host label %q, want %q", got, want)
}

func hostName(t *testing.T, st store.Store, token string) string {
	t.Helper()
	got, err := st.HostByTokenHash(context.Background(), store.HashSecret(token))
	if err != nil {
		t.Fatal(err)
	}
	return got.Name
}

func waitDevice(t *testing.T, st store.Store, hostPub, devPub []byte, name, model string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	var got store.Binding
	for time.Now().Before(deadline) {
		got = deviceRow(t, st, hostPub, devPub)
		if got.DeviceName == name && got.DeviceModel == model {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("device label name=%q model=%q, want %q %q", got.DeviceName, got.DeviceModel, name, model)
}

func assertDevice(t *testing.T, st store.Store, hostPub, devPub []byte, name, model string) {
	t.Helper()
	got := deviceRow(t, st, hostPub, devPub)
	if got.DeviceName != name || got.DeviceModel != model {
		t.Fatalf("device label name=%q model=%q, want %q %q", got.DeviceName, got.DeviceModel, name, model)
	}
}

func deviceRow(t *testing.T, st store.Store, hostPub, devPub []byte) store.Binding {
	t.Helper()
	got, err := st.BindingByPeers(context.Background(), hostPub, devPub)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func bindingCreated(t *testing.T, st store.Store, hostPub, devPub []byte) time.Time {
	t.Helper()
	return deviceRow(t, st, hostPub, devPub).Created
}

func readFrame(t *testing.T, conn *websocket.Conn, d time.Duration) protocol.Frame {
	t.Helper()
	fr, ok := readFrameOptional(conn, d)
	if !ok {
		t.Fatal("expected a frame")
	}
	return fr
}

func readFrameOptional(conn *websocket.Conn, d time.Duration) (protocol.Frame, bool) {
	_ = conn.SetReadDeadline(time.Now().Add(d))
	_, data, err := conn.ReadMessage()
	if err != nil {
		return protocol.Frame{}, false
	}
	fr, err := protocol.UnmarshalFrame(data)
	if err != nil {
		return protocol.Frame{}, false
	}
	return fr, true
}
