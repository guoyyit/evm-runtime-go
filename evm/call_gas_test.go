package evm

import (
	"math/big"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
)

func TestSubcallGasReturned(t *testing.T) {
	ev, _ := testEVM(t)
	callee := common.Address{0x60}
	ev.StateDB.CreateAccount(callee)
	ev.StateDB.SetCode(callee, []byte{0x00}) // STOP, uses 0 gas

	// Parent: CALL with requested 10000 gas; all but the call overhead must
	// be returned since the callee spends nothing.
	parentAddr := common.Address{0x61}
	caller := common.Address{0x62}
	ev.StateDB.AddBalance(caller, big.NewInt(1e18))
	ev.StateDB.CreateAccount(parentAddr)
	ev.StateDB.SetCode(parentAddr, []byte{
		0x60, 0x00, // retSize
		0x60, 0x00, // retOffset
		0x60, 0x00, // argsLen
		0x60, 0x00, // argsOffset
		0x60, 0x00, // value
		0x73, // PUSH20 callee
	})
	// Constructing the call manually is error-prone; build it programmatically.
	parent := buildCallParent(callee, 10_000)
	ev.StateDB.SetCode(parentAddr, parent)

	_, left, err := ev.Call(caller, parentAddr, nil, 200_000, new(big.Int))
	if err != nil {
		t.Fatal(err)
	}
	used := uint64(200_000) - left
	// Parent overhead only: pushes + CALL warm (to is cold => 2600 charged in
	// chargeCallGas intrinsic region) + the returned gas is given back.
	// Exact value: 7 pushes *3 =21, CALL intrinsic cold 2600, STOP 0.
	// Cold CALL per EIP-2929 = 700 base + 2500 cold surcharge = 3200.
	want := uint64(15 + 3 + 3 + 2 + 3200)
	if used > want+100 || used < want {
		t.Fatalf("subcall gas used = %d, want around %d", used, want)
	}
}

func buildCallParent(callee common.Address, gas uint64) []byte {
	code := []byte{}
	code = append(code, 0x60, 0x00) // retSize
	code = append(code, 0x60, 0x00) // retOffset
	code = append(code, 0x60, 0x00) // argsLen
	code = append(code, 0x60, 0x00) // argsOffset
	code = append(code, 0x60, 0x00) // value
	code = append(code, 0x73)       // PUSH20
	code = append(code, callee.Bytes()...)
	var g [32]byte
	big.NewInt(int64(gas)).FillBytes(g[:])
	code = append(code, 0x7f) // PUSH32 gas
	code = append(code, g[:]...)
	code = append(code, 0xf1) // CALL
	code = append(code, 0x50) // POP success
	code = append(code, 0x00) // STOP
	return code
}

func TestInsufficientBalanceSubcall(t *testing.T) {
	ev, _ := testEVM(t)
	callee := common.Address{0x70}
	ev.StateDB.CreateAccount(callee)
	ev.StateDB.SetCode(callee, []byte{0x00})

	parentAddr := common.Address{0x71}
	caller := common.Address{0x72}
	// Caller funds parent with nothing; CALL attempts value transfer 100.
	ev.StateDB.AddBalance(caller, big.NewInt(1e18))
	ev.StateDB.CreateAccount(parentAddr)

	code := []byte{}
	code = append(code, 0x60, 0x00)
	code = append(code, 0x60, 0x00)
	code = append(code, 0x60, 0x00)
	code = append(code, 0x60, 0x00)
	code = append(code, 0x60, 0x64) // value 100
	code = append(code, 0x73)
	code = append(code, callee.Bytes()...)
	code = append(code, 0x61, 0x27, 0x10) // gas 10000
	code = append(code, 0xf1)
	code = append(code, 0x50)
	code = append(code, 0x00)
	ev.StateDB.SetCode(parentAddr, code)

	_, _, err := ev.Call(caller, parentAddr, nil, 200_000, new(big.Int))
	// Insufficient balance inside CALL is captured as call failure (push 0),
	// but here the parent itself is unfunded; the call returns 0 and the
	// frame still completes without an EVM-level halt.
	if err != nil {
		t.Fatalf("parent frame should not halt: %v", err)
	}
	if ev.StateDB.GetBalance(callee).Sign() != 0 {
		t.Fatal("callee must not receive value from an unfunded call")
	}
}
