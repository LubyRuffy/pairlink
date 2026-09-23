package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"os"
	"time"
)

func hubClient(ca string, insecure bool) (*http.Client, error) {
	if ca != "" && insecure {
		return nil, errors.New("use either -ca or -insecure")
	}
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("default transport")
	}
	tr := base.Clone()
	switch {
	case insecure:
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}
	case ca != "":
		pemBytes, err := os.ReadFile(ca)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemBytes) {
			return nil, errors.New("no certificate in -ca")
		}
		tr.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	return &http.Client{Timeout: 15 * time.Second, Transport: tr}, nil
}
