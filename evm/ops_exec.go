package evm

import (
	"math/big"

	"github.com/guoyyit/evm-runtime-go/crypto"
	"github.com/guoyyit/evm-runtime-go/state"
)

// exec dispatches one non-PUSH/DUP/SWAP opcode after dynamic gas has been
// charged. It returns halt=true for STOP/RETURN/REVERT/SELFDESTRUCT.
func (in *Interpreter) exec(op OpCode) (bool, error) {
	c := in.frame.contract

	switch {
	case op <= SIGNEXTEND || (LT <= op && op <= SAR):
		return in.opArithmetic(op)
	case op == SHA3:
		return in.opSHA3()
	case ADDRESS <= op && op <= RETURNDATACOPY:
		return in.opContext(op)
	case BLOCKHASH <= op && op <= BASEFEE:
		return in.opBlock(op)
	case op == POP:
		if _, err := in.popN(1); err != nil {
			return false, err
		}
		return false, nil
	case op == MLOAD:
		return in.opMLoad()
	case op == MSTORE:
		return in.opMStore()
	case op == MSTORE8:
		return in.opMStore8()
	case op == SLOAD:
		return in.opSLoad()
	case op == SSTORE:
		return in.opSStore()
	case op == JUMP:
		return in.opJump()
	case op == JUMPI:
		return in.opJumpI()
	case op == PC:
		return false, in.push(big.NewInt(int64(in.frame.pc)))
	case op == MSIZE:
		return false, in.push(big.NewInt(int64(in.frame.memory.Len())))
	case op == GAS:
		return false, in.push(big.NewInt(int64(c.Gas + 1)))
	case op == JUMPDEST:
		return false, nil
	case op == PUSH0:
		return false, in.push(new(big.Int))
	case isLog(op):
		return in.opLog(op)
	case op == CREATE:
		return in.opCreate(false)
	case op == CREATE2:
		return in.opCreate(true)
	case op == CALL, op == CALLCODE:
		return in.opCall(op)
	case op == DELEGATECALL:
		return in.opDelegateCall()
	case op == STATICCALL:
		return in.opStaticCall()
	case op == RETURN:
		return in.opReturn(false)
	case op == REVERT:
		return in.opReturn(true)
	case op == SELFDESTRUCT:
		return in.opSelfDestruct()
	}
	return false, ErrInvalidOpCode
}

func (in *Interpreter) opMLoad() (bool, error) {
	off, _, ok := in.peekUint(0)
	if !ok {
		return false, ErrOutOfGas
	}
	args, err := in.popN(1)
	_ = args
	if err != nil {
		return false, err
	}
	if !requireMemory(in.frame.memory, &in.frame.contract.Gas, off, 32) {
		return false, ErrOutOfGas
	}
	data := in.frame.memory.Get(off, 32)
	return false, in.push(new(big.Int).SetBytes(data))
}

func (in *Interpreter) opMStore() (bool, error) {
	args, err := in.popN(2)
	if err != nil {
		return false, err
	}
	off := uint256toUint(args[0])
	if args[0].BitLen() > 64 {
		return false, ErrOutOfGas
	}
	if !requireMemory(in.frame.memory, &in.frame.contract.Gas, off, 32) {
		return false, ErrOutOfGas
	}
	var buf [32]byte
	args[1].FillBytes(buf[:])
	in.frame.memory.Set(off, buf[:])
	return false, nil
}

func (in *Interpreter) opMStore8() (bool, error) {
	args, err := in.popN(2)
	if err != nil {
		return false, err
	}
	if args[0].BitLen() > 64 {
		return false, ErrOutOfGas
	}
	off := uint256toUint(args[0])
	if !requireMemory(in.frame.memory, &in.frame.contract.Gas, off, 1) {
		return false, ErrOutOfGas
	}
	in.frame.memory.Set8(off, byte(args[1].Uint64()))
	return false, nil
}

func uint256toUint(v *big.Int) uint64 {
	if v.BitLen() > 64 {
		return 1 << 63
	}
	return v.Uint64()
}

func (in *Interpreter) opSLoad() (bool, error) {
	args, err := in.popN(1)
	if err != nil {
		return false, err
	}
	key := bigToHash(trim256(args[0]))
	self := in.frame.contract.Self
	// Warm/cold charge already taken in gasFor; mark the slot warm now.
	in.evm.warmSlot[slotKey{self, key}] = struct{}{}
	v := in.evm.State.GetState(self, key)
	return false, in.push(hashToBig(v))
}

