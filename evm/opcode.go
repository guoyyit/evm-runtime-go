package evm

// OpCode is a single EVM instruction.
type OpCode byte

// 0x0 range: stop and arithmetic.
const (
	STOP       OpCode = 0x00
	ADD        OpCode = 0x01
	MUL        OpCode = 0x02
	SUB        OpCode = 0x03
	DIV        OpCode = 0x04
	SDIV       OpCode = 0x05
	MOD        OpCode = 0x06
	SMOD       OpCode = 0x07
	ADDMOD     OpCode = 0x08
	MULMOD     OpCode = 0x09
	EXP        OpCode = 0x0a
	SIGNEXTEND OpCode = 0x0b
)

// 0x10/0x20 range: comparison, bitwise and hashing.
const (
	LT     OpCode = 0x10
	GT     OpCode = 0x11
	SLT    OpCode = 0x12
	SGT    OpCode = 0x13
	EQ     OpCode = 0x14
	ISZERO OpCode = 0x15
	AND    OpCode = 0x16
	OR     OpCode = 0x17
	XOR    OpCode = 0x18
	NOT    OpCode = 0x19
	BYTE   OpCode = 0x1a
	SHL    OpCode = 0x1b
	SHR    OpCode = 0x1c
	SAR    OpCode = 0x1d
	SHA3   OpCode = 0x20
)

// 0x30 range: execution-context information.
const (
	ADDRESS        OpCode = 0x30
	BALANCE        OpCode = 0x31
	ORIGIN         OpCode = 0x32
	CALLER         OpCode = 0x33
	CALLVALUE      OpCode = 0x34
	CALLDATALOAD   OpCode = 0x35
	CALLDATASIZE   OpCode = 0x36
	CALLDATACOPY   OpCode = 0x37
	CODESIZE       OpCode = 0x38
	CODECOPY       OpCode = 0x39
	GASPRICE       OpCode = 0x3a
	EXTCODESIZE    OpCode = 0x3b
	EXTCODECOPY    OpCode = 0x3c
	RETURNDATASIZE OpCode = 0x3d
	RETURNDATACOPY OpCode = 0x3e
)

// 0x40 range: block information.
const (
	BLOCKHASH   OpCode = 0x40
	COINBASE    OpCode = 0x41
	TIMESTAMP   OpCode = 0x42
	NUMBER      OpCode = 0x43
	DIFFICULTY  OpCode = 0x44
	GASLIMIT    OpCode = 0x45
	CHAINID     OpCode = 0x46
	SELFBALANCE OpCode = 0x47
	BASEFEE     OpCode = 0x48
)

// 0x50 range: stack, memory, storage and flow control.
const (
	POP      OpCode = 0x50
	MLOAD    OpCode = 0x51
	MSTORE   OpCode = 0x52
	MSTORE8  OpCode = 0x53
	SLOAD    OpCode = 0x54
	SSTORE   OpCode = 0x55
	JUMP     OpCode = 0x56
	JUMPI    OpCode = 0x57
	PC       OpCode = 0x58
	MSIZE    OpCode = 0x59
	GAS      OpCode = 0x5a
	JUMPDEST OpCode = 0x5b
	PUSH0    OpCode = 0x5f
)

// PUSH1..PUSH32 and DUP/SWAP ranges.
const (
	PUSH1  OpCode = 0x60
	PUSH32 OpCode = 0x7f
	DUP1   OpCode = 0x80
	DUP16  OpCode = 0x8f
	SWAP1  OpCode = 0x90
	SWAP16 OpCode = 0x9f
)

// 0xa0 range: event logs.
const (
	LOG0 OpCode = 0xa0
	LOG1 OpCode = 0xa1
	LOG2 OpCode = 0xa2
	LOG3 OpCode = 0xa3
	LOG4 OpCode = 0xa4
)

// 0xf0 range: calls, creation and termination.
const (
	CREATE       OpCode = 0xf0
	CALL         OpCode = 0xf1
	CALLCODE     OpCode = 0xf2
	RETURN       OpCode = 0xf3
	DELEGATECALL OpCode = 0xf4
	CREATE2      OpCode = 0xf5
	STATICCALL   OpCode = 0xfa
	REVERT       OpCode = 0xfd
	INVALID      OpCode = 0xfe
	SELFDESTRUCT OpCode = 0xff
)

