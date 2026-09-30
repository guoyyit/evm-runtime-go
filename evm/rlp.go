package evm

import "github.com/guoyyit/evm-runtime-go/state"

// rlpAddressNonce encodes the two-item list [address, nonce] in RLP, which is
// the input used for legacy CREATE address derivation.
func rlpAddressNonce(addr state.Address, nonce uint64) []byte {
	item := encodeItem(addr[:])
	nonceBytes := encodeUint(nonce)
	payload := append(item, nonceBytes...)
	return encodeList(payload)
}

func encodeList(payload []byte) []byte {
	if len(payload) < 56 {
		return append([]byte{0xc0 + byte(len(payload))}, payload...)
	}
	lenBytes := encodeLengthBytes(uint64(len(payload)))
	head := append([]byte{0xf7 + byte(len(lenBytes))}, lenBytes...)
	return append(head, payload...)
}

func encodeItem(b []byte) []byte {
	// Single byte in [0x00, 0x7f] is encoded as itself.
	if len(b) == 1 && b[0] < 0x80 {
		return b
	}
	if len(b) < 56 {
		return append([]byte{0x80 + byte(len(b))}, b...)
	}
	lenBytes := encodeLengthBytes(uint64(len(b)))
	head := append([]byte{0xb7 + byte(len(lenBytes))}, lenBytes...)
	return append(head, b...)
}

func encodeUint(n uint64) []byte {
	if n == 0 {
		return []byte{0x80}
	}
	var buf [8]byte
	i := 8
	for n > 0 {
		i--
		buf[i] = byte(n)
		n >>= 8
	}
	return encodeItem(buf[i:])
}

func encodeLengthBytes(n uint64) []byte {
	var buf [8]byte
	i := 8
	for n > 0 {
		i--
		buf[i] = byte(n)
		n >>= 8
	}
	return buf[i:]
}
