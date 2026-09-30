package evm

import (
	"errors"
	"math/big"
	"math/rand"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
)

// deploy installs code at addr with an endowment so calls with value work.
func deployAt(t *testing.T, ev *EVM, addr common.Address, code []byte, fund *big.Int) {
	t.Helper()
	creator := common.Address{0x99}
	ev.StateDB.AddBalance(creator, new(big.Int).Mul(big.NewInt(1e18), big.NewInt(100)))
	ev.StateDB.CreateAccount(addr)
	ev.StateDB.SetCode(addr, code)
	if fund != nil && fund.Sign() > 0 {
		ev.StateDB.AddBalance(addr, fund)
	}
}

func TestCallDepthLimit(t *testing.T) {
	ev, _ := testEVM(t)
	ev.depth = MaxCallDepth

	caller := common.Address{1}
	target := common.Address{2}
	ev.StateDB.AddBalance(caller, big.NewInt(1e18))
	ev.StateDB.CreateAccount(target)

	_, _, err := ev.Call(caller, target, nil, 100_000, new(big.Int))
	if !errors.Is(err, ErrDepthLimitExceeded) {
		t.Fatalf("want depth limit, got %v", err)
	}
}

func TestCallValueTransfer(t *testing.T) {
	ev, _ := testEVM(t)
	from := common.Address{0x10}
	to := common.Address{0x11}
	ev.StateDB.AddBalance(from, big.NewInt(1000))

	// Callee just returns empty.
	ev.StateDB.CreateAccount(to)
	ev.StateDB.SetCode(to, []byte{0x00})

	_, _, err := ev.Call(from, to, nil, 100_000, big.NewInt(300))
	if err != nil {
		t.Fatal(err)
	}
	if ev.StateDB.GetBalance(from).Cmp(big.NewInt(700)) != 0 {
		t.Fatalf("from balance = %s", ev.StateDB.GetBalance(from))
	}
	if ev.StateDB.GetBalance(to).Cmp(big.NewInt(300)) != 0 {
		t.Fatalf("to balance = %s", ev.StateDB.GetBalance(to))
	}
}

func TestFailedSubcallRollsBack(t *testing.T) {
	ev, _ := testEVM(t)
	caller := common.Address{0x20}
	callee := common.Address{0x21}
	ev.StateDB.AddBalance(caller, big.NewInt(1e18))
	ev.StateDB.CreateAccount(callee)
	// Callee performs SSTORE then INVALID.
	ev.StateDB.SetCode(callee, []byte{
		0x60, 0x01, 0x60, 0x00, 0x55, // SSTORE slot0=1
		0xfe,
	})

	// Parent calls callee with zero value; the subcall frame failure must
	// revert callee storage and value movement.
	parent := []byte{
		0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00,
		0x60, 0x00, 0x60, callee[19], 0x60, 0x05, 0xf1,
		0x50, // pop success flag
		0x00,
	}
	parentAddr := common.Address{0x22}
	deployAt(t, ev, parentAddr, parent, nil)
	ev.StateDB.AddBalance(parentAddr, big.NewInt(1e18))

	_, _, err := ev.Call(caller, parentAddr, nil, 500_000, new(big.Int))
	if err != nil {
		t.Fatalf("parent should not halt on failed subcall: %v", err)
	}
	if ev.StateDB.GetState(callee, common.Hash{}) != (common.Hash{}) {
		t.Fatal("subcall storage was not rolled back")
	}
}

func TestLogEmission(t *testing.T) {
	ev, _ := testEVM(t)
	addr := common.Address{0x30}
	// PUSH 0x1234 as data topic and emit LOG1 from memory.
	code := []byte{
		0x60, 0x42, 0x60, 0x00, 0x52, // store 0x42 at mem 0
		0x60, 0xaa, // topic
		0x60, 0x20, // length
		0x60, 0x00, // offset (top)
		0xa1, // LOG1
		0x00,
	}
	deployAt(t, ev, addr, code, nil)
	caller := common.Address{0x31}
	ev.StateDB.AddBalance(caller, big.NewInt(1e18))

	if _, _, err := ev.Call(caller, addr, nil, 200_000, new(big.Int)); err != nil {
		t.Fatal(err)
	}
	logs := ev.StateDB.Logs()
	if len(logs) != 1 {
		t.Fatalf("want 1 log, got %d", len(logs))
	}
	if len(logs[0].Topics) != 1 || logs[0].Topics[0] != common.BytesToHash([]byte{0xaa}) {
		t.Fatalf("bad topics: %x", logs[0].Topics)
	}
	if new(big.Int).SetBytes(logs[0].Data).Cmp(big.NewInt(0x42)) != 0 {
		t.Fatalf("bad log data: %x", logs[0].Data)
	}
}

func TestRandomBytecodeDoesNotPanic(t *testing.T) {
	rng := rand.New(rand.NewSource(0x5eed))
	for iter := 0; iter < 200; iter++ {
		ev, _ := testEVM(t)
		length := rng.Intn(80)
		code := make([]byte, length)
		rng.Read(code)
		// Bias toward valid opcodes and small pushes to keep executions bounded.
		for i := range code {
			if rng.Intn(2) == 0 {
				code[i] = byte(validOpcodes[rng.Intn(len(validOpcodes))])
			}
		}
		from := common.Address{0x40}
		ev.StateDB.AddBalance(from, new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil))
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("iteration %d panicked: %v", iter, r)
				}
			}()
			ev.Call(from, common.Address{0x41}, code, 50_000, new(big.Int))
		}()
	}
}

var validOpcodes = []OpCode{
	0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b,
	0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d,
	0x20,
	0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a,
	0x3b, 0x3c, 0x3d, 0x3e, 0x3f,
	0x40, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48,
	0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a, 0x5b,
	0x60, 0x61, 0x62,
	0x80, 0x81, 0x82,
	0x90, 0x91,
	0xa0, 0xa1, 0xa2,
	0xf3, 0xfd,
}

// TestAllClaimedOpcodesExecutable ensures every opcode listed in the static
// gas table is recognized (no claimed opcode silently treated as invalid).
func TestAllClaimedOpcodesRecognized(t *testing.T) {
	for op := 0; op < 256; op++ {
		o := OpCode(op)
		_, claimed := staticGasTable[o]
		info := opcodeInfo(o)
		if claimed && info.pops < 0 {
			t.Fatalf("opcode %s in gas table but missing stack spec", o)
		}
	}
}
