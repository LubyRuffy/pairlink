package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteConformance(t *testing.T) {
	runStoreConformance(t, func(t *testing.T) Store {
		s := openTempSQLite(t)
		return s
	})
}

func TestSQLiteReopenKeepsLabels(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pairlink.db")
	s := openSQLite(t, path)
	ctx := context.Background()
	h := Host{TokenHash: HashSecret("persist"), Name: "box-a", Created: time.Now().UTC(), Pub: []byte("12345678901234567890123456789012")}
	if err := s.PutHost(ctx, h); err != nil {
		t.Fatal(err)
	}
	if err := s.PutBinding(ctx, Binding{
		ID: "b1", HostPub: h.Pub, DevicePub: []byte("abcdefghijklmnopqrstuvwxyz012345"),
		DeviceName: "dev-a", DeviceModel: "mod-a", TicketHash: HashSecret("tix"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2 := openSQLite(t, path)
	got, err := s2.HostByTokenHash(ctx, HashSecret("persist"))
	if err != nil || got.Name != "box-a" {
		t.Fatalf("reopen host %+v %v", got, err)
	}
	rows, err := s2.ListAllBindings(ctx)
	if err != nil || len(rows) != 1 || rows[0].DeviceName != "dev-a" || rows[0].DeviceModel != "mod-a" {
		t.Fatalf("reopen bindings %+v %v", rows, err)
	}
}

func openTempSQLite(t *testing.T) *SQLite {
	t.Helper()
	return openSQLite(t, filepath.Join(t.TempDir(), "pairlink.db"))
}

func TestSQLiteRejectsEmptyPathAndNilClose(t *testing.T) {
	if _, err := OpenSQLite("  "); err == nil {
		t.Fatal("empty path")
	}
	var s *SQLite
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteTraceRetain(t *testing.T) {
	prev := traceRetain
	traceRetain = 2
	t.Cleanup(func() { traceRetain = prev })
	s := openTempSQLite(t)
	ctx := context.Background()
	for _, ref := range []string{"t0", "t1", "t2"} {
		if err := s.AppendTrace(ctx, TraceEvent{Ref: ref, Kind: "bind"}); err != nil {
			t.Fatal(err)
		}
	}
	if ev, err := s.Trace(ctx, "t0"); err != nil || len(ev) != 0 {
		t.Fatalf("old trace %+v %v", ev, err)
	}
	if ev, err := s.Trace(ctx, "t2"); err != nil || len(ev) != 1 {
		t.Fatalf("new trace %+v %v", ev, err)
	}
}

func openSQLite(t *testing.T, path string) *SQLite {
	t.Helper()
	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
