package evm

import (
	"math/big"

	"github.com/guoyyit/evm-runtime-go/common"
)

func (in *Interpreter) opCall(contract *Contract, op OpCode) error {
	st := in.stack
	mem := in.memory
	evmCtx := in.evm

	switch op {
	case CALL, CALLCODE:
		gas := st.pop().Uint64()
		to := common.AddressFromBig(st.pop())
		value := st.pop()
		inOffset, inSize := st.pop().Uint64(), st.pop().Uint64()
		retOffset, retSize := st.pop().Uint64(), st.pop().Uint64()

		if op == CALL && in.readOnly && value.Sign() != 0 {
			return ErrWriteInReadOnly
		}

		if err := in.chargeCallGas(contract, op, to, value, &gas); err != nil {
			return err
		}

		input := mem.getCopy(int64(inOffset), int64(inSize))

		var (
			output  []byte
			leftGas uint64
			callErr error
		)
		if value.Sign() != 0 {
			gas += GasCallStipend
		}
		if op == CALL {
			output, leftGas, callErr = evmCtx.Call(contract.Address(), to, input, gas, value)
		} else {
			output, leftGas, callErr = evmCtx.CallCode(contract.Address(), to, input, gas, value)
		}
		in.returnData = output
		in.giveBack(contract, leftGas)

		copyReturnData(mem, retOffset, retSize, output)
		st.push(callSuccess(callErr))

	case DELEGATECALL, STATICCALL:
		gas := st.pop().Uint64()
		to := common.AddressFromBig(st.pop())
		inOffset, inSize := st.pop().Uint64(), st.pop().Uint64()
		retOffset, retSize := st.pop().Uint64(), st.pop().Uint64()

		if err := in.chargeCallGas(contract, op, to, new(big.Int), &gas); err != nil {
			return err
		}

		input := mem.getCopy(int64(inOffset), int64(inSize))
		var output []byte
		var leftGas uint64
		var callErr error
		if op == DELEGATECALL {
			output, leftGas, callErr = evmCtx.DelegateCall(contract, to, input, gas, in.readOnly)
		} else {
			output, leftGas, callErr = evmCtx.StaticCall(contract.Address(), to, input, gas)
		}
		in.returnData = output
		in.giveBack(contract, leftGas)

		copyReturnData(mem, retOffset, retSize, output)
		st.push(callSuccess(callErr))

	case CREATE, CREATE2:
		value := st.pop()
		offset, size := st.pop().Uint64(), st.pop().Uint64()
		var salt *big.Int
		if op == CREATE2 {
			salt = st.pop()
		}
		gas := in.callCreateGas(contract)
		initCode := mem.getCopy(int64(offset), int64(size))

		var (
			addr      common.Address
			leftGas   uint64
			createErr error
		)
		if op == CREATE {
			_, addr, leftGas, createErr = evmCtx.Create(contract.Address(), initCode, gas, value)
		} else {
			_, addr, leftGas, createErr = evmCtx.Create2(contract.Address(), initCode, gas, value, salt)
		}
		in.returnData = nil
		in.giveBack(contract, leftGas)
		if createErr != nil {
			st.push(new(big.Int))
		} else {
			st.push(new(big.Int).SetBytes(addr.Bytes()))
		}
	}
	return nil
}

func callSuccess(err error) *big.Int {
	if err != nil {
		return new(big.Int)
	}
	return big.NewInt(1)
}

func copyReturnData(mem *Memory, offset, size uint64, data []byte) {
	copyLen := size
	if uint64(len(data)) < copyLen {
		copyLen = uint64(len(data))
	}
	buf := make([]byte, size)
	copy(buf, data[:copyLen])
	mem.set(offset, size, buf)
}

func (in *Interpreter) giveBack(contract *Contract, leftGas uint64) {
	contract.Gas += leftGas
}

// chargeCallGas deducts the fixed call overhead and reserves the 63/64 cap.
// The actual gas forwarded is written back through gasPtr after the stipend.
func (in *Interpreter) chargeCallGas(contract *Contract, op OpCode, to common.Address, value *big.Int, gasPtr *uint64) error {
	db := in.evm.StateDB
	intrinsic := GasCall
	if value.Sign() != 0 {
		intrinsic += GasCallValue
		if op == CALL && !db.Exist(to) {
			intrinsic += GasNewAccount
		}
	}
	if !db.IsAddressWarm(to) {
		intrinsic += GasColdAccountAccess - GasWarmAccess
		db.AddAddressToAccessList(to)
	}
	if err := in.useGas(contract, intrinsic); err != nil {
		return err
	}

	requested := *gasPtr
	available := contract.Gas
	cap := available - available/64
	if requested > cap {
		requested = cap
	}
	if err := in.useGas(contract, requested); err != nil {
		return err
	}
	*gasPtr = requested
	return nil
}

func (in *Interpreter) callCreateGas(contract *Contract) uint64 {
	available := contract.Gas
	forwarded := available - available/64
	contract.Gas -= forwarded
	return forwarded
}
