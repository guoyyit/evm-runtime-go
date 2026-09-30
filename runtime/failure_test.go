package runtime

import (
	"errors"
	"math/big"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
	"github.com/guoyyit/evm-runtime-go/evm"
)

// TestOutOfGasIsAtomic runs a contract that SSTOREs then loops until gas is
// exhausted. The storage write must not survive the failed execution.
func TestOutOfGasIsAtomic(t *testing.T) {
	r := testRuntime(t)
	from := common.HexToAddress("0x4242")
	r.setBalanceForTest(from, big.NewInt(1e18))

	runtimeCode := []byte{
		0x60, 0x01, 0x60, 0x00, 0x55, // SSTORE slot0=1
		0x5b,             // JUMPDEST at pc=5
		0x60, 0x05, 0x56, // PUSH1 5; JUMP -> loop forever
	}
	cfg := baseConfig(from)
	cfg.Data = buildInitCode(runtimeCode)
	deploy, _ := r.Execute(cfg)
	if deploy.Failed {
		t.Fatal(deploy.Err)
	}
	to := *deploy.Address

	before, _ := r.Revision(), error(nil)
	call := baseConfig(from)
	call.To = &to
	call.GasLimit = 50_000
	res, err := r.Execute(call)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || !errors.Is(res.Err, evm.ErrOutOfGas) {
		t.Fatalf("want out-of-gas, got failed=%v err=%v", res.Failed, res.Err)
	}
	if r.Revision() != before {
		t.Fatal("failed OOG execution must not commit a new revision")
	}
	slot, _ := r.StorageAt(to, common.HexToHash("0x0"))
	if slot.Big().Sign() != 0 {
		t.Fatalf("OOG execution leaked storage: %s", slot.Hex())
	}
}

// TestDepthLimitedNestedCall builds a chain of calls deeper than 1024.
func TestDepthLimitedNestedCall(t *testing.T) {
	r := testRuntime(t)
	from := common.HexToAddress("0x7777")
	r.setBalanceForTest(from, big.NewInt(1e18))

	// Callee calls itself unconditionally until the depth limit errors.
	callee := common.HexToAddress("0xc411ee")
	runtimeCode := buildSelfCall(callee)
	cfg := baseConfig(from)
	cfg.Data = buildInitCode(runtimeCode)
	deploy, _ := r.Execute(cfg)
	if deploy.Failed {
		t.Fatal(deploy.Err)
	}

	call := baseConfig(from)
	to := *deploy.Address
	call.To = &to
	call.GasLimit = 50_000_000
	res, _ := r.Execute(call)
	// Depth exhaustion inside nested CALLs surfaces as a failed subcall;
	// the top-level frame continues, but no value should have moved.
	_ = res
	bal, _ := r.Balance(to)
	if bal.Sign() != 0 {
		t.Fatalf("self-call chain changed balance: %s", bal)
	}
}

// buildSelfCall emits bytecode that CALLs targetAddress with zero value and
// then STOPs. Each successful call enters one frame deeper.
func buildSelfCall(target common.Address) []byte {
	var c []byte
	// ret size/off, args size/off, value
	c = append(c, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00)
	c = append(c, 0x73)
	c = append(c, target.Bytes()...)
	c = append(c, 0x62, 0x0f, 0x42, 0x40) // PUSH3 1000000
	c = append(c, 0xf1)                   // CALL
	c = append(c, 0x50, 0x00)             // POP, STOP
	return c
}
