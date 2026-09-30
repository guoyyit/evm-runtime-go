package evm

import (
	"math/big"

	"github.com/guoyyit/evm-runtime-go/common"
)

// rlpEncodeAddress serializes a (sender, nonce) pair the way CREATE does:
// RLP([sender_bytes, nonce]).
func rlpEncodeAddress(sender common.Address, nonce uint64) []byte {
	senderEnc := rlpEncodeBytes(sender.Bytes())
	nonceEnc := rlpEncodeUint(nonce)
	payload := append(append([]byte(nil), senderEnc...), nonceEnc...)
	return rlpEncodeList(payload)
}

func rlpEncodeBytes(b []byte) []byte {
	if len(b) == 1 && b[0] < 0x80 {
		return b
	}
	return append(rlpLengthPrefix(len(b), 0x80), b...)
}

func rlpEncodeUint(n uint64) []byte {
	if n == 0 {
		return []byte{0x80}
	}
	var buf [8]byte
	big.NewInt(int64(n)).FillBytes(buf[:])
	body := trimLeadingZeroBytes(buf[:])
	return rlpEncodeBytes(body)
}

func rlpEncodeList(body []byte) []byte {
	return append(rlpLengthPrefix(len(body), 0xc0), body...)
}

func rlpLengthPrefix(length int, offset byte) []byte {
	if length < 56 {
		return []byte{offset + byte(length)}
	}
	lenBytes := uintBE(uint64(length))
	return append([]byte{offset + 55 + byte(len(lenBytes))}, lenBytes...)
}

func uintBE(v uint64) []byte {
	var buf [8]byte
	for i := 7; i >= 0; i-- {
		buf[i] = byte(v)
		v >>= 8
	}
	return trimLeadingZeroBytes(buf[:])
}

// createAddress computes the CREATE address keccak(rlp([sender, nonce]))[12:].
func createAddress(sender common.Address, nonce uint64) common.Address {
	return common.BytesToAddress(common.Keccak256(rlpEncodeAddress(sender, nonce))[12:])
}

// create2Address computes keccak(0xff ++ sender ++ salt ++ keccak(init))[12:].
func create2Address(sender common.Address, salt, codeHash common.Hash) common.Address {
	buf := make([]byte, 0, 1+20+32+32)
	buf = append(buf, 0xff)
	buf = append(buf, sender.Bytes()...)
	buf = append(buf, salt.Bytes()...)
	buf = append(buf, codeHash.Bytes()...)
	return common.BytesToAddress(common.Keccak256(buf)[12:])
}
