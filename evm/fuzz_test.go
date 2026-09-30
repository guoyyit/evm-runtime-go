package evm

import (
	"math/big"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
)

// FuzzBytecodeNeverPanics feeds arbitrary byte streams to the interpreter.
// Every input must terminate with either success or a typed EVM error and
// must never panic or corrupt state.
func FuzzBytecodeNeverPanics(f *testing.F) {
	seeds := [][]byte{
		{0x00},
		{0x60, 0x01, 0x60, 0x02, 0x01},
		{0xfe},
		{0x5b, 0x60, 0x00, 0x56},
		{0x60, 0x00, 0x54},
		{0x73, 1, 2, 3, 0xf1},
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, code []byte) {
		if len(code) > 128 {
			code = code[:128]
		}
		ev, _ := testEVM(t)
		from := common.Address{0x55}
		ev.StateDB.AddBalance(from, new(big.Int).Exp(big.NewInt(10), big.NewInt(24), nil))
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic on code %x: %v", code, r)
				}
			}()
			_, _, _ = ev.Call(from, common.Address{0x56}, code, 200_000, new(big.Int))
		}()
	})
}
