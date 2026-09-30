package evm

import (
	"errors"
	"math/big"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
	"github.com/guoyyit/evm-runtime-go/state"
)

func testEVM(t *testing.T) (*EVM, *state.StateDB) {
	t.Helper()
	dir := t.TempDir()
	store, err := state.NewFileStore(dir + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	db, err := state.NewStateDB(store)
	if err != nil {
		t.Fatal(err)
	}
	ev := NewEVM(db, BlockContext{
		Number:      big.NewInt(1),
		Timestamp:   1000,
		GasLimit:    10_000_000,
		BlockHashes: map[uint64]common.Hash{},
	}, TxContext{Origin: common.Address{0x1}})
	return ev, db
}

// runCodeAt deploys code to an address and calls it.
func runCodeAt(t *testing.T, ev *EVM, addr common.Address, code []byte, gas uint64) ([]byte, uint64, error) {
	t.Helper()
	ev.StateDB.CreateAccount(addr)
	ev.StateDB.SetCode(addr, code)
	caller := common.Address{0xaa}
	ev.StateDB.AddBalance(caller, big.NewInt(1e18))
	return ev.Call(caller, addr, nil, gas, new(big.Int))
}

func TestArithmeticAdd(t *testing.T) {
	ev, _ := testEVM(t)
	// PUSH1 2 PUSH1 40 ADD PUSH1 0 MSTORE PUSH1 32 PUSH1 0 RETURN
	code := []byte{
		0x60, 0x02,
		0x60, 0x28,
		0x01,
		0x60, 0x00, 0x52,
		0x60, 0x20, 0x60, 0x00, 0xf3,
	}
	out, _, err := runCodeAt(t, ev, common.Address{0xcc}, code, 100_000)
	if err != nil {
		t.Fatalf("execution: %v", err)
	}
	got := new(big.Int).SetBytes(out)
	if got.Cmp(big.NewInt(0x2a)) != 0 {
		t.Fatalf("want 42, got %s", got.String())
	}
}

func TestInvalidOpcode(t *testing.T) {
	ev, _ := testEVM(t)
	code := []byte{0xef}
	_, _, err := runCodeAt(t, ev, common.Address{0xcd}, code, 100_000)
	if err == nil {
		t.Fatal("expected invalid opcode error")
	}
}

func TestStackUnderflow(t *testing.T) {
	ev, _ := testEVM(t)
	code := []byte{0x01} // ADD with empty stack
	_, _, err := runCodeAt(t, ev, common.Address{0xce}, code, 100_000)
	if !errors.Is(err, ErrStackUnderflow) {
		t.Fatalf("want stack underflow, got %v", err)
	}
}

func TestOutOfGas(t *testing.T) {
	ev, _ := testEVM(t)
	code := []byte{0x00} // STOP costs 0, use EXP pair below
	_ = code
	// JUMPDEST loops forever, exhausting gas.
	loop := []byte{0x5b, 0x60, 0x00, 0x56}
	_, _, err := runCodeAt(t, ev, common.Address{0xcf}, loop, 100)
	if !errors.Is(err, ErrOutOfGas) {
		t.Fatalf("want out of gas, got %v", err)
	}
}
