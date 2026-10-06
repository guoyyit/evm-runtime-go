package runtime

import (
	"errors"
	"math/big"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
	"github.com/guoyyit/evm-runtime-go/evm"
)

// deployCode deploys runtimeCode and returns its address.
func deployCode(t *testing.T, r *Runtime, from common.Address, runtimeCode []byte) common.Address {
	t.Helper()
	cfg := baseConfig(from)
	cfg.Data = buildInitCode(runtimeCode)
	res, err := r.Execute(cfg)
	if err != nil {
		t.Fatalf("deploy transport: %v", err)
	}
	if res.Failed {
		t.Fatalf("deploy failed: %v", res.Err)
	}
	return *res.Address
}

// callContract runs a plain call against addr with the given gas limit.
func callContract(t *testing.T, r *Runtime, from, to common.Address, gas uint64) *Result {
	t.Helper()
	cfg := baseConfig(from)
	cfg.To = &to
	cfg.GasLimit = gas
	res, err := r.Execute(cfg)
	if err != nil {
		t.Fatalf("execute transport: %v", err)
	}
	return res
}

// TestRevertReportsActualGas: a reverting execution must report the gas it
// actually consumed — equal to the same program ending in STOP — and never
// the full budget.
func TestRevertReportsActualGas(t *testing.T) {
	r := testRuntime(t)
	from := common.HexToAddress("0xbeef01")
	if err := r.setBalanceForTest(from, big.NewInt(1e18)); err != nil {
		t.Fatal(err)
	}

	// Work: SSTORE slot0=1, MSTORE 0xaa at 0; both variants then push the
	// same (offset, length) and differ only in the final opcode, so the gas
	// consumed up to termination must be identical.
	work := []byte{
		0x60, 0x01, 0x60, 0x00, 0x55, // SSTORE slot0=1
		0x60, 0xaa, 0x60, 0x00, 0x52, // MSTORE(0, 0xaa)
		0x60, 0x20, 0x60, 0x00, // PUSH1 32, PUSH1 0
	}
	reverting := append(append([]byte(nil), work...), 0xfd) // REVERT(0, 32)
	returning := append(append([]byte(nil), work...), 0xf3) // RETURN(0, 32)

	stopAddr := deployCode(t, r, from, returning)
	revertAddr := deployCode(t, r, from, reverting)

	const gasLimit = 1_000_000
	stopRes := callContract(t, r, from, stopAddr, gasLimit)
	if stopRes.Failed {
		t.Fatalf("RETURN variant must succeed: %v", stopRes.Err)
	}

	revRes := callContract(t, r, from, revertAddr, gasLimit)
	if !revRes.Failed || !errors.Is(revRes.Err, evm.ErrExecutionReverted) {
		t.Fatalf("want revert, got failed=%v err=%v", revRes.Failed, revRes.Err)
	}
	if revRes.GasUsed != stopRes.GasUsed {
		t.Fatalf("revert gas used = %d, want %d (same as RETURN variant)", revRes.GasUsed, stopRes.GasUsed)
	}
	if revRes.GasUsed >= gasLimit || revRes.GasLeft == 0 {
		t.Fatalf("revert must not report the budget cap: used=%d left=%d", revRes.GasUsed, revRes.GasLeft)
	}
	if revRes.GasUsed+revRes.GasLeft != gasLimit {
		t.Fatalf("gas not conserved: used=%d left=%d limit=%d", revRes.GasUsed, revRes.GasLeft, gasLimit)
	}
	// The revert payload is the 32-byte word 0xaa.
	want := make([]byte, 32)
	want[31] = 0xaa
	if string(revRes.ReturnData) != string(want) {
		t.Fatalf("revert payload = %x, want %x", revRes.ReturnData, want)
	}
	// The reverted SSTORE must not be committed.
	if slot, _ := r.StorageAt(revertAddr, common.Hash{}); slot.Big().Sign() != 0 {
		t.Fatalf("reverted storage write leaked: %s", slot.Hex())
	}
}

