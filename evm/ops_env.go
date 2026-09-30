package evm

import (
	"math/big"

	"github.com/guoyyit/evm-runtime-go/common"
)

func (in *Interpreter) opEnv(contract *Contract, op OpCode) {
	st := in.stack
	mem := in.memory
	evmCtx := in.evm
	db := evmCtx.StateDB

	switch op {
	case ADDRESS:
		st.push(new(big.Int).SetBytes(contract.Address().Bytes()))
	case BALANCE:
		addr := common.AddressFromBig(st.pop())
		st.push(new(big.Int).Set(db.GetBalance(addr)))
	case ORIGIN:
		st.push(new(big.Int).SetBytes(evmCtx.Tx.Origin.Bytes()))
	case CALLER:
		st.push(new(big.Int).SetBytes(contract.CallerAddress.Bytes()))
	case CALLVALUE:
		st.push(new(big.Int).Set(contract.Value()))
	case GASPRICE:
		st.push(new(big.Int).Set(evmCtx.Tx.GasPrice))

	case CALLDATALOAD:
		offset := st.pop().Uint64()
		data := getData(contract.Input, int64(offset), 32)
		st.push(new(big.Int).SetBytes(data))
	case CALLDATASIZE:
		st.push(new(big.Int).SetInt64(int64(len(contract.Input))))
	case CALLDATACOPY:
		memOffset, dataOffset, length := st.pop().Uint64(), st.pop().Uint64(), st.pop().Uint64()
		data := make([]byte, length)
		copy(data, getData(contract.Input, int64(dataOffset), int64(length)))
		mem.set(memOffset, length, data)

	case CODESIZE:
		st.push(new(big.Int).SetInt64(int64(len(contract.Code))))
	case CODECOPY:
		memOffset, codeOffset, length := st.pop().Uint64(), st.pop().Uint64(), st.pop().Uint64()
		data := make([]byte, length)
		copy(data, getData(contract.Code, int64(codeOffset), int64(length)))
		mem.set(memOffset, length, data)

	case EXTCODESIZE:
		addr := common.AddressFromBig(st.pop())
		st.push(new(big.Int).SetInt64(int64(db.GetCodeSize(addr))))
	case EXTCODECOPY:
		addr := common.AddressFromBig(st.pop())
		memOffset, codeOffset, length := st.pop().Uint64(), st.pop().Uint64(), st.pop().Uint64()
		code := db.GetCode(addr)
		data := make([]byte, length)
		copy(data, getData(code, int64(codeOffset), int64(length)))
		mem.set(memOffset, length, data)
	case EXTCODEHASH:
		addr := common.AddressFromBig(st.pop())
		if !db.Exist(addr) {
			st.push(new(big.Int))
		} else {
			st.push(db.GetCodeHash(addr).Big())
		}

	case RETURNDATASIZE:
		st.push(new(big.Int).SetInt64(int64(len(in.returnData))))
	case RETURNDATACOPY:
		memOffset, dataOffset, length := st.pop().Uint64(), st.pop().Uint64(), st.pop().Uint64()
		if dataOffset+length < dataOffset || dataOffset+length > uint64(len(in.returnData)) {
			panic(ErrReturnDataOutOfBound)
		}
		mem.set(memOffset, length, in.returnData[dataOffset:dataOffset+length])

	case BLOCKHASH:
		num := st.pop().Uint64()
		st.push(blockHashBig(evmCtx.Block, num))
	case COINBASE:
		st.push(new(big.Int).SetBytes(evmCtx.Block.Coinbase.Bytes()))
	case TIMESTAMP:
		st.push(new(big.Int).SetUint64(evmCtx.Block.Timestamp))
	case NUMBER:
		st.push(new(big.Int).Set(evmCtx.Block.Number))
	case DIFFICULTY:
		st.push(new(big.Int).Set(evmCtx.Block.Difficulty))
	case GASLIMIT:
		st.push(new(big.Int).SetUint64(evmCtx.Block.GasLimit))
	case CHAINID:
		st.push(new(big.Int).Set(evmCtx.Block.ChainID))
	case SELFBALANCE:
		st.push(new(big.Int).Set(db.GetBalance(contract.Address())))
	case BASEFEE:
		st.push(new(big.Int).Set(evmCtx.Block.BaseFee))
	}
}

func blockHashBig(block BlockContext, num uint64) *big.Int {
	if h, ok := block.BlockHashes[num]; ok {
		return h.Big()
	}
	// Deterministic default: unknown history hashes are zero.
	return new(big.Int)
}

func getData(data []byte, offset int64, length int64) []byte {
	if length == 0 {
		return nil
	}
	if offset < 0 || length < 0 || offset > int64(len(data)) {
		return make([]byte, length)
	}
	end := offset + length
	out := make([]byte, length)
	if end > int64(len(data)) {
		copy(out, data[offset:])
		return out
	}
	copy(out, data[offset:end])
	return out
}
