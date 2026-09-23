package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"
)

func localIPs() []net.IP {
	out := []net.IP{net.IPv4(127, 0, 0, 1)}
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	seen := map[string]bool{"127.0.0.1": true}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok || ipnet.IP.IsLoopback() || ipnet.IP.IsLinkLocalUnicast() {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil || seen[ip4.String()] {
				continue
			}
			seen[ip4.String()] = true
			out = append(out, ip4)
		}
	}
	return out
}

func listenHosts(listen string) []string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return nil
	}
	if host == "0.0.0.0" || host == "::" || host == "" {
		var out []string
		for _, ip := range localIPs() {
			out = append(out, net.JoinHostPort(ip.String(), port))
		}
		return out
	}
	return []string{listen}
}

func certIPs(listen string) []net.IP {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return localIPs()
	}
	if host == "0.0.0.0" || host == "::" || host == "" {
		return localIPs()
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return localIPs()
	}
	return []net.IP{ip}
}

func buildCert(ips []net.IP) (tls.Certificate, []byte, error) {
	if len(ips) == 0 {
		ips = []net.IP{net.IPv4(127, 0, 0, 1)}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "pairlink"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           ips,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("tls: %w", err)
	}
	return pair, certPEM, nil
}
