package evm

import (
	"crypto/sha256"
	"math/big"

	"github.com/guoyyit/evm-runtime-go/common"
	"golang.org/x/crypto/ripemd160"
)

// PrecompiledContract is a native built-in contract.
type PrecompiledContract interface {
	RequiredGas(input []byte) uint64
	Run(input []byte) ([]byte, error)
}

func defaultPrecompiles() map[common.Address]PrecompiledContract {
	return map[common.Address]PrecompiledContract{
		common.BytesToAddress([]byte{1}): &ecrecover{},
		common.BytesToAddress([]byte{2}): &sha256hash{},
		common.BytesToAddress([]byte{3}): &ripemd160hash{},
		common.BytesToAddress([]byte{4}): &identity{},
	}
}

// ecrecover (address 0x01): elliptic curve public key recovery.
type ecrecover struct{}

func (c *ecrecover) RequiredGas(_ []byte) uint64 { return 3000 }

func (c *ecrecover) Run(input []byte) ([]byte, error) {
	in := make([]byte, 128)
	copy(in, input)

	hash := common.BytesToHash(in[0:32])
	v := int(common.BytesToHash(in[32:64]).Big().Uint64()) - 27
	r := new(big.Int).SetBytes(in[64:96])
	s := new(big.Int).SetBytes(in[96:128])

	if v != 0 && v != 1 {
		return nil, nil
	}
	pub, err := recoverECDSA(hash.Bytes(), r, s, v)
	if err != nil || pub == nil {
		return nil, nil
	}
	addr := common.BytesToAddress(common.Keccak256(pub[1:])[12:])
	out := make([]byte, 32)
	copy(out[12:], addr[:])
	return out, nil
}

// sha256 (address 0x02).
type sha256hash struct{}

func (c *sha256hash) RequiredGas(input []byte) uint64 {
	return 60 + 12*toWordSize(uint64(len(input)))
}

func (c *sha256hash) Run(input []byte) ([]byte, error) {
	sum := sha256.Sum256(input)
	return sum[:], nil
}

// ripemd160 (address 0x03).
type ripemd160hash struct{}

func (c *ripemd160hash) RequiredGas(input []byte) uint64 {
	return 600 + 120*toWordSize(uint64(len(input)))
}

func (c *ripemd160hash) Run(input []byte) ([]byte, error) {
	h := ripemd160.New()
	h.Write(input)
	sum := h.Sum(nil)
	out := make([]byte, 32)
	copy(out[12:], sum)
	return out, nil
}

// identity (address 0x04): returns its input unchanged.
type identity struct{}

func (c *identity) RequiredGas(input []byte) uint64 {
	return 15 + 3*toWordSize(uint64(len(input)))
}

func (c *identity) Run(input []byte) ([]byte, error) {
	return append([]byte(nil), input...), nil
}
