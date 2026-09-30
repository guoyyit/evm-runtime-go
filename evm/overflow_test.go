package evm

import (
	"errors"
	"math/big"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
)

func TestMemoryOffsetOverflow(t *testing.T) {
	ev, _ := testEVM(t)
	a := common.Address{0xe6}

	maxBytes := make([]byte, 32)
	for i := range maxBytes {
		maxBytes[i] = 0xff
	}
	// PUSH1 0 (value); PUSH32 max (offset); MSTORE.
	code := append([]byte{0x60, 0x00, 0x7f}, maxBytes...)
	code = append(code, 0x52, 0x00)
	ev.StateDB.CreateAccount(a)
	ev.StateDB.SetCode(a, code)

	caller := common.Address{0xe7}
	ev.StateDB.AddBalance(caller, new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("overflow must be an error, not a panic: %v", r)
			}
		}()
		_, _, err := ev.Call(caller, a, nil, 500_000_000, new(big.Int))
		if !errors.Is(err, ErrGasUintOverflow) {
			t.Fatalf("want gas overflow error, got %v", err)
		}
	}()
}

func TestMemoryGasCostBoundaries(t *testing.T) {
	if _, err := memoryGasCost(^uint64(0)); !errors.Is(err, ErrGasUintOverflow) {
		t.Fatalf("max size must overflow, got %v", err)
	}
	if _, err := memoryGasCost(^uint64(0) - 30); !errors.Is(err, ErrGasUintOverflow) {
		t.Fatalf("near-max size must overflow, got %v", err)
	}
}
