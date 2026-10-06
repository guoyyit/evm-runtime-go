package evm

import (
	"math/big"

	"github.com/guoyyit/evm-runtime-go/common"
)

// Interpreter executes one contract frame.
type Interpreter struct {
	evm      *EVM
	readOnly bool

	stack      *Stack
	memory     *Memory
	returnData []byte
}

func newInterpreter(evm *EVM, readOnly bool) *Interpreter {
	return &Interpreter{evm: evm, readOnly: readOnly}
}

// Run executes the contract code and returns the return data.
func (in *Interpreter) Run(contract *Contract, input []byte, readOnly bool) (ret []byte, err error) {
	in.stack = newstack()
	in.memory = newMemory()
	in.readOnly = readOnly
	in.returnData = nil

	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				// An exceptional halt consumes the frame's remaining gas.
				contract.Gas = 0
				err = &Error{Err: e}
				return
			}
			panic(r)
		}
	}()

	code := contract.Code
	pc := 0

	// fail reports an exceptional halt: the frame's remaining gas is
	// consumed, unlike REVERT which returns the unused gas to the caller.
	fail := func(err error, pc int, op OpCode) ([]byte, error) {
		contract.Gas = 0
		return nil, &Error{Err: err, PC: uint64(pc), Op: op}
	}

	// Running past the last instruction is a legal implicit STOP: the frame
	// halts successfully with empty output, exactly like an explicit STOP.
	for pc < len(code) {
		op := OpCode(code[pc])

		// Static gas and stack validation happen before memory expansion.
		info := opcodeInfo(op)
		if info.pops < 0 {
			return fail(ErrInvalidOpcode, pc, op)
		}
		if err := in.stack.require(info.pops, info.pushes); err != nil {
			return fail(err, pc, op)
		}
		if err := in.chargeStatic(contract, op, pc); err != nil {
			return fail(err, pc, op)
		}

		// Memory-expanding operations charge for the new size up front.
		if err := in.chargeMemory(contract, op); err != nil {
			return fail(err, pc, op)
		}

		if op.IsPush() {
			// Operand bytes are read past the current pc; code beyond end is 0.
			start := pc + 1
			end := start + op.PushSize()
			immediate := make([]byte, op.PushSize())
			if start < len(code) {
				if end > len(code) {
					end = len(code)
				}
				copy(immediate, code[start:end])
			}
			in.stack.push(new(big.Int).SetBytes(immediate))
			pc = end
			continue
		}

		nextPC, halted, out, runErr := in.execute(contract, op, pc)
		if runErr != nil {
			if rev, ok := runErr.(*revertError); ok {
				// REVERT returns its payload and keeps the remaining gas.
				evmErr := &Error{Err: ErrExecutionReverted, PC: uint64(pc), Op: op, RevertData: rev.data}
				return rev.data, evmErr
			}
			return fail(runErr, pc, op)
		}
		if halted {
			return out, nil
		}
		pc = nextPC
	}
	// Implicit termination: the program counter walked off the end of the
	// bytecode. Treat as STOP — success with no return data.
	return nil, nil
}

// useGas deducts amount from the frame gas budget.
func (in *Interpreter) useGas(contract *Contract, amount uint64) error {
	if amount > contract.Gas {
		contract.Gas = 0
		return ErrOutOfGas
	}
	contract.Gas -= amount
	return nil
}

func (in *Interpreter) chargeStatic(contract *Contract, op OpCode, pc int) error {
	cost, ok := staticGasCost(op)
	if !ok {
		return ErrInvalidOpcode
	}
	return in.useGas(contract, cost)
}

func staticGasCost(op OpCode) (uint64, bool) {
	if op.IsPush() {
		return GasFastestStep, true
	}
	if op.IsDup() || op.IsSwap() {
		return GasFastestStep, true
	}
	cost, ok := staticGasTable[op]
	return cost, ok
}

