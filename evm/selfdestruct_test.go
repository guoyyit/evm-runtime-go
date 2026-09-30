package evm

import (
	"math/big"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
)

func TestSelfDestructTransfersAndRemoves(t *testing.T) {
	ev, _ := testEVM(t)
	victim := common.Address{0xb0}
	beneficiary := common.Address{0xb1}
	caller := common.Address{0xb2}

	ev.StateDB.CreateAccount(victim)
	ev.StateDB.AddBalance(victim, big.NewInt(500))
	// PUSH20 beneficiary; SELFDESTRUCT
	code := append([]byte{0x73}, beneficiary.Bytes()...)
	code = append(code, 0xff)
	ev.StateDB.SetCode(victim, code)
	ev.StateDB.AddBalance(caller, big.NewInt(1e18))

	_, _, err := ev.Call(caller, victim, nil, 200_000, new(big.Int))
	if err != nil {
		t.Fatal(err)
	}

	if !ev.StateDB.HasSuicided(victim) {
		t.Fatal("victim should be marked suicided")
	}
	if ev.StateDB.GetBalance(victim).Sign() != 0 {
		t.Fatalf("victim balance = %s, want 0", ev.StateDB.GetBalance(victim))
	}
	if ev.StateDB.GetBalance(beneficiary).Cmp(big.NewInt(500)) != 0 {
		t.Fatalf("beneficiary = %s, want 500", ev.StateDB.GetBalance(beneficiary))
	}
}

func TestPrecompileSha256(t *testing.T) {
	ev, _ := testEVM(t)
	caller := common.Address{0xb3}
	ev.StateDB.AddBalance(caller, big.NewInt(1e18))

	out, _, err := ev.Call(caller, common.BytesToAddress([]byte{2}), []byte("abc"), 100_000, new(big.Int))
	if err != nil {
		t.Fatal(err)
	}
	// sha256("abc")
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if toHex(out) != want {
		t.Fatalf("sha256(abc) = %x, want %s", out, want)
	}
}

func TestPrecompileIdentity(t *testing.T) {
	ev, _ := testEVM(t)
	caller := common.Address{0xb4}
	ev.StateDB.AddBalance(caller, big.NewInt(1e18))
	in := []byte("hello world")
	out, _, err := ev.Call(caller, common.BytesToAddress([]byte{4}), in, 100_000, new(big.Int))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(in) {
		t.Fatalf("identity = %q", out)
	}
}

func toHex(b []byte) string {
	const hexchars = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, x := range b {
		out[2*i] = hexchars[x>>4]
		out[2*i+1] = hexchars[x&0xf]
	}
	return string(out)
}
