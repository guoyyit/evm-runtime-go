package evm

import (
	"math/big"

	"github.com/guoyyit/evm-runtime-go/common"
)

// memoryExpansionNeeded returns the required memory size (bytes) for the
// current opcode based on the values still on the stack (not yet popped).
func (in *Interpreter) requiredMemory(op OpCode) (uint64, bool) {
	st := in.stack
	end := func(off, size uint64) (uint64, bool) {
		if off > ^uint64(0)-size {
			panic(ErrGasUintOverflow)
		}
		return off + size, true
	}
	switch op {
	case MLOAD:
		return end(st.peek(1).Uint64(), 32)
	case MSTORE:
		return end(st.peek(1).Uint64(), 32)
	case MSTORE8:
		return end(st.peek(1).Uint64(), 1)
	case KECCAK256, RETURN, REVERT, LOG0, LOG1, LOG2, LOG3, LOG4:
		return end(st.peek(1).Uint64(), st.peek(2).Uint64())
	case CALLDATACOPY, CODECOPY, RETURNDATACOPY:
		return end(st.peek(1).Uint64(), st.peek(3).Uint64())
	case EXTCODECOPY:
		return end(st.peek(2).Uint64(), st.peek(4).Uint64())
	case CREATE, CREATE2:
		return end(st.peek(2).Uint64(), st.peek(3).Uint64())
	case CALL, CALLCODE:
		argsEnd, _ := end(st.peek(4).Uint64(), st.peek(5).Uint64())
		retEnd, _ := end(st.peek(6).Uint64(), st.peek(7).Uint64())
		if retEnd > argsEnd {
			return retEnd, true
		}
		return argsEnd, true
	case DELEGATECALL, STATICCALL:
		argsEnd, _ := end(st.peek(3).Uint64(), st.peek(4).Uint64())
		retEnd, _ := end(st.peek(5).Uint64(), st.peek(6).Uint64())
		if retEnd > argsEnd {
			return retEnd, true
		}
		return argsEnd, true
	}
	return 0, false
}

// chargeMemory charges memory expansion plus per-opcode dynamic gas that must
// be known before the operation runs.
func (in *Interpreter) chargeMemory(contract *Contract, op OpCode) error {
	newSize, expands := in.requiredMemory(op)
	if expands {
		needed, err := memoryGasCost(newSize)
		if err != nil {
			return err
		}
		if needed > in.memory.lastGasCost {
			if err := in.useGas(contract, needed-in.memory.lastGasCost); err != nil {
				return err
			}
			in.memory.lastGasCost = needed
		}
		in.memory.resize(newSize)
	}
	return in.chargeDynamic(contract, op)
}

func (in *Interpreter) chargeDynamic(contract *Contract, op OpCode) error {
	db := in.evm.StateDB
	st := in.stack
	switch op {
	case KECCAK256:
		return in.useGas(contract, GasKeccakWord*toWordSize(st.peek(2).Uint64()))
	case EXP:
		return in.useGas(contract, GasExpByte*uint64(len(trimLeadingZeroBytes(st.peek(1).Bytes()))))
	case CALLDATACOPY, CODECOPY, RETURNDATACOPY:
		var length uint64
		switch op {
		case CALLDATACOPY, CODECOPY:
			length = st.peek(3).Uint64()
		case RETURNDATACOPY:
			length = st.peek(3).Uint64()
		}
		return in.useGas(contract, GasCopy*toWordSize(length))
	case EXTCODECOPY:
		if err := in.chargeAccountAccess(contract, common.AddressFromBig(st.peek(1))); err != nil {
			return err
		}
		return in.useGas(contract, GasCopy*toWordSize(st.peek(4).Uint64()))
	case BALANCE, EXTCODESIZE, EXTCODEHASH:
		return in.chargeAccountAccess(contract, common.AddressFromBig(st.peek(1)))
	case SLOAD:
		addr := contract.Address()
		key := common.BytesToHash(st.peek(1).Bytes())
		if db.IsSlotWarm(addr, key) {
			return in.useGas(contract, GasSLoadEIP2929)
		}
		db.AddSlotToAccessList(addr, key)
		db.AddAddressToAccessList(addr)
		return in.useGas(contract, GasColdAccountAccess)
	case SSTORE:
		return in.chargeSStore(contract)
	case LOG0, LOG1, LOG2, LOG3, LOG4:
		topics := uint64(op - LOG0)
		length := st.peek(2).Uint64()
		cost := GasLogTopic*topics + GasLogData*length
		return in.useGas(contract, cost)
	case CREATE2:
		return in.useGas(contract, GasKeccakWord*toWordSize(st.peek(3).Uint64()))
	case SELFDESTRUCT:
		addr := common.AddressFromBig(st.peek(1))
		if !db.IsAddressWarm(addr) {
			db.AddAddressToAccessList(addr)
			if err := in.useGas(contract, GasColdAccountAccess); err != nil {
				return err
			}
		}
		if db.GetBalance(contract.Address()).Sign() != 0 && !db.Exist(addr) {
			if err := in.useGas(contract, GasNewAccount); err != nil {
				return err
			}
		}
	}
	return nil
}

func (in *Interpreter) chargeAccountAccess(contract *Contract, addr common.Address) error {
	if in.evm.StateDB.IsAddressWarm(addr) {
		return in.useGas(contract, GasWarmAccess)
	}
	in.evm.StateDB.AddAddressToAccessList(addr)
	return in.useGas(contract, GasColdAccountAccess)
}

// chargeSStore implements the EIP-2200/EIP-3529 dynamic storage schedule.
func (in *Interpreter) chargeSStore(contract *Contract) error {
	db := in.evm.StateDB
	addr := contract.Address()
	key := common.BytesToHash(in.stack.peek(1).Bytes())
	original := db.GetCommittedState(addr, key)
	current := db.GetState(addr, key)
	value := common.BytesToHash(in.stack.peek(2).Bytes())

	if !db.IsSlotWarm(addr, key) {
		db.AddSlotToAccessList(addr, key)
		db.AddAddressToAccessList(addr)
		if err := in.useGas(contract, GasColdAccountAccess); err != nil {
			return err
		}
	}

	var cost uint64
	switch {
	case current == value:
		cost = GasWarmAccess
	case original == current:
		if original == (common.Hash{}) {
			cost = GasSStoreSet
			// No refund up front; clearing is realized if it later goes to zero.
		} else {
			cost = GasSStoreReset
			if value == (common.Hash{}) {
				// EIP-3529: clearing a non-zero slot refunds 4800.
				in.evm.addRefund(GasSStoreClear)
			}
		}
	default:
		cost = GasWarmAccess
		// A dirty change converging back to its original value receives a
		// refund equal to the earlier up-front charge.
		if value == original {
			if original == (common.Hash{}) {
				in.evm.addRefund(GasSStoreSet - GasWarmAccess)
			} else {
				in.evm.addRefund(GasSStoreReset - GasWarmAccess)
			}
		}
	}
	return in.useGas(contract, cost)
}

var _ = big.NewInt
