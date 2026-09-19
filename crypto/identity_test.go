package crypto

import (
	"bytes"
	"testing"
)

func TestSessionRoundTripAndThirdPartyCannotOpen(t *testing.T) {
	a, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	b, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	hs, msg1, err := Initiate(a, b.Public())
	if err != nil {
		t.Fatal(err)
	}
	sessB, msg2, err := Respond(b, a.Public(), msg1)
	if err != nil {
		t.Fatal(err)
	}
	sessA, err := hs.Finish(msg2)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("binding-complete")
	ct, err := sessA.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ct, plain) {
		t.Fatal("plaintext leaked into ciphertext")
	}
	got, err := sessB.Open(ct)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("got %q", got)
	}
	c, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	hsC, msgC, err := Initiate(c, b.Public())
	if err != nil {
		t.Fatal(err)
	}
	_, _, _ = hsC, msgC, c
	if _, err := sessB.Open(ct); err == nil {
		// already consumed nonce-wise but same ciphertext should still open... actually Open is not single-use.
	}
	// A third identity talking to B cannot open A's packet.
	_, msg2c, err := Respond(b, c.Public(), msgC)
	if err != nil {
		t.Fatal(err)
	}
	sessC, err := hsC.Finish(msg2c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessC.Open(ct); err == nil {
		t.Fatal("outsider opened someone else's packet")
	}
}

func TestFingerprintIsNotTheRawKey(t *testing.T) {
	id, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	fp := id.Fingerprint()
	if fp == PublicBase64(id.Public()) {
		t.Fatal("fingerprint must not be the public key")
	}
	if len(fp) != 16 {
		t.Fatalf("fingerprint len %d", len(fp))
	}
}
