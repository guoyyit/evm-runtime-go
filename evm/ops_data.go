package evm

import (
	"math/big"

	"github.com/guoyyit/evm-runtime-go/state"
)

// gasFor computes the full (static + dynamic) gas of op before execution,
// consulting the stack top for operand-derived costs.
func (in *Interpreter) gasFor(op OpCode) (uint64, bool) {
	c := in.frame.contract

	// 1. static part
	static := staticGas(op)

	// 2. operand-derived additions
	var extra uint64
	switch op {
	case EXP:
		base, err := in.frame.stack.Peek(1)
		if err != nil {
			return 0, false
		}
		extra = gasExpByte * uint64((base.BitLen()+7)/8)

	case SHA3:
		off, size, ok := in.memOperands()
		if !ok {
			return 0, false
		}
		extra = gasSha3Word * toWordSize(size)
		memCost, ok2 := expansionCost(in.frame.memory.Len(), off, size)
		if !ok2 {
			return 0, false
		}
		extra += memCost

	case MLOAD, MSTORE:
		off, _, ok := in.peekUint(0)
		if !ok {
			return 0, false
		}
		memCost, ok2 := expansionCost(in.frame.memory.Len(), off, 32)
		if !ok2 {
			return 0, false
		}
		extra = memCost
	case MSTORE8:
		off, _, ok := in.peekUint(0)
		if !ok {
			return 0, false
		}
		memCost, ok2 := expansionCost(in.frame.memory.Len(), off, 1)
		if !ok2 {
			return 0, false
		}
		extra = memCost

	case CALLDATACOPY, CODECOPY, RETURNDATACOPY:
		_, size, ok := in.memOperands()
		if !ok {
			return 0, false
		}
		extra = gasCopy * toWordSize(size)
		memCost, ok2 := expansionCostFromTop(in, 1, 2)
		if !ok2 {
			return 0, false
		}
		extra += memCost
	case EXTCODECOPY:
		_, size, ok := in.memOperandsFrom(1)
		if !ok {
			return 0, false
		}
		extra = gasCopy * toWordSize(size)

	case LOG0, LOG1, LOG2, LOG3, LOG4:
		_, size, ok := in.memOperands()
		if !ok {
			return 0, false
		}
		topics := logTopics(op)
		extra = gasLog + gasLogTopic*uint64(topics) + gasLogData*size
		memCost, ok2 := expansionCostFromTop(in, 0, 1)
		if !ok2 {
			return 0, false
		}
		extra += memCost

	case SLOAD:
		addr, key, ok := in.peekStorage(0)
		if !ok {
			return 0, false
		}
		if in.evm.isWarmSlot(addr, key) {
			static = gasWarmAccess
		} else {
			static = gasSload
		}

	case SSTORE:
		addr, key, ok := in.peekStorage(0)
		if !ok {
			return 0, false
		}
		val, _, ok2 := in.peekHash(1)
		if !ok2 {
			return 0, false
		}
		warm := in.evm.isWarmSlot(addr, key)
		charge, refund, ok3 := sstoreGas(in.evm.State, in.evm, addr, key, val, warm)
		if !ok3 {
			return 0, false
		}
		static = charge
		in.frame.pendingRefund = refund

	case BALANCE, SELFBALANCE:
		// handled in exec: SELFBALANCE is static; BALANCE needs cold/warm.
	case CREATE:
		static = gasCreate
	case CREATE2:
		static = gasCreate2
		_, size, ok := in.peekUint(1) // size is second from top (salt top)
		if !ok {
			return 0, false
		}
		extra = gasSha3Word * toWordSize(size)

	case CALL, CALLCODE, STATICCALL, DELEGATECALL:
		return in.callGas(op)
	}

	total := static + extra
	if total > c.Gas {
		return 0, false
	}
	return total, true
}

// growFor performs the memory expansion already priced by gasFor.
func (in *Interpreter) growFor(op OpCode) {
	m := in.frame.memory
	grow := func(offDepth, sizeDepth int) {
		off, _, ok := in.peekUint(offDepth)
		if !ok {
			return
		}
		size, _, ok := in.peekUint(sizeDepth)
		if !ok || size == 0 {
			return
		}
		m.Resize(toWordSize(off+size) * 32)
	}
	switch op {
	case MLOAD, MSTORE:
		if off, _, ok := in.peekUint(0); ok {
			m.Resize(toWordSize(off+32) * 32)
		}
	case MSTORE8:
		if off, _, ok := in.peekUint(0); ok {
			m.Resize(off + 1)
		}
	case SHA3, RETURN, REVERT, LOG0, LOG1, LOG2, LOG3, LOG4:
		grow(0, 1)
	case CALLDATACOPY, CODECOPY, RETURNDATACOPY:
		grow(1, 2)
	case EXTCODECOPY:
		grow(2, 3)
	case CALL, CALLCODE:
		grow(3, 4)
		grow(5, 6)
	case STATICCALL, DELEGATECALL:
		grow(2, 3)
		grow(4, 5)
	case CREATE:
		grow(0, 1)
	case CREATE2:
		grow(2, 1)
	}
}

