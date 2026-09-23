package main

import (
	"bytes"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type options struct {
	Listen     string
	UDP        string
	Database   string
	AdminToken string
	TLS        bool
	TLSCert    string
}

type fileConfig struct {
	Listen   string `yaml:"listen"`
	UDP      string `yaml:"udp"`
	Database string `yaml:"database"`
	TLS      bool   `yaml:"tls"`
	TLSCert  string `yaml:"tls_cert"`
}

func loadOptions(args []string, getenv func(string) string) (options, error) {
	fs := flag.NewFlagSet("pairlinkd", flag.ContinueOnError)
	var buf bytes.Buffer
	fs.SetOutput(&buf)
	listen := fs.String("listen", "", "HTTP listen address")
	udp := fs.String("udp", "", "STUN-lite UDP listen address")
	database := fs.String("database", "", "sqlite file; empty uses memory")
	admin := fs.String("admin-token", "", "management bearer token")
	configPath := fs.String("config", "", "yaml config file")
	tlsOn := fs.Bool("tls", false, "HTTPS with an ephemeral certificate")
	tlsCert := fs.String("tls-cert", "", "path for the ephemeral certificate PEM")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	opt := options{
		Listen:  "127.0.0.1:7780",
		UDP:     "127.0.0.1:7781",
		TLSCert: "pairlink-cert.pem",
	}
	if *configPath != "" {
		raw, err := os.ReadFile(*configPath)
		if err != nil {
			return options{}, err
		}
		var file fileConfig
		if err := yaml.Unmarshal(raw, &file); err != nil {
			return options{}, err
		}
		if file.Listen != "" {
			opt.Listen = file.Listen
		}
		if file.UDP != "" {
			opt.UDP = file.UDP
		}
		if file.Database != "" {
			opt.Database = file.Database
		}
		if file.TLS {
			opt.TLS = true
		}
		if file.TLSCert != "" {
			opt.TLSCert = file.TLSCert
		}
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if set["listen"] {
		opt.Listen = *listen
	}
	if set["udp"] {
		opt.UDP = *udp
	}
	if set["database"] {
		opt.Database = *database
	}
	if set["admin-token"] {
		opt.AdminToken = strings.TrimSpace(*admin)
	} else if getenv != nil {
		opt.AdminToken = strings.TrimSpace(getenv("PAIRLINK_ADMIN_TOKEN"))
	}
	if set["tls"] {
		opt.TLS = *tlsOn
	}
	if set["tls-cert"] && strings.TrimSpace(*tlsCert) != "" {
		opt.TLSCert = *tlsCert
	}
	if _, _, err := net.SplitHostPort(opt.Listen); err != nil {
		return options{}, fmt.Errorf("listen: %w", err)
	}
	if _, _, err := net.SplitHostPort(opt.UDP); err != nil {
		return options{}, fmt.Errorf("udp: %w", err)
	}
	return opt, nil
}
