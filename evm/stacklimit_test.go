package evm

import (
	"errors"
	"math/big"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
)

func common2() common.Address { return common.Address{0xd0} }

func common3() common.Address { return common.Address{0xd1} }

func big1e18() *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil) }

func TestStackOverflow(t *testing.T) {
	ev, _ := testEVM(t)
	addr := common2()
	// 1025 PUSH1 instructions; the 1025th value must overflow the 1024 stack.
	code := make([]byte, 0, 1025*2+1)
	for i := 0; i < 1025; i++ {
		code = append(code, 0x60, 0x01)
	}
	code = append(code, 0x00)
	ev.StateDB.CreateAccount(addr)
	ev.StateDB.SetCode(addr, code)
	caller := common3()
	ev.StateDB.AddBalance(caller, big1e18())

	_, _, err := ev.Call(caller, addr, nil, 100_000_000, nil)
	if !errors.Is(err, ErrStackOverflow) {
		t.Fatalf("want stack overflow, got %v", err)
	}
}

func TestStackAtLimitOk(t *testing.T) {
	ev, _ := testEVM(t)
	addr := common2()
	// Exactly 1024 values fit, then STOP.
	code := make([]byte, 0, 1024*2+1)
	for i := 0; i < 1024; i++ {
		code = append(code, 0x60, 0x01)
	}
	code = append(code, 0x00)
	ev.StateDB.CreateAccount(addr)
	ev.StateDB.SetCode(addr, code)
	caller := common3()
	ev.StateDB.AddBalance(caller, big1e18())

	_, _, err := ev.Call(caller, addr, nil, 100_000_000, nil)
	if err != nil {
		t.Fatalf("1024 stack entries should fit: %v", err)
	}
}
