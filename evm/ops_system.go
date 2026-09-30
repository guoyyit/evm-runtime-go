package evm

import (
	"math/big"

	"github.com/guoyyit/evm-runtime-go/common"
	"github.com/guoyyit/evm-runtime-go/state"
)

func (in *Interpreter) opSystem(contract *Contract, op OpCode) (halted bool, output []byte, err error) {
	st := in.stack
	mem := in.memory
	db := in.evm.StateDB

	switch op {
	case KECCAK256:
		offset, length := st.pop().Uint64(), st.pop().Uint64()
		data := mem.getCopy(int64(offset), int64(length))
		st.push(new(big.Int).SetBytes(common.Keccak256(data)))

	case SLOAD:
		key := common.BytesToHash(st.pop().Bytes())
		val := db.GetState(contract.Address(), key)
		st.push(val.Big())
	case SSTORE:
		if in.readOnly {
			return false, nil, ErrWriteInReadOnly
		}
		key := common.BytesToHash(st.pop().Bytes())
		value := common.BytesToHash(st.pop().Bytes())
		db.SetState(contract.Address(), key, value)

	case LOG0, LOG1, LOG2, LOG3, LOG4:
		if in.readOnly {
			return false, nil, ErrWriteInReadOnly
		}
		topicCount := int(op - LOG0)
		offset, length := st.back(0).Uint64(), st.back(1).Uint64()
		topics := make([]common.Hash, topicCount)
		for i := 0; i < topicCount; i++ {
			topics[i] = common.BytesToHash(st.back(2 + i).Bytes())
		}
		// Stack order (top to bottom): offset, length, topic0..topicN.
		st.pop() // offset
		st.pop() // length
		for i := 0; i < topicCount; i++ {
			st.pop()
		}
		data := append([]byte(nil), mem.getCopy(int64(offset), int64(length))...)
		db.AddLog(state.Log{
			Address: contract.Address(),
			Topics:  topics,
			Data:    data,
		})

	case RETURN:
		offset, length := st.pop().Uint64(), st.pop().Uint64()
		return true, append([]byte(nil), mem.getCopy(int64(offset), int64(length))...), nil
	case REVERT:
		offset, length := st.pop().Uint64(), st.pop().Uint64()
		data := append([]byte(nil), mem.getCopy(int64(offset), int64(length))...)
		return true, data, &revertError{data: data}

	case SELFDESTRUCT:
		if in.readOnly {
			return false, nil, ErrWriteInReadOnly
		}
		beneficiary := common.AddressFromBig(st.pop())
		balance := db.GetBalance(contract.Address())
		if balance.Sign() != 0 {
			if !db.Exist(beneficiary) {
				db.CreateAccount(beneficiary)
			}
			db.AddBalance(beneficiary, balance)
			db.SubBalance(contract.Address(), balance)
		}
		db.Suicide(contract.Address())
		return true, nil, nil
	case STOP:
		return true, nil, nil
	}
	return false, nil, nil
}

type revertError struct{ data []byte }

func (e *revertError) Error() string { return ErrExecutionReverted.Error() }
