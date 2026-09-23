package client

import (
	"crypto/tls"
	"net/http"
	"testing"
)

func TestWSTLSConfigFromHTTPClient(t *testing.T) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	c := &Conn{http: &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}}
	d := c.wsDialer()
	if d.TLSClientConfig == nil || d.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatal("tls config not copied")
	}
	if c.wsDialer().TLSClientConfig == cfg {
		t.Fatal("dialer must clone the tls config")
	}
}

func TestWSDialerWithoutTransport(t *testing.T) {
	c := &Conn{http: &http.Client{}}
	if c.wsDialer().TLSClientConfig != nil {
		t.Fatal("nil transport")
	}
}
