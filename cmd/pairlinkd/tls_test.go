package main

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"testing"
)

func TestLocalIPsIncludeLoopback(t *testing.T) {
	found := false
	for _, ip := range localIPs() {
		if ip.Equal(net.IPv4(127, 0, 0, 1)) {
			found = true
		}
	}
	if !found {
		t.Fatal("missing loopback")
	}
}

func TestListenHosts(t *testing.T) {
	got := listenHosts("127.0.0.1:7780")
	if len(got) != 1 || got[0] != "127.0.0.1:7780" {
		t.Fatalf("%v", got)
	}
	all := listenHosts("0.0.0.0:7780")
	if len(all) == 0 {
		t.Fatal("empty")
	}
}

func TestBuildCertHasIP(t *testing.T) {
	_, pemBytes, err := buildCert([]net.IP{net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatal("pem")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ip := range cert.IPAddresses {
		if ip.Equal(net.IPv4(127, 0, 0, 1)) {
			found = true
		}
	}
	if !found {
		t.Fatal("san")
	}
}
