package evm

import (
	"errors"
	"math/big"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
)

func staticViolation(t *testing.T, code []byte) {
	t.Helper()
	ev, _ := testEVM(t)
	addr := common.Address{0xf0}
	ev.StateDB.CreateAccount(addr)
	ev.StateDB.SetCode(addr, code)
	caller := common.Address{0xf1}
	ev.StateDB.AddBalance(caller, big.NewInt(1e18))

	_, _, err := ev.StaticCall(caller, addr, nil, 500_000)
	if !errors.Is(err, ErrWriteInReadOnly) {
		t.Fatalf("want write-in-read-only, got %v", err)
	}
	// No state should have changed.
	if ev.StateDB.GetState(addr, common.Hash{}) != (common.Hash{}) {
		t.Fatal("static context leaked a storage write")
	}
}

func TestStaticRejectsSStore(t *testing.T) {
	staticViolation(t, []byte{0x60, 0x01, 0x60, 0x00, 0x55, 0x00})
}

func TestStaticRejectsLog(t *testing.T) {
	staticViolation(t, []byte{
		0x60, 0x00, 0x60, 0x00, 0xa0, // LOG0(0,0)
		0x00,
	})
}

func TestStaticRejectsCallWithValue(t *testing.T) {
	target := common.Address{0xf2}
	ev, _ := testEVM(t)
	ev.StateDB.CreateAccount(target)
	ev.StateDB.SetCode(target, []byte{0x00})

	addr := common.Address{0xf3}
	ev.StateDB.CreateAccount(addr)
	// CALL(gas,to,value=1,args0,0,ret0,0)
	code := []byte{}
	code = append(code, 0x60, 0x00) // retLen
	code = append(code, 0x60, 0x00) // retOff
	code = append(code, 0x60, 0x00) // argsLen
	code = append(code, 0x60, 0x00) // argsOff
	code = append(code, 0x60, 0x01) // value 1
	code = append(code, 0x73)
	code = append(code, target.Bytes()...)
	code = append(code, 0x61, 0x27, 0x10) // gas
	code = append(code, 0xf1)
	code = append(code, 0x50, 0x00)
	ev.StateDB.SetCode(addr, code)

	caller := common.Address{0xf4}
	ev.StateDB.AddBalance(caller, big.NewInt(1e18))
	_, _, err := ev.StaticCall(caller, addr, nil, 500_000)
	if !errors.Is(err, ErrWriteInReadOnly) {
		t.Fatalf("want read-only violation for valued call, got %v", err)
	}
}
