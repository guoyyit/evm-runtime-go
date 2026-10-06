package runtime

import (
	"errors"
	"math/big"
	"sync"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
	"github.com/guoyyit/evm-runtime-go/evm"
)

// TestImplicitHaltSuccess: bytecode that ends without STOP/RETURN/REVERT
// halts successfully (implicit STOP), with gas and state changes identical
// to an explicitly terminated twin program.
func TestImplicitHaltSuccess(t *testing.T) {
	r := testRuntime(t)
	from := common.HexToAddress("0xaaaa")
	if err := r.setBalanceForTest(from, big.NewInt(1e18)); err != nil {
		t.Fatal(err)
	}

	deploy := func(runtimeCode []byte) common.Address {
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
	call := func(to common.Address) *Result {
		cfg := baseConfig(from)
		cfg.To = &to
		res, err := r.Execute(cfg)
		if err != nil {
			t.Fatalf("call transport: %v", err)
		}
		return res
	}

	// SSTORE slot7=0x2a, then fall off the end (no STOP).
	implicit := deploy([]byte{0x60, 0x2a, 0x60, 0x07, 0x55})
	// Same program with an explicit STOP terminator.
	explicit := deploy([]byte{0x60, 0x2a, 0x60, 0x07, 0x55, 0x00})

	ri := call(implicit)
	re := call(explicit)
	if ri.Failed {
		t.Fatalf("implicit-halt program failed: %v", ri.Err)
	}
	if re.Failed {
		t.Fatalf("explicit-stop program failed: %v", re.Err)
	}
	// Identical resource consumption: STOP itself costs 0 gas.
	if ri.GasUsed != re.GasUsed {
		t.Fatalf("gas mismatch: implicit=%d explicit=%d", ri.GasUsed, re.GasUsed)
	}
	// 2*PUSH1 (3+3) + SSTORE cold set (2600+20000).
	const wantGas = 3 + 3 + 2600 + 20000
	if ri.GasUsed != wantGas {
		t.Fatalf("gas used = %d, want %d", ri.GasUsed, wantGas)
	}
	if ri.GasLeft != 10_000_000-wantGas {
		t.Fatalf("gas left = %d", ri.GasLeft)
	}
	// Identical state change.
	for _, addr := range []common.Address{implicit, explicit} {
		slot, err := r.StorageAt(addr, common.HexToHash("0x7"))
		if err != nil {
			t.Fatal(err)
		}
		if slot.Big().Cmp(big.NewInt(0x2a)) != 0 {
			t.Fatalf("slot7 = %s, want 0x2a", slot.Hex())
		}
	}

	// A trailing PUSH that runs off the end is also a clean halt.
	pushOnly := deploy([]byte{0x60, 0x2a})
	rp := call(pushOnly)
	if rp.Failed {
		t.Fatalf("trailing-push program failed: %v", rp.Err)
	}
	if rp.GasUsed != 3 {
		t.Fatalf("trailing-push gas = %d, want 3", rp.GasUsed)
	}
}

// TestImplicitHaltCreate: init code without a terminator deploys empty
// runtime code successfully instead of erroring.
func TestImplicitHaltCreate(t *testing.T) {
	r := testRuntime(t)
	from := common.HexToAddress("0xbb")
	if err := r.setBalanceForTest(from, big.NewInt(1e18)); err != nil {
		t.Fatal(err)
	}
	cfg := baseConfig(from)
	cfg.Data = []byte{0x60, 0x00} // PUSH1 0, then fall off: returns nothing
	res, err := r.Execute(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("unterminated init code failed: %v", res.Err)
	}
	if res.Address == nil {
		t.Fatal("missing created address")
	}
	code, err := r.Code(*res.Address)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 0 {
		t.Fatalf("deployed code = %x, want empty", code)
	}
}

// TestRevertGasMetering: a reverting execution reports the gas actually
// consumed up to the REVERT, never the flat budget, and the figure is
// reproducible across identical runs.
func TestRevertGasMetering(t *testing.T) {
	// SSTORE slot0=1; MSTORE(0, 0x2a); REVERT(0, 32).
	code := []byte{
		0x60, 0x01, 0x60, 0x00, 0x55, // SSTORE
		0x60, 0x2a, 0x60, 0x00, 0x52, // MSTORE
		0x60, 0x20, 0x60, 0x00, 0xfd, // REVERT(0, 32)
	}
	// 2*PUSH1 + SSTORE cold set + 2*PUSH1 + MSTORE(3+3 mem) + 2*PUSH1 + REVERT(0).
	const wantGas = 3 + 3 + (2600 + 20000) + 3 + 3 + (3 + 3) + 3 + 3 + 0

	run := func(t *testing.T) *Result {
		r := testRuntime(t)
		from := common.HexToAddress("0xcccc")
		if err := r.setBalanceForTest(from, big.NewInt(1e18)); err != nil {
			t.Fatal(err)
		}
		cfg := baseConfig(from)
		cfg.Data = buildInitCode(code)
		deploy, err := r.Execute(cfg)
		if err != nil || deploy.Failed {
			t.Fatalf("deploy: %v %v", err, deploy.Err)
		}
		call := baseConfig(from)
		call.To = deploy.Address
		res, err := r.Execute(call)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	res := run(t)
	if !res.Failed || !errors.Is(res.Err, evm.ErrExecutionReverted) {
		t.Fatalf("want revert, got failed=%v err=%v", res.Failed, res.Err)
	}
	if res.GasUsed != wantGas {
		t.Fatalf("revert gas used = %d, want actual %d (not the budget)", res.GasUsed, wantGas)
	}
	if res.GasLeft != 10_000_000-wantGas {
		t.Fatalf("revert gas left = %d, want %d", res.GasLeft, 10_000_000-wantGas)
	}
	if len(res.ReturnData) != 32 || res.ReturnData[31] != 0x2a {
		t.Fatalf("revert payload = %x", res.ReturnData)
	}
	// Reproducible: identical input yields identical metering.
	again := run(t)
	if again.GasUsed != res.GasUsed || again.GasLeft != res.GasLeft {
		t.Fatalf("metering not reproducible: %d/%d vs %d/%d",
			again.GasUsed, again.GasLeft, res.GasUsed, res.GasLeft)
	}
}

// TestFailureGasMetering distinguishes failure classes by their metering:
// OOG consumes the whole budget, while INVALID and stack faults report
// only what was actually consumed.
func TestFailureGasMetering(t *testing.T) {
	r := testRuntime(t)
	from := common.HexToAddress("0xdddd")
	if err := r.setBalanceForTest(from, big.NewInt(1e18)); err != nil {
		t.Fatal(err)
	}
	deploy := func(code []byte) common.Address {
		cfg := baseConfig(from)
		cfg.Data = buildInitCode(code)
		res, err := r.Execute(cfg)
		if err != nil || res.Failed {
			t.Fatalf("deploy: %v %v", err, res.Err)
		}
		return *res.Address
	}
	call := func(to common.Address, gas uint64) *Result {
		cfg := baseConfig(from)
		cfg.To = &to
		cfg.GasLimit = gas
		res, err := r.Execute(cfg)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	// Out of gas: infinite loop, budget fully consumed.
	looper := deploy([]byte{0x5b, 0x60, 0x00, 0x56}) // JUMPDEST; PUSH1 0; JUMP
	res := call(looper, 50_000)
	if !res.Failed || !errors.Is(res.Err, evm.ErrOutOfGas) {
		t.Fatalf("want OOG, got %v", res.Err)
	}
	if res.GasUsed != 50_000 || res.GasLeft != 0 {
		t.Fatalf("OOG metering: used=%d left=%d, want 50000/0", res.GasUsed, res.GasLeft)
	}

	// Invalid opcode: only the instructions before it are consumed.
	invalid := deploy([]byte{0x60, 0x01, 0xfe}) // PUSH1 1; INVALID
	res = call(invalid, 1_000_000)
	if !res.Failed || !errors.Is(res.Err, evm.ErrInvalidOpcode) {
		t.Fatalf("want invalid opcode, got %v", res.Err)
	}
	if res.GasUsed != 3 || res.GasLeft != 1_000_000-3 {
		t.Fatalf("invalid-opcode metering: used=%d left=%d, want 3/999997", res.GasUsed, res.GasLeft)
	}

	// Stack underflow: fails validation before any gas is charged.
	underflow := deploy([]byte{0x50}) // POP on empty stack
	res = call(underflow, 1_000_000)
	if !res.Failed || !errors.Is(res.Err, evm.ErrStackUnderflow) {
		t.Fatalf("want stack underflow, got %v", res.Err)
	}
	if res.GasUsed != 0 || res.GasLeft != 1_000_000 {
		t.Fatalf("underflow metering: used=%d left=%d, want 0/1000000", res.GasUsed, res.GasLeft)
	}
}

// callBytecode builds: PUSH1 0 x4 (ret/args range), PUSH1 value, PUSH20 to,
// PUSH2 gas, CALL, POP. The caller falls through afterwards.
func callBytecode(to common.Address, value byte, gas uint16) []byte {
	b := []byte{0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, value, 0x73}
	b = append(b, to.Bytes()...)
	b = append(b, 0x61, byte(gas>>8), byte(gas))
	return append(b, 0xf1, 0x50)
}

// revertBytecode appends REVERT(0,0).
func revertBytecode() []byte { return []byte{0x60, 0x00, 0x60, 0x00, 0xfd} }

// setupNestedRevert deploys C (calls target with 1 wei, then reverts) and P
// (calls C with 5 wei, then runs tail, then falls off the end implicitly).
// It returns the runtime with everything deployed and the addresses.
func setupNestedRevert(t *testing.T, r *Runtime, from common.Address, target common.Address, tail []byte) (c, p common.Address) {
	t.Helper()
	deploy := func(code []byte) common.Address {
		cfg := baseConfig(from)
		cfg.Data = buildInitCode(code)
		res, err := r.Execute(cfg)
		if err != nil || res.Failed {
			t.Fatalf("deploy: %v %v", err, res.Err)
		}
		return *res.Address
	}
	cCode := append(callBytecode(target, 1, 0), revertBytecode()...)
	c = deploy(cCode)
	pCode := append(callBytecode(c, 5, 0xffff), tail...)
	p = deploy(pCode)
	return c, p
}

// TestNestedRevertRollbackConsistency: a child call creates and funds a fresh
// account, then reverts. The rollback must erase every trace of the child:
// the commit afterwards must not panic or leak the rolled-back account, and
// re-running the identical scenario must reproduce gas and state exactly.
func TestNestedRevertRollbackConsistency(t *testing.T) {
	from := common.HexToAddress("0xeeee")
	fresh := common.HexToAddress("0xf1e57")

	run := func(t *testing.T) (*Result, *Runtime) {
		r := testRuntime(t)
		if err := r.setBalanceForTest(from, big.NewInt(1e18)); err != nil {
			t.Fatal(err)
		}
		// P only calls C, then halts implicitly (no retouch of `fresh`).
		c, p := setupNestedRevert(t, r, from, fresh, nil)
		cfg := baseConfig(from)
		cfg.To = &p
		cfg.Value = big.NewInt(10)
		res, err := r.Execute(cfg)
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if res.Failed {
			t.Fatalf("top-level must succeed despite child revert: %v", res.Err)
		}
		// Rolled-back effects must be gone: C kept nothing, fresh never appeared.
		if bal, _ := r.Balance(c); bal.Sign() != 0 {
			t.Fatalf("child balance leaked: %s", bal)
		}
		if bal, _ := r.Balance(fresh); bal.Sign() != 0 {
			t.Fatalf("rolled-back account leaked balance: %s", bal)
		}
		if bal, _ := r.Balance(p); bal.Cmp(big.NewInt(10)) != 0 {
			t.Fatalf("parent balance = %s, want 10", bal)
		}
		return res, r
	}

	first, r1 := run(t)
	second, r2 := run(t)
	if first.GasUsed != second.GasUsed || first.GasLeft != second.GasLeft {
		t.Fatalf("gas not reproducible after rollback: %d/%d vs %d/%d",
			first.GasUsed, first.GasLeft, second.GasUsed, second.GasLeft)
	}
	// Final committed state must be byte-identical across the two runs.
	for _, addr := range []common.Address{from, fresh} {
		b1, _ := r1.Balance(addr)
		b2, _ := r2.Balance(addr)
		if b1.Cmp(b2) != 0 {
			t.Fatalf("state diverged for %s: %s vs %s", addr.Hex(), b1, b2)
		}
	}
}

// TestRetouchAfterRollback: after a child call touching F reverts, the parent
// calls F again. F must be treated as brand new (new-account gas, cold
// access), so two scenarios — one where the child touched F, one where it
// touched an unrelated account G — must consume identical gas.
func TestRetouchAfterRollback(t *testing.T) {
	from := common.HexToAddress("0xef01")
	fresh := common.HexToAddress("0xf2e57")
	other := common.HexToAddress("0x607e57")

	run := func(t *testing.T, childTarget common.Address) (*Result, common.Address, common.Address) {
		r := testRuntime(t)
		if err := r.setBalanceForTest(from, big.NewInt(1e18)); err != nil {
			t.Fatal(err)
		}
		// P calls C (which touches childTarget then reverts), then calls
		// `fresh` with 1 wei, then halts implicitly.
		tail := callBytecode(fresh, 1, 0xffff)
		c, p := setupNestedRevert(t, r, from, childTarget, tail)
		cfg := baseConfig(from)
		cfg.To = &p
		cfg.Value = big.NewInt(10)
		res, err := r.Execute(cfg)
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if res.Failed {
			t.Fatalf("execute failed: %v", res.Err)
		}
		if bal, _ := r.Balance(fresh); bal.Cmp(big.NewInt(1)) != 0 {
			t.Fatalf("fresh balance = %s, want 1", bal)
		}
		if bal, _ := r.Balance(p); bal.Cmp(big.NewInt(9)) != 0 {
			t.Fatalf("parent balance = %s, want 9", bal)
		}
		return res, c, p
	}

	resA, _, _ := run(t, fresh) // child touched F, rolled back, parent retouches F
	resB, _, _ := run(t, other) // child touched G; parent touches F (brand new)
	if resA.GasUsed != resB.GasUsed {
		t.Fatalf("rolled-back account changed gas metering: touched=%d untouched=%d",
			resA.GasUsed, resB.GasUsed)
	}
}

// TestConcurrentSameAccountCommits hammers one account with hundreds of
// concurrent commits. Balance deltas must merge additively (no lost update,
// no overwrite) and the final state must equal the serial theoretical value
// regardless of interleaving or commit order.
func TestConcurrentSameAccountCommits(t *testing.T) {
	r := testRuntime(t)
	const n = 400

	treasury := common.HexToAddress("0x7ea5")
	if err := r.setBalanceForTest(treasury, big.NewInt(1e18)); err != nil {
		t.Fatal(err)
	}
	target := common.HexToAddress("0x7a96e7")

	// Half the commits share one treasury as funder; the other half use
	// distinct pre-funded accounts. All credit the same target with 1 wei.
	funders := make([]common.Address, n)
	for i := range funders {
		if i%2 == 0 {
			funders[i] = treasury
			continue
		}
		funders[i] = common.Address{byte(i >> 8), byte(i), 0xf0}
		if err := r.setBalanceForTest(funders[i], big.NewInt(100)); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			to := target
			cfg := baseConfig(funders[i])
			cfg.To = &to
			cfg.Value = big.NewInt(1)
			res, err := r.Execute(cfg)
			if err != nil {
				errs <- err
				return
			}
			if res.Failed {
				errs <- res.Err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent execute: %v", err)
	}

	// The shared target must have collected every single wei.
	bal, err := r.Balance(target)
	if err != nil {
		t.Fatal(err)
	}
	if bal.Cmp(big.NewInt(n)) != 0 {
		t.Fatalf("target balance = %s, want %d (lost update)", bal, n)
	}
	// The shared treasury was debited n/2 times.
	treasuryBal, err := r.Balance(treasury)
	if err != nil {
		t.Fatal(err)
	}
	wantTreasury := new(big.Int).Sub(big.NewInt(1e18), big.NewInt(n/2))
	if treasuryBal.Cmp(wantTreasury) != 0 {
		t.Fatalf("treasury balance = %s, want %s (lost update)", treasuryBal, wantTreasury)
	}
	// Distinct funders each spent exactly 1.
	for i := 1; i < n; i += 2 {
		b, err := r.Balance(funders[i])
		if err != nil {
			t.Fatal(err)
		}
		if b.Cmp(big.NewInt(99)) != 0 {
			t.Fatalf("funder %d balance = %s, want 99", i, b)
		}
	}
}

// TestConcurrentSameAccountStorage: hundreds of concurrent calls to one
// contract, each writing its own caller-keyed storage slot. Per-slot merge
// must preserve every write.
func TestConcurrentSameAccountStorage(t *testing.T) {
	r := testRuntime(t)
	from := common.HexToAddress("0xd0")
	if err := r.setBalanceForTest(from, big.NewInt(1e18)); err != nil {
		t.Fatal(err)
	}

	// runtime code: SSTORE(key=CALLER, value=1), then implicit halt.
	code := []byte{0x60, 0x01, 0x33, 0x55}
	cfg := baseConfig(from)
	cfg.Data = buildInitCode(code)
	deploy, err := r.Execute(cfg)
	if err != nil || deploy.Failed {
		t.Fatalf("deploy: %v %v", err, deploy.Err)
	}
	contract := *deploy.Address

	const n = 300
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			caller := common.Address{byte(i >> 8), byte(i), 0xca}
			cfg := baseConfig(caller)
			cfg.To = &contract
			res, err := r.Execute(cfg)
			if err != nil {
				errs <- err
				return
			}
			if res.Failed {
				errs <- res.Err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent call: %v", err)
	}

	// Every caller's slot survived: no write was lost or overwritten.
	for i := 0; i < n; i++ {
		caller := common.Address{byte(i >> 8), byte(i), 0xca}
		key := common.BytesToHash(caller.Bytes())
		got, err := r.StorageAt(contract, key)
		if err != nil {
			t.Fatal(err)
		}
		if got.Big().Cmp(big.NewInt(1)) != 0 {
			t.Fatalf("slot[%s] = %s, want 1", caller.Hex(), got.Hex())
		}
	}
}
