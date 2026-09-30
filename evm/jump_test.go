package evm

import (
	"errors"
	"math/big"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
)

func TestInvalidJump(t *testing.T) {
	ev, _ := testEVM(t)
	addr := common.Address{0x90}
	// JUMP to offset 5 which is inside a PUSH operand, not a JUMPDEST.
	code := []byte{
		0x60, 0x05, // PUSH1 5
		0x56, // JUMP
		0x5b, // JUMPDEST at 3
		0x00,
		0x00, // offset 5
	}
	deployAt(t, ev, addr, code, nil)
	caller := common.Address{0x91}
	ev.StateDB.AddBalance(caller, big.NewInt(1e18))
	_, _, err := ev.Call(caller, addr, nil, 100_000, new(big.Int))
	if !errors.Is(err, ErrInvalidJump) {
		t.Fatalf("want invalid jump, got %v", err)
	}
}

func TestValidJump(t *testing.T) {
	ev, _ := testEVM(t)
	addr := common.Address{0x92}
	// Jump over an INVALID byte to a JUMPDEST, then STOP.
	code := []byte{
		0x60, 0x06, // PUSH1 6
		0x56,       // JUMP
		0xfe,       // INVALID at 3 (skipped)
		0x00, 0x00, // padding
		0x5b, // JUMPDEST at 6
		0x00, // STOP
	}
	deployAt(t, ev, addr, code, nil)
	caller := common.Address{0x93}
	ev.StateDB.AddBalance(caller, big.NewInt(1e18))
	_, _, err := ev.Call(caller, addr, nil, 100_000, new(big.Int))
	if err != nil {
		t.Fatalf("valid jump failed: %v", err)
	}
}

func TestCreate2Address(t *testing.T) {
	// Known vector: sender=0xdeadbeef..., salt=0, init=0x00.
	sender := common.HexToAddress("0x00000000000000000000000000000000deadbeef")
	got := create2Address(sender, common.Hash{}, common.Keccak256Hash([]byte{0x00}))
	// Independently verified with Python pycryptodome's keccak-256.
	want := common.HexToAddress("0xf2f2c74e8a9f6b37e64d7d56a6aff03ddad73242")
	if got != want {
		t.Fatalf("create2 = %s, want %s", got.Hex(), want.Hex())
	}
}

func TestCreateAddressVector(t *testing.T) {
	// Well-known vector: sender 0x6ac7ea33f8831ea9dcc53393aaa88b25a785dbf0
	// nonce 0 -> 0xcd234a471b72ba2f1ccf0a70fcaba648a5eecd8d
	sender := common.HexToAddress("0x6ac7ea33f8831ea9dcc53393aaa88b25a785dbf0")
	got := createAddress(sender, 0)
	want := common.HexToAddress("0xcd234a471b72ba2f1ccf0a70fcaba648a5eecd8d")
	if got != want {
		t.Fatalf("create(nonce0) = %s, want %s", got.Hex(), want.Hex())
	}
	// Independently verified with Python keccak over the RLP encoding.
	got1 := createAddress(sender, 1)
	want1 := common.HexToAddress("0x343c43a37d37dff08ae8c4a11544c718abb4fcf8")
	if got1 != want1 {
		t.Fatalf("create(nonce1) = %s, want %s", got1.Hex(), want1.Hex())
	}
}
