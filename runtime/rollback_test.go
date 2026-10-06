package runtime

import (
	"math/big"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
)

// buildInitCodeWithPrefix wraps runtime code into init code that first runs
// prefix (e.g. to seed storage) and then deploys runtime.
func buildInitCodeWithPrefix(prefix, runtime []byte) []byte {
	length := len(runtime)
	offset := len(prefix) + 14
	init := append([]byte(nil), prefix...)
	init = append(init,
		0x61, byte(length>>8), byte(length), // PUSH2 length
		0x60, byte(offset), // PUSH1 offset
		0x60, 0x00, // PUSH1 0
		0x39,                                // CODECOPY
		0x61, byte(length>>8), byte(length), // PUSH2 length
		0x60, 0x00, // PUSH1 0
		0xf3, // RETURN
	)
	return append(init, runtime...)
}

// buildCallerCode emits runtime code that CALLs target with empty input and
// no value, drops the result and stops.
func buildCallerCode(target common.Address) []byte {
	c := []byte{
		0x60, 0x00, // retSize
		0x60, 0x00, // retOffset
		0x60, 0x00, // argSize
		0x60, 0x00, // argOffset
		0x60, 0x00, // value
		0x73, // PUSH20
	}
	c = append(c, target.Bytes()...)
	c = append(c,
		0x61, 0xff, 0xff, // gas
		0xf1, // CALL
		0x50, // POP
		0x00, // STOP
	)
	return c
}

// TestRevertedChildRefundDoesNotLeak: a child frame that earns an SSTORE
// clearing refund and then REVERTS must not pass that refund up to the
// transaction. The same program with a RETURNING child (refund legitimately
// kept) differs from the reverting one exactly by the applied refund.
func TestRevertedChildRefundDoesNotLeak(t *testing.T) {
	from := common.HexToAddress("0xro11")
	const gasLimit = 1_000_000

	// runVariant deploys the child (slot0 pre-seeded to 5, then clears it and
	// ends with the given terminator) plus a parent that calls it, executes
	// the call and returns the result.
	runVariant := func(t *testing.T, terminator byte) (*Runtime, common.Address, *Result) {
		r := testRuntime(t)
		if err := r.setBalanceForTest(from, big.NewInt(1e18)); err != nil {
			t.Fatal(err)
		}
		// Child: PUSH1 0 DUP1 SSTORE (clears slot0, earns 4800 refund),
		// then PUSH1 0 DUP1 <terminator>. Both terminators (RETURN/REVERT)
		// cost 0, so raw gas is identical across variants.
		childRuntime := []byte{0x60, 0x00, 0x80, 0x55, 0x60, 0x00, 0x80, terminator}
		childInit := buildInitCodeWithPrefix(
			[]byte{0x60, 0x05, 0x60, 0x00, 0x55}, // SSTORE slot0 = 5
			childRuntime,
		)
		cfg := baseConfig(from)
		cfg.Data = childInit
		childRes, err := r.Execute(cfg)
		if err != nil || childRes.Failed {
			t.Fatalf("deploy child: %v %v", err, childRes.Err)
		}
		child := *childRes.Address

		parentCfg := baseConfig(from)
		parentCfg.Data = buildInitCode(buildCallerCode(child))
		parentRes, err := r.Execute(parentCfg)
		if err != nil || parentRes.Failed {
			t.Fatalf("deploy parent: %v %v", err, parentRes.Err)
		}
		parent := *parentRes.Address

		res := callContract(t, r, from, parent, gasLimit)
		return r, child, res
	}

	rRet, childRet, resReturn := runVariant(t, 0xf3) // child RETURNs
	rRev, childRev, resRevert := runVariant(t, 0xfd) // child REVERTs

	if resReturn.Failed || resRevert.Failed {
		t.Fatalf("parent executions must succeed: return=%v revert=%v", resReturn.Err, resRevert.Err)
	}

	// The reverting child keeps no refund: its execution reports the raw
	// consumption. The returning child gets back min(raw/5, 4800).
	raw := resRevert.GasUsed
	refund := raw / 5
	if refund > 4800 {
		refund = 4800
	}
	if refund == 0 {
		t.Fatal("test expects a non-zero capped refund")
	}
	if resReturn.GasUsed != raw-refund {
		t.Fatalf("returning child: used=%d, want %d (raw %d - refund %d)",
			resReturn.GasUsed, raw-refund, raw, refund)
	}

	// The returning child's clear is committed; the reverting child's is not.
	if slot, _ := rRet.StorageAt(childRet, common.Hash{}); slot.Big().Sign() != 0 {
		t.Fatalf("returning child must commit the clear, got %s", slot.Hex())
	}
	if slot, _ := rRev.StorageAt(childRev, common.Hash{}); slot.Big().Cmp(big.NewInt(5)) != 0 {
		t.Fatalf("reverted child clear leaked: %s", slot.Hex())
	}
}

