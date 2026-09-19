// Package store is the hub's bounded control state: hosts, pairings, bindings.
package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("store: not found")
	ErrExpired  = errors.New("store: expired")
	ErrConsumed = errors.New("store: already consumed")
	ErrRevoked  = errors.New("store: revoked")
	ErrLimit    = errors.New("store: binding limit")
)

const MaxBindingsPerHost = 32

type Host struct {
	Pub       []byte
	TokenHash []byte
	Created   time.Time
}

type Pairing struct {
	ID        string
	HostPub   []byte
	CodeHash  []byte
	Expires   time.Time
	Consumed  bool
	SessionID []byte
}

type Binding struct {
	ID         string
	HostPub    []byte
	DevicePub  []byte
	TicketHash []byte
	Revoked    bool
	Created    time.Time
	SessionID  []byte
}

type TraceEvent struct {
	Ref    string    `json:"ref"`
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	PeerFP string    `json:"peer_fp"`
	Bytes  int       `json:"bytes"`
	Path   string    `json:"path,omitempty"`
	Note   string    `json:"note,omitempty"`
}

type Store interface {
	PutHost(ctx context.Context, h Host) error
	HostByTokenHash(ctx context.Context, hash []byte) (Host, error)
	HostByPub(ctx context.Context, pub []byte) (Host, error)

	PutPairing(ctx context.Context, p Pairing) error
	ConsumePairing(ctx context.Context, codeHash []byte) (Pairing, error)
	PairingByID(ctx context.Context, id string) (Pairing, error)

	PutBinding(ctx context.Context, b Binding) error
	BindingByTicketHash(ctx context.Context, hash []byte) (Binding, error)
	BindingByPeers(ctx context.Context, hostPub, devicePub []byte) (Binding, error)
	ListBindings(ctx context.Context, hostPub []byte) ([]Binding, error)
	RevokeBinding(ctx context.Context, id string) error
	CountBindings(ctx context.Context, hostPub []byte) (int, error)

	AppendTrace(ctx context.Context, ev TraceEvent) error
	Trace(ctx context.Context, ref string) ([]TraceEvent, error)
}

func HashSecret(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}
