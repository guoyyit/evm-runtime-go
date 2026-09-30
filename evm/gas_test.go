package evm

import (
	"math/big"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
)

func gasUsed(t *testing.T, code []byte, start uint64) uint64 {
	t.Helper()
	ev, _ := testEVM(t)
	addr := common.Address{0x50}
	ev.StateDB.CreateAccount(addr)
	ev.StateDB.SetCode(addr, code)
	caller := common.Address{0x51}
	ev.StateDB.AddBalance(caller, big.NewInt(1e18))
	_, left, err := ev.Call(caller, addr, nil, start, new(big.Int))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return start - left
}

func TestGasStop(t *testing.T) {
	if got := gasUsed(t, []byte{0x00}, 100_000); got != 0 {
		t.Fatalf("STOP gas = %d, want 0", got)
	}
}

func TestGasAddPopStop(t *testing.T) {
	// PUSH1 1 PUSH1 2 ADD POP STOP: 3+3+3+2+0
	code := []byte{0x60, 0x01, 0x60, 0x02, 0x01, 0x50, 0x00}
	if got := gasUsed(t, code, 100_000); got != 3+3+3+2 {
		t.Fatalf("gas = %d", got)
	}
}

func TestGasMemoryExpansion(t *testing.T) {
	// MSTORE at offset 0: pushes (3+3) + MSTORE 3 + memory expansion for
	// 1 word = 3 gas.
	code := []byte{0x60, 0x00, 0x60, 0x00, 0x52, 0x00}
	want := uint64(3 + 3 + 3 + 3)
	if got := gasUsed(t, code, 100_000); got != want {
		t.Fatalf("mstore gas = %d, want %d", got, want)
	}
}

func TestGasSStoreFresh(t *testing.T) {
	// Cold account/slot access (2600) + SSTORE set (20000).
	code := []byte{0x60, 0x01, 0x60, 0x00, 0x55, 0x00}
	want := uint64(3 + 3 + 2600 + 20000)
	if got := gasUsed(t, code, 100_000); got != want {
		t.Fatalf("sstore gas = %d, want %d", got, want)
	}
}

func TestGasKeccak(t *testing.T) {
	// keccak(mem[0:32]): MSTORE (incl. 3 mem), two pushes, KECCAK 30 + 6*1.
	code := []byte{
		0x60, 0x01, 0x60, 0x00, 0x52,
		0x60, 0x20, 0x60, 0x00, 0x20, 0x00,
	}
	want := uint64(3 + 3 + 3 + 3 + 3 + 3 + 30 + 6)
	if got := gasUsed(t, code, 100_000); got != want {
		t.Fatalf("keccak gas = %d, want %d", got, want)
	}
}