// Gas prices, following the Berlin/EIP-2929 schedule with a warm touched
// set that contains the origin and precompiles for every frame.
const (
	gasZero              uint64 = 0
	gasJumpdest          uint64 = 1
	gasQuickStep         uint64 = 3
	gasFastestStep       uint64 = 2
	gasFastStep          uint64 = 5
	gasMidStep           uint64 = 8
	gasExt               uint64 = 700
	gasBalance           uint64 = 700 // BALANCE on a cold account
	gasSload             uint64 = 800 // cold SLOAD
	gasWarmAccess        uint64 = 100 // access to an already warm account/slot
	gasSstoreSet         uint64 = 20000
	gasSstoreReset       uint64 = 5000
	gasSstoreNoop        uint64 = 100
	gasSstoreClearRefund uint64 = 4800
	gasSelfdestruct      uint64 = 5000
	gasCreate            uint64 = 32000
	gasCreate2           uint64 = 32000
	gasCall              uint64 = 700 // cold account access base
	gasCallValue         uint64 = 9000
	gasCallStipend       uint64 = 2300
	gasNewAccount        uint64 = 25000
	gasExpBase           uint64 = 10
	gasExpByte           uint64 = 50
	gasSha3Base          uint64 = 30
	gasSha3Word          uint64 = 6
	gasCopy              uint64 = 3
	gasLog               uint64 = 375
	gasLogTopic          uint64 = 375
	gasLogData           uint64 = 8
	gasMemory            uint64 = 3
	gasCodeDeposit       uint64 = 200
	gasQuadCoeffDiv      uint64 = 512
	MaxCallDepth         int    = 1024
)

func isPush(op OpCode) bool   { return PUSH1 <= op && op <= PUSH32 }
func isDup(op OpCode) bool    { return DUP1 <= op && op <= DUP16 }
func isSwap(op OpCode) bool   { return SWAP1 <= op && op <= SWAP16 }
func isLog(op OpCode) bool    { return LOG0 <= op && op <= LOG4 }
func pushSize(op OpCode) int  { return int(op-PUSH1) + 1 }
func dupIndex(op OpCode) int  { return int(op-DUP1) + 1 }
func swapIndex(op OpCode) int { return int(op-SWAP1) + 1 }
func logTopics(op OpCode) int { return int(op - LOG0) }

// staticGas returns the fixed gas component of op. Opcodes whose cost depends
// on operands (memory, storage, calls, EXP, SHA3, LOG, creation) return 0 and
// are priced by the interpreter.
func staticGas(op OpCode) uint64 {
	switch {
	case op == STOP || op == RETURN || op == REVERT:
		return gasZero
	case isPush(op) || op == PUSH0:
		return gasQuickStep
	case isDup(op) || isSwap(op):
		return gasQuickStep
	case op == JUMPDEST:
		return gasJumpdest
	}
	switch op {
	case ADD, SUB, NOT, LT, GT, SLT, SGT, EQ, ISZERO, AND, OR, XOR, BYTE, SHL, SHR, SAR:
		return gasQuickStep
	case MUL, DIV, SDIV, MOD, SMOD, SIGNEXTEND:
		return gasMidStep
	case ADDMOD, MULMOD:
		return gasMidStep
	case EXP:
		return gasExpBase
	case SHA3:
		return gasSha3Base
	case ADDRESS, ORIGIN, CALLER, CALLVALUE, CALLDATASIZE, CODESIZE,
		RETURNDATASIZE, PC, MSIZE, GAS:
		return gasFastestStep
	case CALLDATALOAD:
		return gasQuickStep
	case GASPRICE, COINBASE, TIMESTAMP, NUMBER, DIFFICULTY, GASLIMIT,
		CHAINID, SELFBALANCE, BASEFEE, BLOCKHASH:
		return gasFastestStep
	case POP:
		return gasFastestStep
	case SLOAD:
		return 0
	case SSTORE:
		return 0
	case MLOAD, MSTORE, MSTORE8, CALLDATACOPY, CODECOPY, RETURNDATACOPY:
		return 0
	case BALANCE, EXTCODESIZE, EXTCODECOPY:
		return 0
	case CREATE, CREATE2, CALL, CALLCODE, DELEGATECALL, STATICCALL:
		return 0
	case SELFDESTRUCT:
		return 0
	default:
		if isLog(op) {
			return 0
		}
	}
	return 0
}

