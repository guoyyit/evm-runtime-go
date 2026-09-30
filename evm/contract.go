package evm

import (
	"math/big"

	"github.com/guoyyit/evm-runtime-go/state"
)

// Contract is the code/value context for one message call or creation.
type Contract struct {
	CodeAddress state.Address
	Caller      state.Address
	Self        state.Address
	Value       *big.Int
	Input       []byte
	Code        []byte
	Gas         uint64
}

// Frame is one executing call frame with its own stack, memory and pc.
type Frame struct {
	contract      *Contract
	stack         *Stack
	memory        *Memory
	pc            uint64
	lastReturn    []byte
	pendingReturn []byte
	pendingRefund int64
	callOverhead  uint64
	readOnly      bool
	jumpdests     map[uint64]bool
}

// frameSnapshot captures the full volatile state of a frame so execution can
// be rewound to an earlier point with stack, memory, pc and gas intact.
type frameSnapshot struct {
	stack      []*big.Int
	memory     []byte
	pc         uint64
	gas        uint64
	lastReturn []byte
}

func (f *Frame) snapshot() frameSnapshot {
	stackCp := make([]*big.Int, f.stack.Len())
	for i, v := range f.stack.items {
		stackCp[i] = new(big.Int).Set(v)
	}
	return frameSnapshot{
		stack:      stackCp,
		memory:     append([]byte(nil), f.memory.store...),
		pc:         f.pc,
		gas:        f.contract.Gas,
		lastReturn: append([]byte(nil), f.lastReturn...),
	}
}

func (f *Frame) restore(s frameSnapshot) {
	f.stack.items = make([]*big.Int, len(s.stack))
	for i, v := range s.stack {
		f.stack.items[i] = new(big.Int).Set(v)
	}
	f.memory.store = append([]byte(nil), s.memory...)
	f.pc = s.pc
	f.contract.Gas = s.gas
	f.lastReturn = append([]byte(nil), s.lastReturn...)
}

// newFrame builds a frame and precomputes its JUMPDEST bitmap, treating bytes
// inside PUSH data as non-destinations.
func newFrame(c *Contract, readOnly bool) *Frame {
	return &Frame{
		contract:   c,
		stack:      NewStack(),
		memory:     NewMemory(),
		readOnly:   readOnly,
		jumpdests:  findJumpDests(c.Code),
		lastReturn: nil,
	}
}

func findJumpDests(code []byte) map[uint64]bool {
	dests := make(map[uint64]bool)
	for i := 0; i < len(code); {
		op := OpCode(code[i])
		switch {
		case op == JUMPDEST:
			dests[uint64(i)] = true
			i++
		case isPush(op):
			i += pushSize(op) + 1
		default:
			i++
		}
	}
	return dests
}
