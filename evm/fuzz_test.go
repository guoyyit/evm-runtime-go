package evm

import (
	"math/rand"
	"testing"
)

// TestHeavyFuzz runs thousands of random programs to guarantee the interpreter
// never panics and never reports gas used above the requested budget.
func TestHeavyFuzz(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000000000000000"))
	allowed := []byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b,
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a,
		0x1b, 0x1c, 0x1d, 0x20, 0x30, 0x33, 0x34, 0x36, 0x38, 0x3d, 0x42,
		0x43, 0x46, 0x47, 0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57,
		0x58, 0x59, 0x5a, 0x5b, 0x5f, 0x60, 0x80, 0x81, 0x82, 0x90, 0x91,
		0xa0, 0x00, 0xf3, 0xfd,
	}
	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < 3000; trial++ {
		l := 1 + rng.Intn(60)
		code := make([]byte, l)
		for i := 0; i < l; i++ {
			op := allowed[rng.Intn(len(allowed))]
			code[i] = op
			if op == 0x60 && i+1 < l {
				i++
				code[i] = byte(rng.Intn(256))
			}
		}
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					t.Fatalf("panic code=%x: %v", code, rec)
				}
			}()
			res, _ := r.Execute(Message{From: from, Data: code, GasLimit: 1_000_000})
			if res.GasUsed > 1_000_000 {
				t.Fatalf("gas used exceeds budget: %d", res.GasUsed)
			}
		}()
	}
}
