// Package store is the hub's bounded control state: hosts, pairings, bindings.
package store

import (
	"bytes"
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
	ErrConflict = errors.New("store: conflict")
)

const MaxBindingsPerHost = 32

type Host struct {
	Pub       []byte
	TokenHash []byte
	Created   time.Time
	// Name is the host's own label (hostname or the name it shows a phone).
	// Empty means the endpoint has not announced one.
	Name string
	// Version is the PC build string the endpoint chose to publish.
	// Empty means it has not announced one. The hub does not invent it.
	Version string
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
	// DeviceName and DeviceModel are display labels from redeem (`name`, `model`)
	// or a later TypeLabel on that device's socket. Empty means that field was
	// not sent. Neither is a secret, a path, or a fingerprint.
	DeviceName  string
	DeviceModel string
	// DeviceVersion is the phone software version from redeem or a later
	// TypeLabel. Empty means that field was not sent.
	DeviceVersion string
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
	// LookupPairing reports the pairing for a code hash and does not consume it.
	LookupPairing(ctx context.Context, codeHash []byte) (Pairing, error)
	ConsumePairing(ctx context.Context, codeHash []byte) (Pairing, error)
	// RestorePairing makes a consumed code redeemable again after a failed redeem.
	RestorePairing(ctx context.Context, codeHash []byte) error
	PairingByID(ctx context.Context, id string) (Pairing, error)

	PutBinding(ctx context.Context, b Binding) error
	BindingByTicketHash(ctx context.Context, hash []byte) (Binding, error)
	// BindingByPeers returns the newest live binding for the pair. Older
	// revoked rows do not hide it. Revoked-only history returns ErrRevoked.
	BindingByPeers(ctx context.Context, hostPub, devicePub []byte) (Binding, error)
	ListBindings(ctx context.Context, hostPub []byte) ([]Binding, error)
	ListAllBindings(ctx context.Context) ([]Binding, error)
	RevokeBinding(ctx context.Context, id string) error
	// NoteDeviceSeen advances LastConnected on every binding for this device
	// public key, including revoked rows. A host key, an unknown key, a zero
	// time, or an older time changes nothing.
	NoteDeviceSeen(ctx context.Context, devicePub []byte, at time.Time) error
	// SetDeviceLabels updates device_name, device_model, and device_version on
	// every binding for this device public key, including revoked rows. An
	// empty name, model, or version leaves that column unchanged. Created,
	// last-connected, and any other device's row stay as they are. A host
	// key, a short key, or an unknown key changes nothing.
	SetDeviceLabels(ctx context.Context, devicePub []byte, name, model, version string) error
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
	if next.Version == "" {
		next.Version = prev.Version
	}
	if next.Created.IsZero() {
		next.Created = prev.Created
	}
	return next
}

// locateHost matches a token first, then a 32-byte public key. An empty
// public key matches nothing. A new token cannot take a public key that
// already belongs to a different token.
func locateHost(hosts []Host, h Host) (int, error) {
	tokenAt, pubAt := -1, -1
	if len(h.TokenHash) > 0 {
		for i, x := range hosts {
			if bytes.Equal(x.TokenHash, h.TokenHash) {
				tokenAt = i
				break
			}
		}
	}
	if len(h.Pub) == 32 {
		for i, x := range hosts {
			if len(x.Pub) == 32 && bytes.Equal(x.Pub, h.Pub) {
				pubAt = i
				break
			}
		}
	}
	if tokenAt >= 0 && pubAt >= 0 && tokenAt != pubAt {
		return -1, ErrConflict
	}
	if tokenAt < 0 && pubAt >= 0 && len(h.TokenHash) > 0 && len(hosts[pubAt].TokenHash) > 0 && !bytes.Equal(hosts[pubAt].TokenHash, h.TokenHash) {
		return -1, ErrConflict
	}
	if tokenAt >= 0 {
		return tokenAt, nil
	}
	return pubAt, nil
}

// selectBinding keeps the newest live row for a peer pair. A later revoked
// row does not hide an earlier live one, and row order does not matter.
func selectBinding(rows []Binding, hostPub, devicePub []byte) (Binding, error) {
	var live Binding
	found, revoked := false, false
	var best time.Time
	bestAt := -1
	for i, b := range rows {
		if !bytes.Equal(b.HostPub, hostPub) || !bytes.Equal(b.DevicePub, devicePub) {
			continue
		}
		if b.Revoked {
			revoked = true
			continue
		}
		if !found || b.Created.After(best) || (b.Created.Equal(best) && i >= bestAt) {
			live = b
			found = true
			best = b.Created
			bestAt = i
		}
	}
	if found {
		return live, nil
	}
	if revoked {
		return Binding{}, ErrRevoked
	}
	return Binding{}, ErrNotFound
}

func HashSecret(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}
