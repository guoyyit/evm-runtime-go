package common

import (
	"encoding/hex"
	"math/big"
	"strings"
)

// AddressLength is the number of bytes in an Ethereum account address.
const AddressLength = 20

// HashLength is the number of bytes in a Keccak-256 hash.
const HashLength = 32

// Address is a 20-byte account identifier.
type Address [AddressLength]byte

// Hash is a 32-byte Keccak-256 digest.
type Hash [HashLength]byte

// U256 wraps a non-negative 256-bit integer. All values are reduced modulo
// 2^256 when entering the EVM.
type U256 struct {
	v *big.Int
}

// BytesToAddress converts a byte slice into an address, left-padding with
// zero bytes when the slice is shorter than 20 bytes and truncating the high
// bytes when it is longer.
func BytesToAddress(b []byte) Address {
	var a Address
	if len(b) > AddressLength {
		b = b[len(b)-AddressLength:]
	}
	copy(a[AddressLength-len(b):], b)
	return a
}

// BytesToHash converts a byte slice into a hash using right-alignment rules.
func BytesToHash(b []byte) Hash {
	var h Hash
	if len(b) > HashLength {
		b = b[len(b)-HashLength:]
	}
	copy(h[HashLength-len(b):], b)
	return h
}

// HexToAddress parses an EIP-55 or plain hexadecimal address.
func HexToAddress(s string) Address {
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	if len(s) > 2*AddressLength {
		s = s[len(s)-2*AddressLength:]
	}
	if len(s)%2 == 1 {
		s = "0" + s
	}
	b, _ := hex.DecodeString(s)
	return BytesToAddress(b)
}

// HexToHash parses a 0x-prefixed hash.
func HexToHash(s string) Hash {
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	if len(s) > 2*HashLength {
		s = s[len(s)-2*HashLength:]
	}
	if len(s)%2 == 1 {
		s = "0" + s
	}
	b, _ := hex.DecodeString(s)
	return BytesToHash(b)
}

// Hex returns the 0x-prefixed hexadecimal representation of the address.
func (a Address) Hex() string {
	return "0x" + hex.EncodeToString(a[:])
}

// Bytes returns the address as a byte slice.
func (a Address) Bytes() []byte {
	return a[:]
}

// Hex returns the 0x-prefixed hexadecimal representation of the hash.
func (h Hash) Hex() string {
	return "0x" + hex.EncodeToString(h[:])
}

// Bytes returns the hash as a byte slice.
func (h Hash) Bytes() []byte {
	return h[:]
}

// MarshalText / UnmarshalText make Address JSON-friendly.
func (a Address) MarshalText() ([]byte, error) {
	return []byte(a.Hex()), nil
}

func (a *Address) UnmarshalText(b []byte) error {
	*a = HexToAddress(string(b))
	return nil
}

// MarshalText / UnmarshalText make Hash JSON-friendly.
func (h Hash) MarshalText() ([]byte, error) {
	return []byte(h.Hex()), nil
}

func (h *Hash) UnmarshalText(b []byte) error {
	*h = HexToHash(string(b))
	return nil
}

// Big converts a hash to a big integer (left-aligned, as used for code hash
// comparisons this is only needed for equality in practice).
func (h Hash) Big() *big.Int {
	return new(big.Int).SetBytes(h[:])
}

// AddressFromBig converts a big-endian 256-bit integer to a 20-byte address.
func AddressFromBig(x *big.Int) Address {
	return BytesToAddress(x.Bytes())
}
