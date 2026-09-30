package evm

import (
	"math/big"

	"github.com/guoyyit/evm-runtime-go/common"
)

func (in *Interpreter) opFlow(contract *Contract, op OpCode, pc int) (nextPC int, err error) {
	st := in.stack
	mem := in.memory
	switch op {
	case POP:
		st.pop()
	case MLOAD:
		offset := st.pop().Uint64()
		st.push(new(big.Int).SetBytes(mem.getCopy(int64(offset), 32)))
	case MSTORE:
		offset := st.pop().Uint64()
		value := st.pop()
		mem.set32(offset, common.NewU256(value).Bytes32())
	case MSTORE8:
		offset := st.pop().Uint64()
		value := st.pop()
		b := byte(value.Uint64())
		mem.set(offset, 1, []byte{b})
	case JUMP:
		dest := st.pop().Uint64()
		if !contract.validJumpdest(dest) {
			return pc, ErrInvalidJump
		}
		return int(dest), nil
	case JUMPI:
		dest, condition := st.pop(), st.pop()
		if condition.Sign() != 0 {
			if !contract.validJumpdest(dest.Uint64()) {
				return pc, ErrInvalidJump
			}
			return int(dest.Uint64()), nil
		}
	case PC:
		st.push(new(big.Int).SetUint64(uint64(pc)))
	case MSIZE:
		st.push(new(big.Int).SetUint64(uint64(mem.len())))
	case GAS:
		st.push(new(big.Int).SetUint64(contract.Gas))
	case JUMPDEST:
		// no-op
	}
	return pc + 1, nil
}
