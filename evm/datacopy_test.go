package evm

import (
	"math/big"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
)

func TestCalldataloadZeroPadding(t *testing.T) {
	ev, _ := testEVM(t)
	addr := common.Address{0xe0}
	// CALLDATALOAD(0); MSTORE mem0; RETURN(0,32)
	code := []byte{
		0x60, 0x00, 0x35, // PUSH1 0 CALLDATALOAD
		0x60, 0x00, 0x52,
		0x60, 0x20, 0x60, 0x00, 0xf3,
	}
	ev.StateDB.CreateAccount(addr)
	ev.StateDB.SetCode(addr, code)
	caller := common.Address{0xe1}
	ev.StateDB.AddBalance(caller, big.NewInt(1e18))

	// Only 4 bytes of calldata; remaining 28 must be zero-padded.
	out, _, err := ev.Call(caller, addr, []byte{0xde, 0xad, 0xbe, 0xef}, 200_000, new(big.Int))
	if err != nil {
		t.Fatal(err)
	}
	want := new(big.Int).Lsh(big.NewInt(0xdeadbeef), 224)
	if new(big.Int).SetBytes(out).Cmp(want) != 0 {
		t.Fatalf("calldataload = %x", out)
	}

	// Offset beyond calldata yields all zeros.
	code2 := []byte{
		0x60, 0xff, 0x35,
		0x60, 0x00, 0x52,
		0x60, 0x20, 0x60, 0x00, 0xf3,
	}
	addr2 := common.Address{0xe2}
	ev.StateDB.CreateAccount(addr2)
	ev.StateDB.SetCode(addr2, code2)
	out2, _, err := ev.Call(caller, addr2, []byte{1, 2, 3}, 200_000, new(big.Int))
	if err != nil {
		t.Fatal(err)
	}
	if new(big.Int).SetBytes(out2).Sign() != 0 {
		t.Fatalf("out-of-range calldataload = %x", out2)
	}
}

func TestExtcodesizeAndHash(t *testing.T) {
	ev, _ := testEVM(t)
	target := common.Address{0xe3}
	ev.StateDB.CreateAccount(target)
	ev.StateDB.SetCode(target, []byte{0x60, 0x01, 0x60, 0x02})

	if ev.StateDB.GetCodeSize(target) != 4 {
		t.Fatalf("code size = %d", ev.StateDB.GetCodeSize(target))
	}
	wantHash := common.Keccak256Hash([]byte{0x60, 0x01, 0x60, 0x02})
	if ev.StateDB.GetCodeHash(target) != wantHash {
		t.Fatal("code hash mismatch")
	}
}
