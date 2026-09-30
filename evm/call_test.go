package evm

import (
	"errors"
	"testing"
)

// callee runtime: SSTORE slot5 = callvalue, then return 32-byte 7.
func calleeCode() []byte {
	return []byte{
		0x34,       // CALLVALUE (pushed first, ends below)
		0x60, 0x05, // key=5 (top; SSTORE pops key then value)
		0x55, // SSTORE
		0x60, 0x07, 0x60, 0x00, 0x52,
		0x60, 0x20, 0x60, 0x00, 0xf3,
	}
}

// buildCaller constructs a contract that CALLs target with the given value,
// no input, and copies up to 32 bytes of return data to memory starting at 0.
// It STOPs on success and REVERTs (0,0) on failure.
func buildCaller(target [20]byte, value uint64, static bool) []byte {
	var c []byte
	// Build CALL args by pushing in REVERSE pop order. POP order is:
	// gas, addr, value, inOff, inSize, retOff, retSize.
	// retSize=32
	c = append(c, 0x60, 0x20)
	// retOff=0
	c = append(c, 0x60, 0x00)
	// inSize=0
	c = append(c, 0x60, 0x00)
	// inOff=0
	c = append(c, 0x60, 0x00)
	// value
	c = append(c, pushUint(value)...)
	// addr (PUSH20)
	c = append(c, 0x73)
	c = append(c, target[:]...)
	// gas: GAS
	c = append(c, 0x5a)
	// CALL or STATICCALL
	if static {
		c = append(c, 0xfa)
	} else {
		c = append(c, 0xf1)
	}
	// success flag on top. If zero, REVERT; else return 32 bytes at 0.
	// Success JUMPDEST sits after PUSH1/JUMPI (3 bytes) and the 5-byte
	// REVERT(0,0) failure path.
	jumpDest := len(c) + 3 + 5
	c = append(c,
		0x60, byte(jumpDest),
		0x57,                         // JUMPI pops cond(top=success), dest
		0x60, 0x00, 0x60, 0x00, 0xfd, // REVERT 0,0
		0x5b,                         // JUMPDEST (success)
		0x60, 0x20, 0x60, 0x00, 0xf3, // RETURN 0,32
	)
	return c
}

// pushUint emits the shortest PUSH<n> for a value.
func pushUint(v uint64) []byte {
	var b []byte
	for v > 0 {
		b = append([]byte{byte(v)}, b...)
		v >>= 8
	}
	if len(b) == 0 {
		return []byte{0x5f} // PUSH0
	}
	return append([]byte{0x5f + byte(len(b))}, b...)
}

func TestContractCallWithValue(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000000000"))

	callee := deploy(t, r, from, calleeCode())
	caller := deploy(t, r, from, buildCaller(callee, 100, false))

	beforeCallee := r.Balance(callee)
	res, _ := r.Execute(Message{From: from, To: &caller, GasLimit: 2_000_000})
	if res.Err != nil {
		t.Fatalf("call: %v used=%d", res.Err, res.GasUsed)
	}
	if len(res.ReturnData) != 32 || res.ReturnData[31] != 7 {
		t.Fatalf("callee output = %x", res.ReturnData)
	}
	if r.Balance(callee) != addDec(beforeCallee, 100) {
		t.Fatalf("callee balance = %s, want +100", r.Balance(callee))
	}
	var slot5 [32]byte
	slot5[31] = 5
	if got := r.StorageAt(callee, slot5); got[31] != 100 {
		t.Fatalf("callee slot5 = %x, want 100", got)
	}
}

func TestCalleeRevertRollsBack(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000000000"))

	revertCallee := []byte{
		0x60, 0x00, // key=0
		0x60, 0x09, // value=9
		0x55,
		0x60, 0x00, 0x60, 0x00, 0xfd,
	}
	callee := deploy(t, r, from, revertCallee)
	caller := deploy(t, r, from, buildCaller(callee, 100, false))

	res, _ := r.Execute(Message{From: from, To: &caller, GasLimit: 2_000_000})
	// Caller itself REVERTs when child fails, so the whole tx returns the
	// reverted error but must not leak the callee's storage or value.
	if !errors.Is(res.Err, ErrExecutionReverted) && res.Err != nil {
		t.Fatalf("unexpected: %v", res.Err)
	}
	var slot0 [32]byte
	if got := r.StorageAt(callee, slot0); got != [32]byte{} {
		t.Fatalf("reverted callee storage leaked: %x", got)
	}
	if r.Balance(callee) != "0" {
		t.Fatalf("reverted call left value: %s", r.Balance(callee))
	}
}

func TestUnusedGasReturned(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000000000"))
	callee := deploy(t, r, from, []byte{0x00})
	caller := deploy(t, r, from, buildCaller(callee, 0, false))

	res, _ := r.Execute(Message{From: from, To: &caller, GasLimit: 500_000})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if res.GasUsed > 100_000 {
		t.Fatalf("unused child gas not returned, used=%d", res.GasUsed)
	}
}

func TestStaticCallCannotSendValue(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000000000"))
	target := addrAt(77)
	// A contract that, when run statically, executes STATICCALL fine but we
	// instead test that a value-bearing CALL inside a static frame is rejected.
	code := buildCaller(target, 1, false)
	addr := deploy(t, r, from, code)
	res, _ := r.ExecuteStatic(from, addr, nil, 500_000)
	if !errors.Is(res.Err, ErrWriteInReadOnly) {
		t.Fatalf("want readonly violation, got %v", res.Err)
	}
}

func addDec(s string, n int64) string {
	v := bigWei(s)
	return v.Add(v, bigWeiI(n)).String()
}
