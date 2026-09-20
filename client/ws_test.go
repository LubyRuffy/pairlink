package client

import (
	"testing"
	"time"
)

func TestKeepEveryDefaultAndOverride(t *testing.T) {
	c := &Conn{}
	if c.keepEvery() != defaultKeepAlive {
		t.Fatalf("default %s", c.keepEvery())
	}
	c.cfg.KeepAlive = time.Second
	if c.keepEvery() != time.Second {
		t.Fatalf("override %s", c.keepEvery())
	}
}

func TestStoppedAndConnectedOnAFreshConn(t *testing.T) {
	c := &Conn{closed: make(chan struct{})}
	if c.stopped() || c.Connected() {
		t.Fatal("fresh conn")
	}
	_ = c.Close()
	if !c.stopped() {
		t.Fatal("Close must stop the conn")
	}
	if c.Connected() {
		t.Fatal("Close must clear the socket")
	}
}
