package evm

import (
	"math/big"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
)

func TestDelegateCallPreservesCallerAndValue(t *testing.T) {
	ev, _ := testEVM(t)

	original := common.Address{0x01}
	proxy := common.Address{0x02}
	lib := common.Address{0x03}

	ev.StateDB.AddBalance(original, big.NewInt(1e18))

	// Library: stores CALLVALUE into slot 1 and CALLER into slot 2.
	libCode := []byte{
		0x34, 0x60, 0x01, 0x55, // CALLVALUE -> slot1
		0x33, 0x60, 0x02, 0x55, // CALLER -> slot2
		0x00,
	}
	ev.StateDB.CreateAccount(lib)
	ev.StateDB.SetCode(lib, libCode)

	// Proxy delegates to the library: DELEGATECALL(gas,lib,0,0,0,0).
	proxyCode := []byte{}
	proxyCode = append(proxyCode, 0x60, 0x00) // retLen
	proxyCode = append(proxyCode, 0x60, 0x00) // retOff
	proxyCode = append(proxyCode, 0x60, 0x00) // argsLen
	proxyCode = append(proxyCode, 0x60, 0x00) // argsOff
	proxyCode = append(proxyCode, 0x73)
	proxyCode = append(proxyCode, lib.Bytes()...)
	proxyCode = append(proxyCode, 0x62, 0x01, 0x86, 0xa0) // PUSH3 100000
	proxyCode = append(proxyCode, 0xf4)
	proxyCode = append(proxyCode, 0x50, 0x00)
	ev.StateDB.CreateAccount(proxy)
	ev.StateDB.SetCode(proxy, proxyCode)

	_, _, err := ev.Call(original, proxy, nil, 500_000, big.NewInt(123))
	if err != nil {
		t.Fatalf("delegate call: %v", err)
	}

	// Writes must land in the proxy's storage context.
	slot1 := common.BytesToHash(big.NewInt(1).Bytes())
	slot2 := common.BytesToHash(big.NewInt(2).Bytes())
	if ev.StateDB.GetState(proxy, slot1).Big().Cmp(big.NewInt(123)) != 0 {
		t.Fatalf("slot1 (value) = %s, want 123", ev.StateDB.GetState(proxy, slot1).Big())
	}
	if ev.StateDB.GetState(proxy, slot2) != common.BytesToHash(original.Bytes()) {
		t.Fatalf("slot2 (caller) = %s, want original %s",
			ev.StateDB.GetState(proxy, slot2).Hex(), original.Hex())
	}
	// Library storage must remain untouched.
	if ev.StateDB.GetState(lib, slot1) != (common.Hash{}) {
		t.Fatal("library storage must not be written by delegatecall")
	}
}
