package evm

import (
	"math/big"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
)

// TestImplicitHaltIsSuccess verifies that bytecode ending without an explicit
// terminating instruction halts successfully (like STOP) instead of raising
// a runtime bounds error.
func TestImplicitHaltIsSuccess(t *testing.T) {
	ev, _ := testEVM(t)
	// PUSH1 0x2a and then the code simply ends.
	out, left, err := runCodeAt(t, ev, common.Address{0xd0}, []byte{0x60, 0x2a}, 100_000)
	if err != nil {
		t.Fatalf("implicit termination must succeed, got %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("implicit termination must return no data, got %x", out)
	}
	// Only the PUSH static cost (3) is consumed.
	if used := 100_000 - left; used != GasFastestStep {
		t.Fatalf("gas used = %d, want %d", used, GasFastestStep)
	}
}

// TestImplicitHaltTruncatedPush: a PUSH whose operand bytes run past the end
// of the code is padded with zeros and then halts successfully.
func TestImplicitHaltTruncatedPush(t *testing.T) {
	ev, _ := testEVM(t)
	out, left, err := runCodeAt(t, ev, common.Address{0xd1}, []byte{0x60}, 100_000)
	if err != nil {
		t.Fatalf("truncated PUSH must succeed, got %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected no output, got %x", out)
	}
	if used := 100_000 - left; used != GasFastestStep {
		t.Fatalf("gas used = %d, want %d", used, GasFastestStep)
	}
}

// TestImplicitHaltMatchesExplicitStop compares a program that falls off the
// end with the same program plus an explicit STOP: gas consumption and state
// changes must be identical.
func TestImplicitHaltMatchesExplicitStop(t *testing.T) {
	// SSTORE slot0=42, then either fall off the end or STOP explicitly.
	implicit := []byte{0x60, 0x2a, 0x60, 0x00, 0x55}
	explicit := []byte{0x60, 0x2a, 0x60, 0x00, 0x55, 0x00}

	ev1, db1 := testEVM(t)
	_, left1, err1 := runCodeAt(t, ev1, common.Address{0xd2}, implicit, 100_000)
	ev2, db2 := testEVM(t)
	_, left2, err2 := runCodeAt(t, ev2, common.Address{0xd2}, explicit, 100_000)

	if err1 != nil || err2 != nil {
		t.Fatalf("both programs must succeed: implicit=%v explicit=%v", err1, err2)
	}
	if left1 != left2 {
		t.Fatalf("gas left differs: implicit=%d explicit=%d", left1, left2)
	}
	key := common.Hash{}
	v1 := db1.GetState(common.Address{0xd2}, key)
	v2 := db2.GetState(common.Address{0xd2}, key)
	if v1 != v2 || v1.Big().Cmp(big.NewInt(0x2a)) != 0 {
		t.Fatalf("storage differs: implicit=%s explicit=%s", v1.Hex(), v2.Hex())
	}
}

// TestImplicitHaltAfterJump: a JUMP landing on the last byte (JUMPDEST) and
// then falling off the end is also a legal termination.
func TestImplicitHaltAfterJump(t *testing.T) {
	ev, _ := testEVM(t)
	// PUSH1 4 JUMP ... JUMPDEST at pc=4, then code ends.
	code := []byte{0x60, 0x04, 0x56, 0x00, 0x5b}
	_, _, err := runCodeAt(t, ev, common.Address{0xd3}, code, 100_000)
	if err != nil {
		t.Fatalf("jump to final JUMPDEST then implicit halt must succeed, got %v", err)
	}
}

// TestCreateWithEmptyInitCode: empty init code runs zero instructions and
// deploys an empty runtime code blob successfully.
func TestCreateWithEmptyInitCode(t *testing.T) {
	ev, db := testEVM(t)
	caller := common.Address{0xab}
	db.AddBalance(caller, big.NewInt(1e18))
	_, addr, _, err := ev.Create(caller, nil, 100_000, new(big.Int))
	if err != nil {
		t.Fatalf("empty init code must succeed, got %v", err)
	}
	if !db.Exist(addr) {
		t.Fatal("created account must exist")
	}
}
