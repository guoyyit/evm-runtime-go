package evm

import "math/big"

// Interpreter executes contract bytecode for one EVM instance. A fresh
// interpreter is created per call frame.
type Interpreter struct {
	evm   *EVM
	frame *Frame
}

func newInterpreter(e *EVM) *Interpreter {
	return &Interpreter{evm: e}
}

// Run executes c.Code and returns the return data. On REVERT the returned
// error is ErrExecutionReverted with the revert payload as data; on any other
// failure the contract's remaining gas is consumed by the caller per EVM
// rules (the caller decides whether to propagate or catch the failure).
func (in *Interpreter) Run(c *Contract, readOnly bool) (ret []byte, err error) {
	// Empty code (e.g. a plain-value transfer to an EOA) succeeds immediately.
	if len(c.Code) == 0 {
		return nil, nil
	}

	in.frame = newFrame(c, readOnly)
	f := in.frame
	in.evm.activeFrames = append(in.evm.activeFrames, f)
	defer func() {
		in.evm.activeFrames = in.evm.activeFrames[:len(in.evm.activeFrames)-1]
	}()

	for {
		if f.pc >= uint64(len(c.Code)) {
			return nil, nil // implicit STOP at code end
		}
		op := OpCode(c.Code[f.pc])

		// Invalid opcodes are rejected before any gas accounting beyond the
		// static cost; INVALID deliberately burns all remaining gas.
		if op == INVALID {
			c.Gas = 0
			return nil, ErrInvalidOpCode
		}
		if !isValid(op) {
			c.Gas = 0
			return nil, ErrInvalidOpCode
		}

		switch {
		case isPush(op):
			if !useGas(&c.Gas, gasQuickStep) {
				return nil, ErrOutOfGas
			}
			if err := in.execPush(op); err != nil {
				return nil, err
			}
		case isDup(op):
			if !useGas(&c.Gas, gasQuickStep) {
				return nil, ErrOutOfGas
			}
			if err := f.stack.Dup(dupIndex(op)); err != nil {
				return nil, err
			}
			f.pc++
		case isSwap(op):
			if !useGas(&c.Gas, gasQuickStep) {
				return nil, ErrOutOfGas
			}
			if err := f.stack.Swap(swapIndex(op)); err != nil {
				return nil, err
			}
			f.pc++
		default:
			var halt bool
			total, ok := in.gasFor(op)
			if !ok {
				c.Gas = 0
				return nil, ErrOutOfGas
			}
			useGas(&c.Gas, total)
			in.growFor(op)
			halt, err = in.exec(op)
			if err != nil {
				return nil, err
			}
			if halt {
				switch op {
				case RETURN, REVERT:
					ret = f.pendingReturn
				}
				if op == REVERT {
					return ret, ErrExecutionReverted
				}
				return ret, nil
			}
			if op != JUMP && op != JUMPI && !halt {
				f.pc++
			}
		}
	}
}

// execPush reads the immediate data of a PUSH instruction and pushes it.
func (in *Interpreter) execPush(op OpCode) error {
	f := in.frame
	c := f.contract
	n := pushSize(op)
	start := f.pc + 1
	data := make([]byte, n)
	end := start + uint64(n)
	if end > uint64(len(c.Code)) {
		copy(data, c.Code[start:]) // missing bytes read as zero
	} else {
		copy(data, c.Code[start:end])
	}
	if err := f.stack.Push(new(big.Int).SetBytes(data)); err != nil {
		return err
	}
	f.pc = end
	return nil
}

// popN pops exactly n values; args[0] is the former stack top, args[n-1]
// the deepest popped value, matching Peek depth ordering.
func (in *Interpreter) popN(n int) ([]*big.Int, error) {
	f := in.frame
	out := make([]*big.Int, n)
	for i := 0; i < n; i++ {
		v, err := f.stack.Pop()
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func (in *Interpreter) push(v *big.Int) error {
	return in.frame.stack.Push(trim256(v))
}
