// Package protocol is the versioned pairlink wire: QR offers, frames, disco.
package protocol

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
)

const (
	// Scheme is the QR / paste URI prefix. Frozen: scanners look for this.
	Scheme = "pairlink:v1:"
	// DefaultPairingTTL is how long a scan code stays redeemable.
	DefaultPairingTTL = 10 * 60 // seconds, documented here so clients agree
)

// Offer is the scan-to-bind payload. HubURL is whatever the host was
// configured with — never a compiled-in domain.
type Offer struct {
	HubURL  string
	Code    string
	HostPub []byte
	LAN     []string
}

// Encode writes pairlink:v1:<hub_url>:<pairing_code>:<host_spk> with an
// optional ?lan= query. hub_url may contain colons; parsers split from the
// right so https://host:port/path stays intact.
func Encode(o Offer) (string, error) {
	if strings.TrimSpace(o.HubURL) == "" || strings.TrimSpace(o.Code) == "" {
		return "", fmt.Errorf("protocol: incomplete offer")
	}
	if len(o.HostPub) != 32 {
		return "", fmt.Errorf("protocol: host public key must be 32 bytes")
	}
	if strings.ContainsAny(o.Code, ":?&") {
		return "", fmt.Errorf("protocol: pairing code must not contain : ? &")
	}
	spk := base64.RawURLEncoding.EncodeToString(o.HostPub)
	s := Scheme + o.HubURL + ":" + o.Code + ":" + spk
	if len(o.LAN) > 0 {
		q := url.Values{}
		q.Set("lan", strings.Join(o.LAN, ","))
		s += "?" + q.Encode()
	}
	return s, nil
}

// Parse reads an offer URI or the same string pasted by a human.
func Parse(raw string) (Offer, error) {
	raw = strings.TrimSpace(raw)
	var o Offer
	if !strings.HasPrefix(raw, Scheme) {
		return o, fmt.Errorf("protocol: not a pairlink v1 offer")
	}
	rest := strings.TrimPrefix(raw, Scheme)
	lan := ""
	if i := strings.Index(rest, "?"); i >= 0 {
		q, err := url.ParseQuery(rest[i+1:])
		if err != nil {
			return o, fmt.Errorf("protocol: offer query: %w", err)
		}
		lan = q.Get("lan")
		rest = rest[:i]
	}
	// Split from the right: spk, code, then hub (colons allowed).
	spkAt := strings.LastIndex(rest, ":")
	if spkAt <= 0 {
		return o, fmt.Errorf("protocol: missing host public key")
	}
	spk := rest[spkAt+1:]
	rest = rest[:spkAt]
	codeAt := strings.LastIndex(rest, ":")
	if codeAt <= 0 {
		return o, fmt.Errorf("protocol: missing pairing code")
	}
	o.Code = rest[codeAt+1:]
	o.HubURL = rest[:codeAt]
	if o.HubURL == "" || o.Code == "" || spk == "" {
		return o, fmt.Errorf("protocol: incomplete offer")
	}
	pub, err := base64.RawURLEncoding.DecodeString(spk)
	if err != nil {
		return o, fmt.Errorf("protocol: host public key: %w", err)
	}
	if len(pub) != 32 {
		return o, fmt.Errorf("protocol: host public key must be 32 bytes")
	}
	o.HostPub = pub
	if lan != "" {
		for _, p := range strings.Split(lan, ",") {
			if p = strings.TrimSpace(p); p != "" {
				o.LAN = append(o.LAN, p)
			}
		}
	}
	return o, nil
}
