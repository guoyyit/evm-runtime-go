package evm

import (
	"errors"
	"testing"

	"github.com/guoyyit/evm-runtime-go/state"
)

func testRuntime(t *testing.T) *Runtime {
	t.Helper()
	r, err := Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func addrAt(b byte) state.Address {
	var a state.Address
	a[19] = b
	return a
}

// 0x60 0x01 0x60 0x02 0x01 0x60 0x00 0x52 ... store 3 at slot 0
func TestSmokeSStore(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	if err := r.Mint(from, bigWei("1000000")); err != nil {
		t.Fatal(err)
	}

	code := []byte{
		0x60, 0x03, // PUSH1 3
		0x60, 0x00, // PUSH1 0
		0x55, // SSTORE
		0x00, // STOP
	}
	res, err := r.Execute(Message{
		From:     from,
		To:       nil,
		Data:     code,
		GasLimit: 200_000,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Err != nil {
		t.Fatalf("evm error: %v (used %d)", res.Err, res.GasUsed)
	}
	if res.CreatedAddress == nil {
		t.Fatal("expected created address")
	}
	var slot state.Hash
	if got := r.StorageAt(*res.CreatedAddress, slot); got[31] != 3 {
		t.Fatalf("slot0 = %x, want 3", got)
	}
}

func TestSmokeArithmetic(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000000"))

	// PUSH1 20, PUSH1 22, ADD, PUSH1 0, MSTORE, PUSH1 32, PUSH1 0, RETURN
	code := []byte{
		0x60, 20, 0x60, 22, 0x01,
		0x60, 0x00, 0x52,
		0x60, 0x20, 0x60, 0x00, 0xf3,
	}
	res, _ := r.Execute(Message{From: from, Data: code, GasLimit: 100_000})
	if res.Err != nil {
		t.Fatalf("err: %v used=%d", res.Err, res.GasUsed)
	}
	if len(res.ReturnData) != 32 || res.ReturnData[31] != 42 {
		t.Fatalf("return = %x", res.ReturnData)
	}
}

func TestSmokeInvalidOpcode(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000000"))
	res, _ := r.Execute(Message{From: from, Data: []byte{0xef, 0x00}, GasLimit: 100_000})
	if !errors.Is(res.Err, ErrInvalidOpCode) {
		t.Fatalf("want invalid opcode, got %v", res.Err)
	}
	if r.Balance(addrAt(9)) != "0" {
		t.Fatal("failed create must not persist")
	}
}

func TestSmokeBadJump(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000000"))
	// PUSH1 0x03, JUMP, STOP, JUMPDEST
	res, _ := r.Execute(Message{
		From:     from,
		Data:     []byte{0x60, 0x03, 0x56, 0x00, 0x5b},
		GasLimit: 100_000,
	})
	if !errors.Is(res.Err, ErrInvalidJump) {
		t.Fatalf("want invalid jump, got %v", res.Err)
	}
}
