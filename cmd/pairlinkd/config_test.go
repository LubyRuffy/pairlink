package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOptionsDefaults(t *testing.T) {
	opt, err := loadOptions(nil, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if opt.Listen != "127.0.0.1:7780" || opt.UDP != "127.0.0.1:7781" || opt.Database != "" || opt.TLS {
		t.Fatalf("%+v", opt)
	}
}

func TestLoadOptionsFlagOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pairlink.yaml")
	raw := []byte("listen: 127.0.0.1:9000\nudp: 127.0.0.1:9001\ndatabase: pairlink.db\ntls: true\n")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	opt, err := loadOptions([]string{"-config", path, "-listen", "127.0.0.1:7788", "-tls=false"}, func(k string) string {
		if k == "PAIRLINK_ADMIN_TOKEN" {
			return "from-env"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if opt.Listen != "127.0.0.1:7788" || opt.UDP != "127.0.0.1:9001" || opt.Database != "pairlink.db" || opt.TLS {
		t.Fatalf("%+v", opt)
	}
	if opt.AdminToken != "from-env" {
		t.Fatalf("admin %q", opt.AdminToken)
	}
}

func TestLoadOptionsFlagBeatsEnv(t *testing.T) {
	opt, err := loadOptions([]string{"-admin-token", "from-flag"}, func(string) string { return "from-env" })
	if err != nil {
		t.Fatal(err)
	}
	if opt.AdminToken != "from-flag" {
		t.Fatalf("%q", opt.AdminToken)
	}
}
