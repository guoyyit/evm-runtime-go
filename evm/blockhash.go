package evm

import (
	"math/big"

	"github.com/guoyyit/evm-runtime-go/crypto"
	"github.com/guoyyit/evm-runtime-go/state"
)

// blockHash deterministically derives a pseudo block hash from the chain
// context and block number. Real Ethereum requires historical hashes; this
// runtime exposes a pure keccak(number ++ parent context) so identical
// requests always observe identical hashes without external data.
func blockHash(b BlockContext, number *big.Int) state.Hash {
	var numBuf [32]byte
	number.FillBytes(numBuf[:])
	sum := crypto.Keccak256(
		[]byte("blockhash"),
		b.ChainID.FillBytes(make([]byte, 32)),
		numBuf[:],
	)
	var h state.Hash
	copy(h[:], sum)
	return h
}
