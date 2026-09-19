// Package crypto is long-term X25519 identities and Noise-KK-style sessions.
package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

const (
	KeySize   = 32
	NonceSize = chacha20poly1305.NonceSize
)

// Identity is a long-term X25519 keypair. The private key never leaves the
// device; the hub addresses peers by the public key.
type Identity struct {
	priv [KeySize]byte
	pub  [KeySize]byte
}

func Generate() (*Identity, error) {
	var id Identity
	if _, err := rand.Read(id.priv[:]); err != nil {
		return nil, fmt.Errorf("crypto: generate: %w", err)
	}
	pub, err := curve25519.X25519(id.priv[:], curve25519.Basepoint)
	if err != nil {
		return nil, fmt.Errorf("crypto: public key: %w", err)
	}
	copy(id.pub[:], pub)
	return &id, nil
}

func FromPrivate(priv []byte) (*Identity, error) {
	if len(priv) != KeySize {
		return nil, fmt.Errorf("crypto: bad private key length")
	}
	var id Identity
	copy(id.priv[:], priv)
	pub, err := curve25519.X25519(id.priv[:], curve25519.Basepoint)
	if err != nil {
		return nil, err
	}
	copy(id.pub[:], pub)
	return &id, nil
}

func (id *Identity) Public() []byte  { return append([]byte(nil), id.pub[:]...) }
func (id *Identity) Private() []byte { return append([]byte(nil), id.priv[:]...) }

// Fingerprint is a short, log-safe label. It is not a secret.
func (id *Identity) Fingerprint() string { return Fingerprint(id.pub[:]) }

func Fingerprint(pub []byte) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:8])
}

func PublicBase64(pub []byte) string {
	return base64.RawURLEncoding.EncodeToString(pub)
}

func ParsePublic(s string) ([]byte, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	if len(b) != KeySize {
		return nil, fmt.Errorf("crypto: public key must be %d bytes", KeySize)
	}
	return b, nil
}

func dh(priv, pub []byte) ([]byte, error) {
	return curve25519.X25519(priv, pub)
}

// Handshake is the initiator state waiting for the responder's ephemeral.
type Handshake struct {
	self    *Identity
	peerPub []byte
	ephPriv [KeySize]byte
	ephPub  []byte
}

// Initiate starts Noise-KK: send our ephemeral public key.
func Initiate(self *Identity, peerPub []byte) (*Handshake, []byte, error) {
	if len(peerPub) != KeySize {
		return nil, nil, fmt.Errorf("crypto: bad peer key")
	}
	hs := &Handshake{self: self, peerPub: append([]byte(nil), peerPub...)}
	if _, err := rand.Read(hs.ephPriv[:]); err != nil {
		return nil, nil, err
	}
	pub, err := curve25519.X25519(hs.ephPriv[:], curve25519.Basepoint)
	if err != nil {
		return nil, nil, err
	}
	hs.ephPub = pub
	return hs, append([]byte(nil), pub...), nil
}

// Respond accepts the initiator ephemeral and returns our ephemeral plus a live session.
func Respond(self *Identity, peerPub, theirEph []byte) (*Session, []byte, error) {
	if len(peerPub) != KeySize || len(theirEph) != KeySize {
		return nil, nil, fmt.Errorf("crypto: bad handshake")
	}
	var ephPriv [KeySize]byte
	if _, err := rand.Read(ephPriv[:]); err != nil {
		return nil, nil, err
	}
	ephPub, err := curve25519.X25519(ephPriv[:], curve25519.Basepoint)
	if err != nil {
		return nil, nil, err
	}
	sess, err := derive(self, peerPub, ephPriv[:], theirEph, false)
	if err != nil {
		return nil, nil, err
	}
	return sess, ephPub, nil
}

func (hs *Handshake) Finish(theirEph []byte) (*Session, error) {
	if hs == nil {
		return nil, errors.New("crypto: nil handshake")
	}
	if len(theirEph) != KeySize {
		return nil, fmt.Errorf("crypto: bad handshake")
	}
	return derive(hs.self, hs.peerPub, hs.ephPriv[:], theirEph, true)
}

func derive(self *Identity, peerPub, ephPriv, theirEph []byte, initiator bool) (*Session, error) {
	ee, err := dh(ephPriv, theirEph)
	if err != nil {
		return nil, err
	}
	es, err := dh(ephPriv, peerPub)
	if err != nil {
		return nil, err
	}
	se, err := dh(self.priv[:], theirEph)
	if err != nil {
		return nil, err
	}
	ss, err := dh(self.priv[:], peerPub)
	if err != nil {
		return nil, err
	}
	if !initiator {
		es, se = se, es
	}
	ikm := append(append(append(ee, es...), se...), ss...)
	key := make([]byte, 64)
	r := hkdf.New(sha256.New, ikm, []byte("pairlink"), []byte("pairlink/v1"))
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}
	send, recv := key[:32], key[32:]
	if !initiator {
		send, recv = recv, send
	}
	saead, err := chacha20poly1305.New(send)
	if err != nil {
		return nil, err
	}
	raead, err := chacha20poly1305.New(recv)
	if err != nil {
		return nil, err
	}
	return &Session{send: saead, recv: raead}, nil
}

// Session is directional AEAD. Switching relay→direct does not mint new keys.
type Session struct {
	mu   sync.Mutex
	send interface {
		Seal(dst, nonce, plaintext, additionalData []byte) []byte
	}
	recv interface {
		Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error)
	}
	sendN uint64
	recvN uint64
}

func (s *Session) Seal(plain []byte) ([]byte, error) {
	if s == nil {
		return nil, errors.New("crypto: nil session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendN++
	n := s.sendN
	var nonce [NonceSize]byte
	binary.BigEndian.PutUint64(nonce[4:], n)
	out := make([]byte, 8, 8+len(plain)+16)
	binary.BigEndian.PutUint64(out, n)
	return s.send.Seal(out, nonce[:], plain, nil), nil
}

func (s *Session) Open(msg []byte) ([]byte, error) {
	if s == nil {
		return nil, errors.New("crypto: nil session")
	}
	if len(msg) < 8+16 {
		return nil, fmt.Errorf("crypto: short message")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := binary.BigEndian.Uint64(msg[:8])
	var nonce [NonceSize]byte
	binary.BigEndian.PutUint64(nonce[4:], n)
	plain, err := s.recv.Open(nil, nonce[:], msg[8:], nil)
	if err != nil {
		return nil, err
	}
	if n > s.recvN {
		s.recvN = n
	}
	return plain, nil
}
