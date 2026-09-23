package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/LubyRuffy/pairlink/store"
	"github.com/gorilla/websocket"
)

func TestHostAndDeviceLabelsRoundTrip(t *testing.T) {
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
	first := "  alpha\tbeta  "
	if err := postRegister(srv.URL, token, hostID, first); err != nil {
		t.Fatal(err)
	}
	got, err := st.HostByTokenHash(ctx, store.HashSecret(token))
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "alpha beta" {
		t.Fatalf("stored host label %q", got.Name)
	}
	if err := postRegister(srv.URL, token, hostID, " \n "); err != nil {
		t.Fatal(err)
	}
	got, err = st.HostByTokenHash(ctx, store.HashSecret(token))
	if err != nil || got.Name != "alpha beta" {
		t.Fatalf("empty label wiped %q %v", got.Name, err)
	}
	if err := postRegister(srv.URL, token, hostID, "other-box"); err != nil {
		t.Fatal(err)
	}
	got, err = st.HostByTokenHash(ctx, store.HashSecret(token))
	if err != nil || got.Name != "other-box" {
		t.Fatalf("replaced host label %q %v", got.Name, err)
	}
	long := strings.Repeat("n", protocol.LabelMaxRunes+5)
	if err := postRegister(srv.URL, token, hostID, long); err != nil {
		t.Fatal(err)
	}
	got, err = st.HostByTokenHash(ctx, store.HashSecret(token))
	if err != nil || got.Name != strings.Repeat("n", protocol.LabelMaxRunes) {
		t.Fatalf("clipped host label len=%d %v", len([]rune(got.Name)), err)
	}

	hostWS := "ws" + strings.TrimPrefix(srv.URL, "http") + "/pairlink/v1/ws"
	hdr := http.Header{}
	hdr.Set("Authorization", "Bearer "+token)
	conn, resp, err := websocket.DefaultDialer.Dial(hostWS, hdr)
	if err != nil {
		t.Fatalf("host ws: %v", err)
	}
	if resp != nil {
		resp.Body.Close()
	}
	t.Cleanup(func() { _ = conn.Close() })

	code := mintPairing(t, srv.URL, token)
	devNamed, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	redeem(t, srv.URL, code, devNamed, "  pocket\nunit ")
	code = mintPairing(t, srv.URL, token)
	devPlain, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	// Whatever string the client sent is the label, including a hostname.
	redeem(t, srv.URL, code, devPlain, "hub.example")
	code = mintPairing(t, srv.URL, token)
	devBlank, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	redeem(t, srv.URL, code, devBlank, "")

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/pairlink/v1/bindings", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		Bindings []struct {
			DeviceFP   string `json:"device_fp"`
			DeviceName string `json:"device_name"`
		} `json:"bindings"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	byFP := map[string]string{}
	for _, row := range body.Bindings {
		byFP[row.DeviceFP] = row.DeviceName
	}
	if byFP[crypto.Fingerprint(devNamed.Public())] != "pocket unit" {
		t.Fatalf("named device %+v", byFP)
	}
	if byFP[crypto.Fingerprint(devPlain.Public())] != "hub.example" {
		t.Fatalf("literal device %+v", byFP)
	}
	if name, ok := byFP[crypto.Fingerprint(devBlank.Public())]; !ok || name != "" {
		t.Fatalf("blank device %+v ok=%v", byFP, ok)
	}
}

func postRegister(hub, token string, id *crypto.Identity, name string) error {
	body, _ := json.Marshal(map[string]string{
		"pub":   crypto.PublicBase64(id.Public()),
		"token": token,
		"name":  name,
	})
	res, err := http.Post(hub+"/pairlink/v1/hosts", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("register %s", res.Status)
	}
	return nil
}

func mintPairing(t *testing.T, hub, token string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, hub+"/pairlink/v1/pairings", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var pairing struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(res.Body).Decode(&pairing); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK || pairing.Code == "" {
		t.Fatalf("pairing %s %q", res.Status, pairing.Code)
	}
	return pairing.Code
}

func redeem(t *testing.T, hub, code string, dev *crypto.Identity, name string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"code":       code,
		"device_pub": crypto.PublicBase64(dev.Public()),
		"name":       name,
	})
	res, err := http.Post(hub+"/pairlink/v1/pairings/redeem", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("redeem %s", res.Status)
	}
}
