package common

import "math/big"

var (
	tt255   = new(big.Int).Lsh(big.NewInt(1), 255)
	tt256   = new(big.Int).Lsh(big.NewInt(1), 256)
	tt256m1 = new(big.Int).Sub(tt256, big.NewInt(1))
)

// NewU256 creates a U256 from a big.Int, reducing it modulo 2^256 and
// normalizing negatives to their two's-complement modulo value.
func NewU256(x *big.Int) U256 {
	if x == nil {
		return U256{v: new(big.Int)}
	}
	v := new(big.Int)
	if x.Sign() < 0 {
		v.Mod(x, tt256)
	} else if x.Cmp(tt256) >= 0 {
		v.And(x, tt256m1)
	} else {
		v.Set(x)
	}
	return U256{v: v}
}

// NewU256FromUint64 creates a U256 from a uint64.
func NewU256FromUint64(x uint64) U256 {
	return U256{v: new(big.Int).SetUint64(x)}
}

// Big returns a copy of the underlying big.Int.
func (u U256) Big() *big.Int {
	if u.v == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(u.v)
}

// Uint64 returns the low 64 bits.
func (u U256) Uint64() uint64 {
	if u.v == nil {
		return 0
	}
	return u.v.Uint64()
}

// IsZero reports whether the value is zero.
func (u U256) IsZero() bool {
	return u.v == nil || u.v.Sign() == 0
}

// Bytes returns the unsigned big-endian 32-byte representation.
func (u U256) Bytes32() []byte {
	b := make([]byte, 32)
	if u.v == nil {
		return b
	}
	v := u.v.Bytes()
	copy(b[32-len(v):], v)
	return b
}
