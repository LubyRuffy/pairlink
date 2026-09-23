package client

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
)

func httpClient(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return http.DefaultClient
}

func postJSON(ctx context.Context, hc *http.Client, hub, token, path string, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(hub, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := httpClient(hc).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return fmt.Errorf("client: %s: %s", res.Status, b)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(b, out)
}

func getJSON(ctx context.Context, hc *http.Client, hub, token, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(hub, "/")+path, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := httpClient(hc).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return fmt.Errorf("client: %s: %s", res.Status, b)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(b, out)
}

// RegisterHost binds a long-term public key to a Host Token before opening WS.
func RegisterHost(ctx context.Context, hubURL, token string, id *crypto.Identity) error {
	return RegisterHostLabel(ctx, hubURL, token, "", id)
}

// RegisterHostLabel is RegisterHost plus the host's own name. An empty label
// leaves a name the hub already stored.
func RegisterHostLabel(ctx context.Context, hubURL, token, label string, id *crypto.Identity) error {
	return registerHost(ctx, nil, hubURL, token, label, id)
}

// RegisterHostLabelHTTP is RegisterHostLabel with a caller-supplied client
// (TLS roots, timeouts). hc may be nil.
func RegisterHostLabelHTTP(ctx context.Context, hc *http.Client, hubURL, token, label string, id *crypto.Identity) error {
	return registerHost(ctx, hc, hubURL, token, label, id)
}

func registerHost(ctx context.Context, hc *http.Client, hubURL, token, label string, id *crypto.Identity) error {
	body := map[string]string{
		"pub":   crypto.PublicBase64(id.Public()),
		"token": token,
	}
	if label = strings.TrimSpace(label); label != "" {
		body["name"] = label
	}
	return postJSON(ctx, hc, hubURL, token, "/pairlink/v1/hosts", body, nil)
}

// RedeemOffer spends the QR pairing code. Call this before Dial with the ticket.
func RedeemOffer(ctx context.Context, offer protocol.Offer, device *crypto.Identity) (ticket string, hostPub, sessionID []byte, err error) {
	return RedeemOfferLabel(ctx, offer, "", device)
}

// RedeemOfferLabel is RedeemOffer plus the device's own name.
func RedeemOfferLabel(ctx context.Context, offer protocol.Offer, label string, device *crypto.Identity) (ticket string, hostPub, sessionID []byte, err error) {
	return RedeemOfferInfo(ctx, nil, offer, device, label, "")
}

// RedeemOfferInfo spends a pairing code and stores the device name and model.
// hc may be nil. Empty labels are omitted.
func RedeemOfferInfo(ctx context.Context, hc *http.Client, offer protocol.Offer, device *crypto.Identity, name, model string) (ticket string, hostPub, sessionID []byte, err error) {
	var resp struct {
		Ticket    string `json:"ticket"`
		HostPub   string `json:"host_pub"`
		SessionID string `json:"session_id"`
	}
	body := map[string]string{
		"code":       offer.Code,
		"device_pub": crypto.PublicBase64(device.Public()),
	}
	if name = strings.TrimSpace(name); name != "" {
		body["name"] = name
	}
	if model = strings.TrimSpace(model); model != "" {
		body["model"] = model
	}
	if err = postJSON(ctx, hc, offer.HubURL, "", "/pairlink/v1/pairings/redeem", body, &resp); err != nil {
		return "", nil, nil, err
	}
	hostPub, err = crypto.ParsePublic(resp.HostPub)
	if err != nil {
		return "", nil, nil, err
	}
	sessionID, err = hex.DecodeString(resp.SessionID)
	return resp.Ticket, hostPub, sessionID, err
}

type BindingView struct {
	ID          string `json:"id"`
	DeviceFP    string `json:"device_fp"`
	DeviceName  string `json:"device_name,omitempty"`
	DeviceModel string `json:"device_model,omitempty"`
	CreatedAt   string `json:"created_at"`
	SessionID   string `json:"session_id"`
	Online      bool   `json:"online"`
	Path        string `json:"path"`
}

func ListBindings(ctx context.Context, hubURL, token string) ([]BindingView, error) {
	var resp struct {
		Bindings []BindingView `json:"bindings"`
	}
	if err := getJSON(ctx, nil, hubURL, token, "/pairlink/v1/bindings", &resp); err != nil {
		return nil, err
	}
	if resp.Bindings == nil {
		resp.Bindings = []BindingView{}
	}
	return resp.Bindings, nil
}

func RevokeBinding(ctx context.Context, hubURL, token, id string) error {
	return postJSON(ctx, nil, hubURL, token, "/pairlink/v1/bindings/"+id+"/revoke", map[string]string{}, nil)
}
