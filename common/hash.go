package common

import "golang.org/x/crypto/sha3"

// Keccak256 computes the legacy Keccak-256 digest used by Ethereum.
func Keccak256(data ...[]byte) []byte {
	h := sha3.NewLegacyKeccak256()
	for _, d := range data {
		h.Write(d)
	}
	return h.Sum(nil)
}

// Keccak256Hash returns the digest as a Hash value.
func Keccak256Hash(data ...[]byte) Hash {
	return BytesToHash(Keccak256(data...))
}
