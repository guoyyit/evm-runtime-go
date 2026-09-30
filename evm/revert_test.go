package evm

import (
	"math/big"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
)

func TestRevertCarriesData(t *testing.T) {
	ev, _ := testEVM(t)
	addr := common.Address{0x80}
	// Store 0xdeadbeef at mem[0], REVERT(0,32).
	code := []byte{
		0x61, 0xde, 0xad, 0x60, 0x00, 0x52, // PUSH2 0xdead -> mem (approx)
	}
	// Build a clean revert program: PUSH4 0xdeadbeef MSTORE PUSH1 32 PUSH1 0 REVERT
	code = []byte{
		0x63, 0xde, 0xad, 0xbe, 0xef, 0x60, 0x00, 0x52,
		0x60, 0x20, 0x60, 0x00, 0xfd,
	}
	deployAt(t, ev, addr, code, nil)
	caller := common.Address{0x81}
	ev.StateDB.AddBalance(caller, big.NewInt(1e18))

	out, _, err := ev.Call(caller, addr, nil, 200_000, new(big.Int))
	if err == nil {
		t.Fatal("expected revert error")
	}
	if !IsRevert(err) {
		t.Fatalf("expected revert, got %v", err)
	}
	data := RevertData(err)
	got := new(big.Int).SetBytes(data)
	if got.Cmp(big.NewInt(0xdeadbeef)) != 0 {
		t.Fatalf("revert data = %x, want deadbeef", data)
	}
	if out != nil && len(out) != len(data) {
		// out is nil on error path; data captured in error.
	}
}

func TestSubcallRevertReadable(t *testing.T) {
	ev, _ := testEVM(t)

	// Callee reverts with a known payload.
	callee := common.Address{0x82}
	ev.StateDB.CreateAccount(callee)
	revertCode := []byte{
		0x63, 0xab, 0xcd, 0xef, 0x01, 0x60, 0x00, 0x52,
		0x60, 0x20, 0x60, 0x00, 0xfd,
	}
	ev.StateDB.SetCode(callee, revertCode)

	// Parent calls callee, reads RETURNDATASIZE and copies return data,
	// then returns it to prove revert payload is observable.
	caller := common.Address{0x83}
	parentAddr := common.Address{0x84}
	ev.StateDB.AddBalance(caller, big.NewInt(1e18))
	ev.StateDB.CreateAccount(parentAddr)

	parent := buildRevertReaderCall(callee)
	ev.StateDB.SetCode(parentAddr, parent)

	out, _, err := ev.Call(caller, parentAddr, nil, 400_000, new(big.Int))
	if err != nil {
		t.Fatalf("parent should succeed reading revert data: %v", err)
	}
	if new(big.Int).SetBytes(out).Cmp(big.NewInt(0xabcdef01)) != 0 {
		t.Fatalf("parent did not propagate revert payload: %x", out)
	}
}

func buildRevertReaderCall(callee common.Address) []byte {
	var c []byte
	// CALL(gas=100000, addr, value=0, args=0,0, ret=0,0)
	c = append(c, 0x60, 0x00) // retSize
	c = append(c, 0x60, 0x00) // retOffset
	c = append(c, 0x60, 0x00) // argsLen
	c = append(c, 0x60, 0x00) // argsOffset
	c = append(c, 0x60, 0x00) // value
	c = append(c, 0x73)
	c = append(c, callee.Bytes()...)
	c = append(c, 0x61, 0x86, 0xa0) // gas ~34464
	c = append(c, 0xf1)             // CALL (success=0)
	c = append(c, 0x50)             // POP
	// RETURNDATACOPY(0,0,32)
	c = append(c, 0x60, 0x20)
	c = append(c, 0x60, 0x00)
	c = append(c, 0x60, 0x00)
	c = append(c, 0x3e)
	// RETURN(0,32)
	c = append(c, 0x60, 0x20)
	c = append(c, 0x60, 0x00)
	c = append(c, 0xf3)
	return c
}