// TestFailureGasAccountingDistinct pins down the per-cause gas reporting
// rules: each failure class reports its actual consumption and stays
// distinguishable via errors.Is.
func TestFailureGasAccountingDistinct(t *testing.T) {
	r := testRuntime(t)
	from := common.HexToAddress("0xbeef02")
	if err := r.setBalanceForTest(from, big.NewInt(1e18)); err != nil {
		t.Fatal(err)
	}

	const gasLimit = 200_000

	// Infinite loop burns every unit of gas.
	loopAddr := deployCode(t, r, from, []byte{0x5b, 0x60, 0x00, 0x56}) // JUMPDEST; PUSH1 0; JUMP
	// INVALID opcode.
	invalidAddr := deployCode(t, r, from, []byte{0xfe})
	// ADD on an empty stack.
	underflowAddr := deployCode(t, r, from, []byte{0x01})
	// Plain REVERT(0, 0) after one PUSH.
	revertAddr := deployCode(t, r, from, []byte{0x60, 0x00, 0x60, 0x00, 0xfd})

	cases := []struct {
		name     string
		to       common.Address
		wantErr  error
		wantUsed func(used uint64) bool
		desc     string
	}{
		{"out_of_gas", loopAddr, evm.ErrOutOfGas, func(u uint64) bool { return u == gasLimit }, "OOG consumes the whole budget"},
		{"invalid_opcode", invalidAddr, evm.ErrInvalidOpcode, func(u uint64) bool { return u == gasLimit }, "exceptional halt consumes the frame gas"},
		{"stack_underflow", underflowAddr, evm.ErrStackUnderflow, func(u uint64) bool { return u == gasLimit }, "exceptional halt consumes the frame gas"},
		{"revert", revertAddr, evm.ErrExecutionReverted, func(u uint64) bool { return u > 0 && u < gasLimit }, "revert reports only the consumed gas"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := callContract(t, r, from, tc.to, gasLimit)
			if !res.Failed || !errors.Is(res.Err, tc.wantErr) {
				t.Fatalf("want %v, got failed=%v err=%v", tc.wantErr, res.Failed, res.Err)
			}
			if !tc.wantUsed(res.GasUsed) {
				t.Fatalf("%s: gas used = %d (limit %d)", tc.desc, res.GasUsed, gasLimit)
			}
			if res.GasUsed+res.GasLeft != gasLimit {
				t.Fatalf("gas not conserved: used=%d left=%d", res.GasUsed, res.GasLeft)
			}
		})
	}

	// Pre-execution failure: nothing ran, nothing was consumed.
	poor := common.HexToAddress("0xbeef03")
	cfg := baseConfig(poor)
	cfg.To = &revertAddr
	cfg.Value = big.NewInt(1)
	res, err := r.Execute(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || !errors.Is(res.Err, evm.ErrInsufficientBalance) {
		t.Fatalf("want insufficient balance, got failed=%v err=%v", res.Failed, res.Err)
	}
	if res.GasUsed != 0 || res.GasLeft != cfg.GasLimit {
		t.Fatalf("pre-execution failure must report zero consumption: used=%d left=%d", res.GasUsed, res.GasLeft)
	}
}

// TestGasReportedReproducible: the same execution on the same initial state
// reports exactly the same gas consumption, including for failures.
func TestGasReportedReproducible(t *testing.T) {
	r := testRuntime(t)
	from := common.HexToAddress("0xbeef04")
	if err := r.setBalanceForTest(from, big.NewInt(1e18)); err != nil {
		t.Fatal(err)
	}
	revertAddr := deployCode(t, r, from, []byte{
		0x60, 0x01, 0x60, 0x00, 0x55, // SSTORE slot0=1
		0x60, 0x00, 0x60, 0x00, 0xfd, // REVERT(0, 0)
	})

	first := callContract(t, r, from, revertAddr, 500_000)
	second := callContract(t, r, from, revertAddr, 500_000)
	if !first.Failed || !second.Failed {
		t.Fatal("both executions must revert")
	}
	if first.GasUsed != second.GasUsed || first.GasLeft != second.GasLeft {
		t.Fatalf("gas not reproducible: first=(%d,%d) second=(%d,%d)",
			first.GasUsed, first.GasLeft, second.GasUsed, second.GasLeft)
	}
}