// TestRevertedChildLeavesNoTrace: a child that writes storage and emits a
// log before REVERTING leaves no committed trace, and re-executing the same
// call reproduces identical gas and state.
func TestRevertedChildLeavesNoTrace(t *testing.T) {
	r := testRuntime(t)
	from := common.HexToAddress("0xro12")
	if err := r.setBalanceForTest(from, big.NewInt(1e18)); err != nil {
		t.Fatal(err)
	}

	// Child: SSTORE slot1=0x77, LOG0(empty), REVERT(0,0).
	childRuntime := []byte{
		0x60, 0x77, 0x60, 0x01, 0x55, // SSTORE slot1 = 0x77
		0x60, 0x00, 0x60, 0x00, 0xa0, // LOG0(offset=0, length=0)
		0x60, 0x00, 0x60, 0x00, 0xfd, // REVERT(0, 0)
	}
	cfg := baseConfig(from)
	cfg.Data = buildInitCode(childRuntime)
	childRes, err := r.Execute(cfg)
	if err != nil || childRes.Failed {
		t.Fatalf("deploy child: %v %v", err, childRes.Err)
	}
	child := *childRes.Address

	parentCfg := baseConfig(from)
	parentCfg.Data = buildInitCode(buildCallerCode(child))
	parentRes, err := r.Execute(parentCfg)
	if err != nil || parentRes.Failed {
		t.Fatalf("deploy parent: %v %v", err, parentRes.Err)
	}
	parent := *parentRes.Address

	first := callContract(t, r, from, parent, 1_000_000)
	if first.Failed {
		t.Fatalf("parent must survive the child revert: %v", first.Err)
	}
	if len(first.Logs) != 0 {
		t.Fatalf("reverted child log leaked: %d logs", len(first.Logs))
	}
	slotKey := common.Hash{31: 1}
	if slot, _ := r.StorageAt(child, slotKey); slot.Big().Sign() != 0 {
		t.Fatalf("reverted child storage leaked: %s", slot.Hex())
	}

	// Re-execute the identical call: gas and state must match exactly.
	second := callContract(t, r, from, parent, 1_000_000)
	if second.Failed {
		t.Fatalf("second execution failed: %v", second.Err)
	}
	if first.GasUsed != second.GasUsed || first.GasLeft != second.GasLeft {
		t.Fatalf("gas not identical after re-execution: (%d,%d) vs (%d,%d)",
			first.GasUsed, first.GasLeft, second.GasUsed, second.GasLeft)
	}
	if len(second.Logs) != 0 {
		t.Fatalf("second execution leaked logs: %d", len(second.Logs))
	}
	if slot, _ := r.StorageAt(child, slotKey); slot.Big().Sign() != 0 {
		t.Fatalf("storage changed after re-execution: %s", slot.Hex())
	}
	logs, err := r.Logs()
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 0 {
		t.Fatalf("committed logs after reverted child calls: %d", len(logs))
	}
}
