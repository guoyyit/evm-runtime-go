package crypto

const keccakRate = 136 // bytes (1088-bit rate for Keccak-256)

var keccakRC = [24]uint64{
	0x0000000000000001, 0x0000000000008082, 0x800000000000808a, 0x8000000080008000,
	0x000000000000808b, 0x0000000080000001, 0x8000000080008081, 0x8000000000008009,
	0x000000000000008a, 0x0000000000000088, 0x0000000080008009, 0x000000008000000a,
	0x000000008000808b, 0x800000000000008b, 0x8000000000008089, 0x8000000000008003,
	0x8000000000008002, 0x8000000000000080, 0x000000000000800a, 0x800000008000000a,
	0x8000000080008081, 0x8000000000008080, 0x0000000080000001, 0x8000000080008008,
}

var keccakRot = [25]uint{
	0, 1, 62, 28, 27,
	36, 44, 6, 55, 20,
	3, 10, 43, 25, 39,
	41, 45, 15, 21, 8,
	18, 2, 61, 56, 14,
}

func rotl(x uint64, n uint) uint64 {
	return (x << n) | (x >> (64 - n))
}

// keccakF applies 24 rounds of the Keccak-f[1600] permutation in place.
func keccakF(a *[25]uint64) {
	var b [25]uint64
	var c, d [5]uint64
	for round := 0; round < 24; round++ {
		for x := 0; x < 5; x++ {
			c[x] = a[x] ^ a[x+5] ^ a[x+10] ^ a[x+15] ^ a[x+20]
		}
		for x := 0; x < 5; x++ {
			d[x] = c[(x+4)%5] ^ rotl(c[(x+1)%5], 1)
		}
		for x := 0; x < 5; x++ {
			for y := 0; y < 5; y++ {
				a[x+5*y] ^= d[x]
			}
		}
		for x := 0; x < 5; x++ {
			for y := 0; y < 5; y++ {
				b[y+5*((2*x+3*y)%5)] = rotl(a[x+5*y], keccakRot[x+5*y])
			}
		}
		for x := 0; x < 5; x++ {
			for y := 0; y < 5; y++ {
				a[x+5*y] = b[x+5*y] ^
					(^b[(x+1)%5+5*y] & b[(x+2)%5+5*y])
			}
		}
		a[0] ^= keccakRC[round]
	}
}

// Keccak256 computes the Keccak-256 digest used by the EVM.
func Keccak256(data ...[]byte) []byte {
	var state [25]uint64

	var buf [keccakRate]byte
	n := 0
	xorBlock := func() {
		for i := 0; i < keccakRate/8; i++ {
			v := uint64(buf[8*i]) |
				uint64(buf[8*i+1])<<8 |
				uint64(buf[8*i+2])<<16 |
				uint64(buf[8*i+3])<<24 |
				uint64(buf[8*i+4])<<32 |
				uint64(buf[8*i+5])<<40 |
				uint64(buf[8*i+6])<<48 |
				uint64(buf[8*i+7])<<56
			state[i] ^= v
		}
		keccakF(&state)
	}

	for _, chunk := range data {
		for len(chunk) > 0 {
			take := copy(buf[n:], chunk)
			n += take
			chunk = chunk[take:]
			if n == keccakRate {
				xorBlock()
				n = 0
			}
		}
	}

	// Keccak padding: 0x01 ... 0x80 (multi-rate padding with domain suffix).
	buf[n] = 0x01
	buf[keccakRate-1] |= 0x80
	xorBlock()

	out := make([]byte, 32)
	for i := 0; i < 4; i++ {
		v := state[i]
		for j := 0; j < 8; j++ {
			out[8*i+j] = byte(v >> (8 * j))
		}
	}
	return out
}
