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
	return postJSON(ctx, nil, hubURL, token, "/pairlink/v1/hosts", map[string]string{
		"pub":   crypto.PublicBase64(id.Public()),
		"token": token,
	}, nil)
}

// RedeemOffer spends the QR pairing code. Call this before Dial with the ticket.
func RedeemOffer(ctx context.Context, offer protocol.Offer, device *crypto.Identity) (ticket string, hostPub, sessionID []byte, err error) {
	var resp struct {
		Ticket    string `json:"ticket"`
		HostPub   string `json:"host_pub"`
		SessionID string `json:"session_id"`
	}
	if err = postJSON(ctx, nil, offer.HubURL, "", "/pairlink/v1/pairings/redeem", map[string]string{
		"code":       offer.Code,
		"device_pub": crypto.PublicBase64(device.Public()),
	}, &resp); err != nil {
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
	ID        string `json:"id"`
	DeviceFP  string `json:"device_fp"`
	CreatedAt string `json:"created_at"`
	SessionID string `json:"session_id"`
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
