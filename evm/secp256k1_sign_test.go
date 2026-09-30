package evm

import (
	"crypto/sha256"
	"encoding/binary"
	"math/big"
	"testing"
)

// signForTest produces a deterministic low-s ECDSA signature over secp256k1
// using the same field arithmetic as the recovery code under test.
func signForTest(t *testing.T, priv *big.Int, msg []byte) (r, s *big.Int, recid int) {
	t.Helper()

	// Deterministic nonce derived from the message (test-only).
	seed := sha256.Sum256(append([]byte("nonce"), msg...))
	k := new(big.Int).SetBytes(seed[:])
	k.Mod(k, new(big.Int).Sub(secpN, big.NewInt(1)))
	k.Add(k, big.NewInt(1))

	G := &curvePoint{x: new(big.Int).Set(secpGx), y: new(big.Int).Set(secpGy)}
	R := scalarMult(G, k)
	r = new(big.Int).Mod(R.x, secpN)
	if r.Sign() == 0 {
		t.Fatal("r is zero")
	}

	e := new(big.Int).SetBytes(msg)
	e.Mod(e, secpN)

	kInv := new(big.Int).ModInverse(k, secpN)
	s = new(big.Int).Mul(r, priv)
	s.Add(s, e)
	s.Mul(s, kInv)
	s.Mod(s, secpN)
	if s.Sign() == 0 {
		t.Fatal("s is zero")
	}

	recid = int(R.y.Bit(0))
	if r.Cmp(R.x) != 0 {
		recid |= 2
	}

	// Normalize to low-s and flip recovery id accordingly.
	halfN := new(big.Int).Rsh(secpN, 1)
	if s.Cmp(halfN) > 0 {
		s.Sub(secpN, s)
		recid ^= 1
	}
	return r, s, recid
}

func TestRecoverECDSARoundTrip(t *testing.T) {
	priv, _ := new(big.Int).SetString("c87f65ff3f271bf5dc8643484f66b200109caffe4bf98c4cb393dc35762d0e65", 16)

	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], 42)
	hash := sha256.Sum256(msg[:])

	r, s, recid := signForTest(t, priv, hash[:])

	pub, err := recoverECDSA(hash[:], r, s, recid)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}

	G := &curvePoint{x: new(big.Int).Set(secpGx), y: new(big.Int).Set(secpGy)}
	want := scalarMult(G, priv)
	if new(big.Int).SetBytes(pub[1:33]).Cmp(want.x) != 0 ||
		new(big.Int).SetBytes(pub[33:]).Cmp(want.y) != 0 {
		t.Fatalf("recovered pubkey mismatch")
	}
}
