package evm

import (
	"math/big"
)

// opContext handles the 0x30-range transaction/contract information opcodes.
func (in *Interpreter) opContext(op OpCode) (bool, error) {
	c := in.frame.contract
	st := in.evm.State

	switch op {
	case ADDRESS:
		return false, in.push(addressBig(c.Self))
	case BALANCE:
		args, err := in.popN(1)
		if err != nil {
			return false, err
		}
		addr := bigToAddress(args[0])
		// Cold/warm account access charged here; gasFor leaves staticGas 0.
		if !in.evm.touchAccount(addr, &c.Gas) {
			return false, ErrOutOfGas
		}
		return false, in.push(st.GetBalance(addr))

	case ORIGIN:
		return false, in.push(addressBig(in.evm.Tx.Origin))
	case CALLER:
		return false, in.push(addressBig(c.Caller))
	case CALLVALUE:
		v := c.Value
		if v == nil {
			v = new(big.Int)
		}
		return false, in.push(v)

	case CALLDATALOAD:
		args, err := in.popN(1)
		if err != nil {
			return false, err
		}
		off := args[0].Uint64()
		buf := make([]byte, 32)
		if args[0].BitLen() <= 64 {
			for i := uint64(0); i < 32; i++ {
				idx := off + i
				if idx < uint64(len(c.Input)) {
					buf[i] = c.Input[idx]
				}
			}
		}
		return false, in.push(new(big.Int).SetBytes(buf))

	case CALLDATASIZE:
		return false, in.push(big.NewInt(int64(len(c.Input))))

	case CALLDATACOPY:
		return in.copyToMemory(len(c.Input), func(off uint64) byte {
			if off < uint64(len(c.Input)) {
				return c.Input[off]
			}
			return 0
		})

	case CODESIZE:
		return false, in.push(big.NewInt(int64(len(c.Code))))

	case CODECOPY:
		code := c.Code
		return in.copyToMemory(len(code), func(off uint64) byte {
			if off < uint64(len(code)) {
				return code[off]
			}
			return 0
		})

	case GASPRICE:
		return false, in.push(in.evm.Tx.GasPrice)

	case EXTCODESIZE:
		args, err := in.popN(1)
		if err != nil {
			return false, err
		}
		addr := bigToAddress(args[0])
		if !in.evm.touchAccount(addr, &c.Gas) {
			return false, ErrOutOfGas
		}
		return false, in.push(big.NewInt(int64(len(st.GetCode(addr)))))

	case EXTCODECOPY:
		args, err := in.popN(4)
		if err != nil {
			return false, err
		}
		addr := bigToAddress(args[0])
		if !in.evm.touchAccount(addr, &c.Gas) {
			return false, ErrOutOfGas
		}
		memOff := args[1].Uint64()
		codeOff := args[2].Uint64()
		size := args[3].Uint64()
		if args[1].BitLen() > 64 || args[2].BitLen() > 64 || args[3].BitLen() > 64 {
			return false, ErrOutOfGas
		}
		if !requireMemory(in.frame.memory, &c.Gas, memOff, size) {
			return false, ErrOutOfGas
		}
		code := st.GetCode(addr)
		buf := make([]byte, size)
		for i := uint64(0); i < size; i++ {
			if codeOff+i < uint64(len(code)) {
				buf[i] = code[codeOff+i]
			}
		}
		in.frame.memory.Set(memOff, buf)
		return false, nil

	case RETURNDATASIZE:
		return false, in.push(big.NewInt(int64(len(in.frame.lastReturn))))

	case RETURNDATACOPY:
		return in.opReturnDataCopy()
	}
	return false, ErrInvalidOpCode
}

// copyToMemory implements CALLDATACOPY/CODECOPY: args are
// (memOffset, dataOffset, length), charged 3 gas per word plus memory gas.
func (in *Interpreter) copyToMemory(srcLen int, at func(off uint64) byte) (bool, error) {
	args, err := in.popN(3)
	if err != nil {
		return false, err
	}
	if args[0].BitLen() > 64 || args[1].BitLen() > 64 || args[2].BitLen() > 64 {
		return false, ErrOutOfGas
	}
	memOff, dataOff, size := args[0].Uint64(), args[1].Uint64(), args[2].Uint64()
	if !requireMemory(in.frame.memory, &in.frame.contract.Gas, memOff, size) {
		return false, ErrOutOfGas
	}
	buf := make([]byte, size)
	for i := uint64(0); i < size; i++ {
		off := dataOff + i
		if off < uint64(srcLen) {
			buf[i] = at(off)
		}
	}
	in.frame.memory.Set(memOff, buf)
	return false, nil
}

func (in *Interpreter) opReturnDataCopy() (bool, error) {
	args, err := in.popN(3)
	if err != nil {
		return false, err
	}
	if args[0].BitLen() > 64 || args[1].BitLen() > 64 || args[2].BitLen() > 64 {
		return false, ErrOutOfGas
	}
	memOff, dataOff, size := args[0].Uint64(), args[1].Uint64(), args[2].Uint64()
	data := in.frame.lastReturn
	if dataOff+size > uint64(len(data)) || dataOff+size < dataOff {
		return false, ErrReturnDataOOB
	}
	if !requireMemory(in.frame.memory, &in.frame.contract.Gas, memOff, size) {
		return false, ErrOutOfGas
	}
	buf := make([]byte, size)
	copy(buf, data[dataOff:dataOff+size])
	in.frame.memory.Set(memOff, buf)
	return false, nil
}

// opBlock handles the 0x40-range block information opcodes.
func (in *Interpreter) opBlock(op OpCode) (bool, error) {
	b := in.evm.Block
	switch op {
	case BLOCKHASH:
		args, err := in.popN(1)
		if err != nil {
			return false, err
		}
		// Only the 256 most recent block numbers produce hashes; others are 0.
		cur := b.Number
		n := args[0]
		if cur != nil && n.Cmp(cur) < 0 &&
			new(big.Int).Sub(cur, n).Cmp(big.NewInt(256)) <= 0 {
			h := blockHash(b, n)
			return false, in.push(new(big.Int).SetBytes(h[:]))
		}
		return false, in.push(new(big.Int))
	case COINBASE:
		return false, in.push(addressBig(b.CoinBase))
	case TIMESTAMP:
		return false, in.push(b.Time)
	case NUMBER:
		return false, in.push(b.Number)
	case DIFFICULTY:
		return false, in.push(b.Difficulty)
	case GASLIMIT:
		return false, in.push(b.GasLimit)
	case CHAINID:
		return false, in.push(b.ChainID)
	case SELFBALANCE:
		return false, in.push(in.evm.State.GetBalance(in.frame.contract.Self))
	case BASEFEE:
		return false, in.push(b.BaseFee)
	}
	return false, ErrInvalidOpCode
}

func addressBig(a [20]byte) *big.Int {
	return new(big.Int).SetBytes(a[:])
}
