package evm

import (
	"math/big"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
)

func TestCreateFailureRollsBackNonce(t *testing.T) {
	ev, _ := testEVM(t)
	creator := common.Address{0xa0}
	ev.StateDB.AddBalance(creator, big.NewInt(1e18))

	// Init code hits INVALID immediately.
	_, _, _, err := ev.Create(creator, []byte{0xfe}, 500_000, new(big.Int))
	if err == nil {
		t.Fatal("expected creation failure")
	}
	if ev.StateDB.GetNonce(creator) != 0 {
		t.Fatalf("creator nonce = %d after failed create, want 0", ev.StateDB.GetNonce(creator))
	}

	// A successful creation bumps nonce exactly once.
	// Init code that deploys empty runtime: PUSH1 0 PUSH1 0 RETURN.
	initCode := []byte{0x60, 0x00, 0x60, 0x00, 0xf3}
	_, _, _, err = ev.Create(creator, initCode, 500_000, new(big.Int))
	if err != nil {
		t.Fatalf("successful create: %v", err)
	}
	if ev.StateDB.GetNonce(creator) != 1 {
		t.Fatalf("creator nonce = %d after successful create, want 1", ev.StateDB.GetNonce(creator))
	}
}

func TestCreateSuccessDeploysCode(t *testing.T) {
	ev, _ := testEVM(t)
	creator := common.Address{0xa1}
	ev.StateDB.AddBalance(creator, big.NewInt(1e18))

	runtimeCode := []byte{0x60, 0x07, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3}
	// Copy runtime code from the trailing bytes and return it.
	initCode := []byte{
		0x61, byte(len(runtimeCode) >> 8), byte(len(runtimeCode)),
		0x61, byte(15 >> 8), byte(15),
		0x60, 0x00, 0x39,
		0x61, byte(len(runtimeCode) >> 8), byte(len(runtimeCode)),
		0x60, 0x00, 0xf3,
	}
	initCode = append(initCode, runtimeCode...)

	_, addr, _, err := ev.Create(creator, initCode, 500_000, new(big.Int))
	if err != nil {
		t.Fatal(err)
	}
	if string(ev.StateDB.GetCode(addr)) != string(runtimeCode) {
		t.Fatalf("deployed code mismatch: %x", ev.StateDB.GetCode(addr))
	}
}
