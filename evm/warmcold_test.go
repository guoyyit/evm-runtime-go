package evm

import (
	"math/big"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
)

func TestSLoadWarmAfterCold(t *testing.T) {
	// Two SLOADs of slot 0: first cold 2600, second warm 100.
	code := []byte{
		0x60, 0x00, 0x54, 0x50, // SLOAD slot0, POP
		0x60, 0x00, 0x54, 0x50, // SLOAD slot0 again (warm), POP
		0x00,
	}
	used := gasUsed(t, code, 200_000)
	// 2 PUSH1 (6) + 2 POP (4) + cold SLOAD 2600 + warm SLOAD 100.
	want := uint64(6 + 4 + 2600 + 100)
	if used != want {
		t.Fatalf("double SLOAD gas = %d, want %d", used, want)
	}
}

func TestBalanceWarmAfterCold(t *testing.T) {
	ev, _ := testEVM(t)
	target := common.Address{0xc1}
	ev.StateDB.CreateAccount(target)

	addr := common.Address{0xc2}
	ev.StateDB.CreateAccount(addr)
	ev.StateDB.SetCode(addr, append([]byte{
		0x73,
	}, append(target.Bytes(), []byte{
		0x31, 0x50, // BALANCE cold; POP
		0x73,
	}...)...))
	// Build programmatically to keep it readable.
	code := []byte{}
	code = append(code, 0x73)
	code = append(code, target.Bytes()...)
	code = append(code, 0x31, 0x50)
	code = append(code, 0x73)
	code = append(code, target.Bytes()...)
	code = append(code, 0x31, 0x50)
	code = append(code, 0x00)
	ev.StateDB.SetCode(addr, code)

	caller := common.Address{0xc3}
	ev.StateDB.AddBalance(caller, big.NewInt(1e18))
	_, left, err := ev.Call(caller, addr, nil, 200_000, new(big.Int))
	if err != nil {
		t.Fatal(err)
	}
	used := uint64(200_000) - left
	// 2 PUSH20 (3 each) + 2 POP (2 each) + cold BALANCE 2600 + warm 100.
	want := uint64(6 + 4 + 2600 + 100)
	if used != want {
		t.Fatalf("double BALANCE gas = %d, want %d", used, want)
	}
}