var staticGasTable = map[OpCode]uint64{
	STOP: 0, ADD: GasFastestStep, MUL: GasQuickStep, SUB: GasFastestStep,
	DIV: GasFastStep, SDIV: GasFastStep, MOD: GasFastStep, SMOD: GasFastStep,
	ADDMOD: GasMidStep, MULMOD: GasMidStep, SIGNEXTEND: GasLowStep,

	LT: GasFastestStep, GT: GasFastestStep, SLT: GasFastestStep, SGT: GasFastestStep,
	EQ: GasFastestStep, ISZERO: GasFastestStep, AND: GasFastestStep, OR: GasFastestStep,
	XOR: GasFastestStep, NOT: GasFastestStep, BYTE: GasFastestStep,
	SHL: GasFastestStep, SHR: GasFastestStep, SAR: GasFastestStep,

	KECCAK256: GasKeccak256,

	ADDRESS: GasQuickStep, BALANCE: 0, ORIGIN: GasQuickStep, CALLER: GasQuickStep,
	CALLVALUE: GasQuickStep, CALLDATALOAD: GasFastestStep,
	CALLDATASIZE: GasQuickStep, CALLDATACOPY: GasFastestStep,
	CODESIZE: GasQuickStep, CODECOPY: GasFastestStep, GASPRICE: GasQuickStep,
	EXTCODESIZE: 0, EXTCODECOPY: 0, EXTCODEHASH: 0,
	RETURNDATASIZE: GasQuickStep, RETURNDATACOPY: GasFastestStep,

	COINBASE: GasQuickStep, TIMESTAMP: GasQuickStep, NUMBER: GasQuickStep,
	DIFFICULTY: GasQuickStep, GASLIMIT: GasQuickStep, CHAINID: GasQuickStep,
	SELFBALANCE: GasLowStep, BASEFEE: GasQuickStep,
	BLOCKHASH: GasExtStep,

	POP: GasQuickStep, MLOAD: GasFastestStep, MSTORE: GasFastestStep,
	MSTORE8: GasFastestStep, SLOAD: 0, SSTORE: 0,
	JUMP: GasMidStep, JUMPI: GasHighStep,
	PC: GasQuickStep, MSIZE: GasQuickStep, GAS: GasQuickStep, JUMPDEST: 1,

	EXP: GasSlowStep,

	LOG0: GasLog, LOG1: GasLog, LOG2: GasLog, LOG3: GasLog, LOG4: GasLog,

	CREATE: GasCreate, CREATE2: GasCreate,
	CALL: 0, CALLCODE: 0, DELEGATECALL: 0, STATICCALL: 0,
	SELFDESTRUCT: GasSelfDestruct,
	RETURN:       0, REVERT: 0,
}

// execute runs a single non-PUSH opcode and returns the next pc.
func (in *Interpreter) execute(contract *Contract, op OpCode, pc int) (nextPC int, halted bool, output []byte, err error) {
	st := in.stack

	switch {
	case op.IsDup():
		st.dup(int(op-DUP1) + 1)
		return pc + 1, false, nil, nil
	case op.IsSwap():
		st.swap(int(op-SWAP1) + 1)
		return pc + 1, false, nil, nil
	}

	switch op {
	case STOP, RETURN, REVERT, SELFDESTRUCT,
		SLOAD, SSTORE,
		LOG0, LOG1, LOG2, LOG3, LOG4,
		KECCAK256:
		halted, output, err = in.opSystem(contract, op)
		return pc + 1, halted, output, err

	case MLOAD, MSTORE, MSTORE8, JUMP, JUMPI, PC, MSIZE, GAS, JUMPDEST, POP:
		nextPC, err = in.opFlow(contract, op, pc)
		return nextPC, false, nil, err

	case ADDRESS, BALANCE, ORIGIN, CALLER, CALLVALUE, GASPRICE,
		CALLDATALOAD, CALLDATASIZE, CALLDATACOPY,
		CODESIZE, CODECOPY, EXTCODESIZE, EXTCODECOPY, EXTCODEHASH,
		RETURNDATASIZE, RETURNDATACOPY,
		BLOCKHASH, COINBASE, TIMESTAMP, NUMBER, DIFFICULTY, GASLIMIT,
		CHAINID, SELFBALANCE, BASEFEE:
		in.opEnv(contract, op)
		return pc + 1, false, nil, nil

	case CALL, CALLCODE, DELEGATECALL, STATICCALL, CREATE, CREATE2:
		err = in.opCall(contract, op)
		return pc + 1, false, nil, err

	case INVALID:
		return pc, false, nil, ErrInvalidOpcode
	}

	if err := in.opArithmetic(contract, op); err != nil {
		return pc + 1, false, nil, err
	}
	return pc + 1, false, nil, nil
}

var _ = common.Address{}
