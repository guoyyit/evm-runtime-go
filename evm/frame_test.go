package evm

import (
	"math/big"
	"testing"
)

func TestFrameSnapshotRestoresStackMemoryPCGas(t *testing.T) {
	c := &Contract{
		Self: addrAt(3),
		Code: []byte{0x01, 0x02, 0x03},
		Gas:  5000,
	}
	f := newFrame(c, false)

	// Baseline: one stack value, small memory, pc=1.
	if err := f.stack.Push(big.NewInt(42)); err != nil {
		t.Fatal(err)
	}
	f.memory.Resize(32)
	f.memory.Set(0, []byte{0xab})
	f.pc = 1
	f.lastReturn = []byte{0x01}
	base := f.snapshot()

	// Mutate volatile state heavily.
	_ = f.stack.Push(big.NewInt(7))
	_ = f.stack.Push(big.NewInt(8))
	f.memory.Resize(128)
	f.memory.Set(64, []byte{0xcd})
	f.pc = 2
	c.Gas = 10
	f.lastReturn = []byte{0x09}

	f.restore(base)

	if f.stack.Len() != 1 {
		t.Fatalf("stack len = %d, want 1", f.stack.Len())
	}
	v, _ := f.stack.Peek(0)
	if v.Int64() != 42 {
		t.Fatalf("stack top = %d, want 42", v.Int64())
	}
	if f.memory.Len() != 32 || f.memory.Get(0, 1)[0] != 0xab {
		t.Fatal("memory not restored")
	}
	if f.pc != 1 || c.Gas != 5000 {
		t.Fatalf("pc/gas not restored: pc=%d gas=%d", f.pc, c.Gas)
	}
	if len(f.lastReturn) != 1 || f.lastReturn[0] != 0x01 {
		t.Fatal("return data not restored")
	}

	// Restoring must be a deep copy: mutating again must not change snapshot.
	_ = f.stack.Push(big.NewInt(99))
	if len(base.stack) != 1 {
		t.Fatal("snapshot stack aliased")
	}
}
