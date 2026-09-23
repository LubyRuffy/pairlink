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
	// Name is the host's own label (hostname or the name it shows a phone).
	// Empty means the endpoint has not announced one.
	Name string
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
	// DeviceName and DeviceModel are display labels from redeem (`name`, `model`).
	// Empty means that field was not sent. Neither is a secret or a path.
	DeviceName  string
	DeviceModel string
	// LastConnected is the latest device-socket attach or drop. Zero means
	// this process has never observed that phone's websocket. It is not Created.
	LastConnected time.Time
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
	ListHosts(ctx context.Context) ([]Host, error)

	PutPairing(ctx context.Context, p Pairing) error
	ConsumePairing(ctx context.Context, codeHash []byte) (Pairing, error)
	PairingByID(ctx context.Context, id string) (Pairing, error)

	PutBinding(ctx context.Context, b Binding) error
	BindingByTicketHash(ctx context.Context, hash []byte) (Binding, error)
	BindingByPeers(ctx context.Context, hostPub, devicePub []byte) (Binding, error)
	ListBindings(ctx context.Context, hostPub []byte) ([]Binding, error)
	ListAllBindings(ctx context.Context) ([]Binding, error)
	RevokeBinding(ctx context.Context, id string) error
	// NoteDeviceSeen advances LastConnected on every binding for this device
	// public key, including revoked rows. A host key, an unknown key, a zero
	// time, or an older time changes nothing.
	NoteDeviceSeen(ctx context.Context, devicePub []byte, at time.Time) error
	CountBindings(ctx context.Context, hostPub []byte) (int, error)

	AppendTrace(ctx context.Context, ev TraceEvent) error
	Trace(ctx context.Context, ref string) ([]TraceEvent, error)
}

// mergeHost keeps identity fields when the incoming record leaves them empty.
// An empty name does not wipe a stored label. An empty public key does not
// detach a token from a host that already registered one.
func mergeHost(prev, next Host) Host {
	if len(next.TokenHash) == 0 {
		next.TokenHash = prev.TokenHash
	}
	if len(next.Pub) == 0 {
		next.Pub = prev.Pub
	}
	if next.Name == "" {
		next.Name = prev.Name
	}
	if next.Created.IsZero() {
		next.Created = prev.Created
	}
	return next
}

func HashSecret(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}