// isValid reports whether op is a recognized instruction.
func isValid(op OpCode) bool {
	switch {
	case op <= SIGNEXTEND:
		return true
	case LT <= op && op <= SAR:
		return true
	case op == SHA3:
		return true
	case ADDRESS <= op && op <= RETURNDATACOPY:
		return true
	case BLOCKHASH <= op && op <= BASEFEE:
		return true
	case POP <= op && op <= JUMPDEST:
		return true
	case op == PUSH0 || isPush(op) || isDup(op) || isSwap(op):
		return true
	case isLog(op):
		return true
	case CREATE <= op && op <= CREATE2:
		return true
	case op == STATICCALL || op == REVERT || op == INVALID || op == SELFDESTRUCT:
		return true
	}
	return false
}

var mnemonics = map[OpCode]string{
	STOP: "STOP", ADD: "ADD", MUL: "MUL", SUB: "SUB", DIV: "DIV", SDIV: "SDIV",
	MOD: "MOD", SMOD: "SMOD", ADDMOD: "ADDMOD", MULMOD: "MULMOD", EXP: "EXP",
	SIGNEXTEND: "SIGNEXTEND",
	LT:         "LT", GT: "GT", SLT: "SLT", SGT: "SGT", EQ: "EQ", ISZERO: "ISZERO",
	AND: "AND", OR: "OR", XOR: "XOR", NOT: "NOT", BYTE: "BYTE",
	SHL: "SHL", SHR: "SHR", SAR: "SAR", SHA3: "SHA3",
	ADDRESS: "ADDRESS", BALANCE: "BALANCE", ORIGIN: "ORIGIN", CALLER: "CALLER",
	CALLVALUE: "CALLVALUE", CALLDATALOAD: "CALLDATALOAD",
	CALLDATASIZE: "CALLDATASIZE", CALLDATACOPY: "CALLDATACOPY",
	CODESIZE: "CODESIZE", CODECOPY: "CODECOPY", GASPRICE: "GASPRICE",
	EXTCODESIZE: "EXTCODESIZE", EXTCODECOPY: "EXTCODECOPY",
	RETURNDATASIZE: "RETURNDATASIZE", RETURNDATACOPY: "RETURNDATACOPY",
	BLOCKHASH: "BLOCKHASH", COINBASE: "COINBASE", TIMESTAMP: "TIMESTAMP",
	NUMBER: "NUMBER", DIFFICULTY: "DIFFICULTY", GASLIMIT: "GASLIMIT",
	CHAINID: "CHAINID", SELFBALANCE: "SELFBALANCE", BASEFEE: "BASEFEE",
	POP: "POP", MLOAD: "MLOAD", MSTORE: "MSTORE", MSTORE8: "MSTORE8",
	SLOAD: "SLOAD", SSTORE: "SSTORE", JUMP: "JUMP", JUMPI: "JUMPI",
	PC: "PC", MSIZE: "MSIZE", GAS: "GAS", JUMPDEST: "JUMPDEST", PUSH0: "PUSH0",
	DUP1: "DUP1", DUP16: "DUP16", SWAP1: "SWAP1", SWAP16: "SWAP16",
	LOG0: "LOG0", LOG1: "LOG1", LOG2: "LOG2", LOG3: "LOG3", LOG4: "LOG4",
	CREATE: "CREATE", CALL: "CALL", CALLCODE: "CALLCODE", RETURN: "RETURN",
	DELEGATECALL: "DELEGATECALL", CREATE2: "CREATE2", STATICCALL: "STATICCALL",
	REVERT: "REVERT", INVALID: "INVALID", SELFDESTRUCT: "SELFDESTRUCT",
}

// String returns the opcode mnemonic, or "UNKNOWN(0x..)".
func (op OpCode) String() string {
	if name, ok := mnemonics[op]; ok {
		return name
	}
	switch {
	case isPush(op):
		return "PUSH" + itoa(int(op-PUSH1)+1)
	case isDup(op):
		return "DUP" + itoa(dupIndex(op))
	case isSwap(op):
		return "SWAP" + itoa(swapIndex(op))
	}
	return "UNKNOWN(0x" + hexByte(byte(op)) + ")"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [4]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func hexByte(b byte) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[b>>4], digits[b&0xf]})
}