// peekUint returns the stack element at depth (0 = top) as uint64 and whether
// it fits; values >= 2^64 are reported as not-fitting for memory indexing.
func (in *Interpreter) peekUint(depth int) (uint64, uint64, bool) {
	v, err := in.frame.stack.Peek(depth)
	if err != nil {
		return 0, 0, false
	}
	if v.BitLen() > 64 {
		return 0, 0, false
	}
	return v.Uint64(), 0, true
}

// memOperands returns (offset, size) from the top two stack elements.
func (in *Interpreter) memOperands() (uint64, uint64, bool) {
	off, _, ok := in.peekUint(0)
	if !ok {
		return 0, 0, false
	}
	size, _, ok := in.peekUint(1)
	if !ok {
		return 0, 0, false
	}
	return off, size, true
}

// memOperandsFrom reads offset/size at arbitrary stack depths.
func (in *Interpreter) memOperandsFrom(offDepth int) (uint64, uint64, bool) {
	off, _, ok := in.peekUint(offDepth)
	if !ok {
		return 0, 0, false
	}
	size, _, ok := in.peekUint(offDepth + 1)
	if !ok {
		return 0, 0, false
	}
	return off, size, true
}

// expansionCostFromTop treats stack[offDepth], stack[sizeDepth] as a memory
// region and returns its expansion gas.
func expansionCostFromTop(in *Interpreter, offDepth, sizeDepth int) (uint64, bool) {
	off, _, ok := in.peekUint(offDepth)
	if !ok {
		return 0, false
	}
	size, _, ok := in.peekUint(sizeDepth)
	if !ok {
		return 0, false
	}
	cost, ok := expansionCost(in.frame.memory.Len(), off, size)
	return cost, ok
}

func (in *Interpreter) peekHash(depth int) (state.Hash, *big.Int, bool) {
	v, err := in.frame.stack.Peek(depth)
	if err != nil {
		return state.Hash{}, nil, false
	}
	return bigToHash(trim256(v)), v, true
}

func (in *Interpreter) peekStorage(keyDepth int) (state.Address, state.Hash, bool) {
	h, _, ok := in.peekHash(keyDepth)
	return in.frame.contract.Self, h, ok
}

func bigToHash(v *big.Int) state.Hash {
	var h state.Hash
	b := v.Bytes()
	copy(h[32-len(b):], b)
	return h
}

func hashToBig(h state.Hash) *big.Int {
	return new(big.Int).SetBytes(h[:])
}

func bigToAddress(v *big.Int) state.Address {
	var a state.Address
	b := trim256(v).Bytes()
	copy(a[20-len(b):], b)
	return a
}

// sstoreGas implements the EIP-2200/3529 pricing for SSTORE:
// current value vs new value vs original value determine the charge and the
// refund queued for the end of execution.
func sstoreGas(st *state.State, e *EVM, addr state.Address, key state.Hash, value state.Hash, warm bool) (uint64, int64, bool) {
	current := st.GetState(addr, key)
	original := e.original(addr, key)
	switch {
	case current == value:
		charge := gasSstoreNoop
		if !warm {
			charge += gasSload
		}
		return charge, 0, true
	case original == current:
		if original == (state.Hash{}) {
			charge := gasSstoreSet
			if !warm {
				charge += gasSload
			}
			return charge, 0, true
		}
		charge := gasSstoreReset
		if !warm {
			charge += gasSload
		}
		var refund int64
		if value == (state.Hash{}) {
			refund = int64(gasSstoreClearRefund)
		}
		return charge, refund, true
	default:
		charge := gasSstoreNoop
		if !warm {
			charge += gasSload
		}
		var refund int64
		if original != (state.Hash{}) {
			if current == (state.Hash{}) {
				refund -= int64(gasSstoreClearRefund)
			} else if value == (state.Hash{}) {
				refund += int64(gasSstoreClearRefund)
			}
		}
		if original == value {
			if original == (state.Hash{}) {
				refund += int64(gasSstoreSet - gasSstoreNoop)
			} else {
				refund += int64(gasSstoreReset - gasSstoreNoop)
			}
		}
		return charge, refund, true
	}
}