func (in *Interpreter) opSStore() (bool, error) {
	if in.frame.readOnly {
		return false, ErrWriteInReadOnly
	}
	args, err := in.popN(2)
	if err != nil {
		return false, err
	}
	key := bigToHash(trim256(args[0]))
	value := bigToHash(trim256(args[1]))
	self := in.frame.contract.Self
	in.evm.warmSlot[slotKey{self, key}] = struct{}{}
	in.evm.State.SetState(self, key, value)
	if in.frame.pendingRefund != 0 {
		in.evm.refund += in.frame.pendingRefund
	}
	return false, nil
}

func (in *Interpreter) opJump() (bool, error) {
	args, err := in.popN(1)
	if err != nil {
		return false, err
	}
	dest := args[0].Uint64()
	if args[0].BitLen() > 64 || !in.frame.jumpdests[dest] {
		return false, ErrInvalidJump
	}
	in.frame.pc = dest
	return false, nil
}

func (in *Interpreter) opJumpI() (bool, error) {
	args, err := in.popN(2)
	if err != nil {
		return false, err
	}
	if args[1].Sign() == 0 {
		in.frame.pc++
		return false, nil
	}
	dest := args[0].Uint64()
	if args[0].BitLen() > 64 || !in.frame.jumpdests[dest] {
		return false, ErrInvalidJump
	}
	in.frame.pc = dest
	return false, nil
}

func (in *Interpreter) opSHA3() (bool, error) {
	args, err := in.popN(2)
	if err != nil {
		return false, err
	}
	if args[0].BitLen() > 64 || args[1].BitLen() > 64 {
		return false, ErrOutOfGas
	}
	off, size := args[0].Uint64(), args[1].Uint64()
	if !requireMemory(in.frame.memory, &in.frame.contract.Gas, off, size) {
		return false, ErrOutOfGas
	}
	data := in.frame.memory.Get(off, size)
	sum := crypto.Keccak256(data)
	return false, in.push(new(big.Int).SetBytes(sum))
}

func (in *Interpreter) opReturn(revert bool) (bool, error) {
	args, err := in.popN(2)
	if err != nil {
		return false, err
	}
	if args[1].BitLen() > 64 || args[0].BitLen() > 64 {
		return false, ErrOutOfGas
	}
	off, size := args[0].Uint64(), args[1].Uint64()
	if !requireMemory(in.frame.memory, &in.frame.contract.Gas, off, size) {
		return false, ErrOutOfGas
	}
	in.frame.pendingReturn = in.frame.memory.Get(off, size)
	return true, nil
}

func (in *Interpreter) opLog(op OpCode) (bool, error) {
	if in.frame.readOnly {
		return false, ErrWriteInReadOnly
	}
	n := logTopics(op)
	args, err := in.popN(2 + n)
	if err != nil {
		return false, err
	}
	if args[0].BitLen() > 64 || args[1].BitLen() > 64 {
		return false, ErrOutOfGas
	}
	off, size := args[0].Uint64(), args[1].Uint64()
	if !requireMemory(in.frame.memory, &in.frame.contract.Gas, off, size) {
		return false, ErrOutOfGas
	}
	data := in.frame.memory.Get(off, size)
	topics := make([]state.Hash, n)
	for i := 0; i < n; i++ {
		topics[i] = bigToHash(trim256(args[2+i]))
	}
	in.evm.logs = append(in.evm.logs, state.Log{
		Address: in.frame.contract.Self,
		Topics:  topics,
		Data:    data,
	})
	return false, nil
}

func (in *Interpreter) opSelfDestruct() (bool, error) {
	if in.frame.readOnly {
		return false, ErrWriteInReadOnly
	}
	args, err := in.popN(1)
	if err != nil {
		return false, err
	}
	beneficiary := bigToAddress(args[0])
	self := in.frame.contract.Self

	if !in.evm.touchAccount(beneficiary, &in.frame.contract.Gas) {
		return false, ErrOutOfGas
	}

	balance := in.evm.State.GetBalance(self)
	if balance.Sign() != 0 {
		// Transferring to a brand-new account carries the new-account cost.
		if !in.evm.State.Exists(beneficiary) {
			if !useGas(&in.frame.contract.Gas, gasNewAccount) {
				return false, ErrOutOfGas
			}
		}
		in.evm.State.SubBalance(self, balance)
		in.evm.State.AddBalance(beneficiary, balance)
	}
	in.evm.State.MarkSelfDestruct(self)
	return true, nil
}
