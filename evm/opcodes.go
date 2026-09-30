package evm

var opNames = [256]string{
	0x00: "STOP",
	0x01: "ADD", 0x02: "MUL", 0x03: "SUB", 0x04: "DIV", 0x05: "SDIV",
	0x06: "MOD", 0x07: "SMOD", 0x08: "ADDMOD", 0x09: "MULMOD",
	0x0a: "EXP", 0x0b: "SIGNEXTEND",

	0x10: "LT", 0x11: "GT", 0x12: "SLT", 0x13: "SGT", 0x14: "EQ",
	0x15: "ISZERO", 0x16: "AND", 0x17: "OR", 0x18: "XOR", 0x19: "NOT",
	0x1a: "BYTE", 0x1b: "SHL", 0x1c: "SHR", 0x1d: "SAR",

	0x20: "KECCAK256",

	0x30: "ADDRESS", 0x31: "BALANCE", 0x32: "ORIGIN", 0x33: "CALLER",
	0x34: "CALLVALUE", 0x35: "CALLDATALOAD", 0x36: "CALLDATASIZE",
	0x37: "CALLDATACOPY", 0x38: "CODESIZE", 0x39: "CODECOPY",
	0x3a: "GASPRICE", 0x3b: "EXTCODESIZE", 0x3c: "EXTCODECOPY",
	0x3d: "RETURNDATASIZE", 0x3e: "RETURNDATACOPY", 0x3f: "EXTCODEHASH",

	0x40: "BLOCKHASH", 0x41: "COINBASE", 0x42: "TIMESTAMP", 0x43: "NUMBER",
	0x44: "DIFFICULTY", 0x45: "GASLIMIT", 0x46: "CHAINID",
	0x47: "SELFBALANCE", 0x48: "BASEFEE",

	0x50: "POP", 0x51: "MLOAD", 0x52: "MSTORE", 0x53: "MSTORE8",
	0x54: "SLOAD", 0x55: "SSTORE", 0x56: "JUMP", 0x57: "JUMPI",
	0x58: "PC", 0x59: "MSIZE", 0x5a: "GAS", 0x5b: "JUMPDEST",

	0xa0: "LOG0", 0xa1: "LOG1", 0xa2: "LOG2", 0xa3: "LOG3", 0xa4: "LOG4",

	0xf0: "CREATE", 0xf1: "CALL", 0xf2: "CALLCODE", 0xf3: "RETURN",
	0xf4: "DELEGATECALL", 0xf5: "CREATE2", 0xfa: "STATICCALL",
	0xfd: "REVERT", 0xfe: "INVALID", 0xff: "SELFDESTRUCT",
}

// OpCode is a single EVM instruction byte.
type OpCode byte

func init() {
	for i := 0; i < 32; i++ {
		opNames[0x60+i] = pushName(i + 1)
		opNames[0x80+i] = dupName(i + 1)
		if i < 16 {
			opNames[0x90+i] = swapName(i + 1)
		}
	}
}

func pushName(n int) string {
	return "PUSH" + itoa(n)
}

func dupName(n int) string { return "DUP" + itoa(n) }

func swapName(n int) string { return "SWAP" + itoa(n) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [3]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func (op OpCode) String() string {
	if name := opNames[op]; name != "" {
		return name
	}
	return "opcode 0x" + hexByte(byte(op))
}

func hexByte(b byte) string {
	const hexdigits = "0123456789abcdef"
	return string([]byte{hexdigits[b>>4], hexdigits[b&0xf]})
}

// IsPush reports whether op is a PUSH1..PUSH32 instruction.
func (op OpCode) IsPush() bool { return op >= PUSH1 && op <= PUSH32 }

// PushSize returns the operand width for PUSH instructions, else 0.
func (op OpCode) PushSize() int {
	if op.IsPush() {
		return int(op-PUSH1) + 1
	}
	return 0
}

// IsDup reports whether op is DUP1..DUP16.
func (op OpCode) IsDup() bool { return op >= DUP1 && op <= DUP16 }

// IsSwap reports whether op is SWAP1..SWAP16.
func (op OpCode) IsSwap() bool { return op >= SWAP1 && op <= SWAP16 }

// IsLog reports whether op is LOG0..LOG4.
func (op OpCode) IsLog() bool { return op >= LOG0 && op <= LOG4 }

// EVM instruction set. Unlisted opcodes are invalid.
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

	KECCAK256 OpCode = 0x20

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
	EXTCODEHASH    OpCode = 0x3f

	BLOCKHASH   OpCode = 0x40
	COINBASE    OpCode = 0x41
	TIMESTAMP   OpCode = 0x42
	NUMBER      OpCode = 0x43
	DIFFICULTY  OpCode = 0x44
	GASLIMIT    OpCode = 0x45
	CHAINID     OpCode = 0x46
	SELFBALANCE OpCode = 0x47
	BASEFEE     OpCode = 0x48

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

	PUSH1  OpCode = 0x60
	PUSH32 OpCode = 0x7f
	DUP1   OpCode = 0x80
	DUP16  OpCode = 0x8f
	SWAP1  OpCode = 0x90
	SWAP16 OpCode = 0x9f

	LOG0 OpCode = 0xa0
	LOG1 OpCode = 0xa1
	LOG2 OpCode = 0xa2
	LOG3 OpCode = 0xa3
	LOG4 OpCode = 0xa4

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

// opInfo records static metadata for a single opcode.
type opInfo struct {
	pops   int
	pushes int
}

func opcodeInfo(op OpCode) opInfo {
	if op.IsPush() {
		return opInfo{pushes: 1}
	}
	if op.IsDup() {
		return opInfo{pops: int(op-DUP1) + 1, pushes: 1}
	}
	if op.IsSwap() {
		return opInfo{pops: int(op-SWAP1) + 2}
	}
	if op.IsLog() {
		return opInfo{pops: 2 + int(op-LOG0)}
	}
	info, ok := opStackSpec[op]
	if !ok {
		return opInfo{pops: -1}
	}
	return info
}

var opStackSpec = map[OpCode]opInfo{
	STOP: {}, ADD: {2, 1}, MUL: {2, 1}, SUB: {2, 1}, DIV: {2, 1},
	SDIV: {2, 1}, MOD: {2, 1}, SMOD: {2, 1}, ADDMOD: {3, 1},
	MULMOD: {3, 1}, EXP: {2, 1}, SIGNEXTEND: {2, 1},

	LT: {2, 1}, GT: {2, 1}, SLT: {2, 1}, SGT: {2, 1}, EQ: {2, 1},
	ISZERO: {1, 1}, AND: {2, 1}, OR: {2, 1}, XOR: {2, 1}, NOT: {1, 1},
	BYTE: {2, 1}, SHL: {2, 1}, SHR: {2, 1}, SAR: {2, 1},

	KECCAK256: {2, 1},

	ADDRESS: {0, 1}, BALANCE: {1, 1}, ORIGIN: {0, 1}, CALLER: {0, 1},
	CALLVALUE: {0, 1}, CALLDATALOAD: {1, 1}, CALLDATASIZE: {0, 1},
	CALLDATACOPY: {3, 0}, CODESIZE: {0, 1}, CODECOPY: {3, 0},
	GASPRICE: {0, 1}, EXTCODESIZE: {1, 1}, EXTCODECOPY: {4, 0},
	RETURNDATASIZE: {0, 1}, RETURNDATACOPY: {3, 0}, EXTCODEHASH: {1, 1},

	BLOCKHASH: {1, 1}, COINBASE: {0, 1}, TIMESTAMP: {0, 1}, NUMBER: {0, 1},
	DIFFICULTY: {0, 1}, GASLIMIT: {0, 1}, CHAINID: {0, 1},
	SELFBALANCE: {0, 1}, BASEFEE: {0, 1},

	POP: {1, 0}, MLOAD: {1, 1}, MSTORE: {2, 0}, MSTORE8: {2, 0},
	SLOAD: {1, 1}, SSTORE: {2, 0}, JUMP: {1, 0}, JUMPI: {2, 0},
	PC: {0, 1}, MSIZE: {0, 1}, GAS: {0, 1}, JUMPDEST: {},

	RETURN: {2, 0}, REVERT: {2, 0},

	CREATE: {3, 1}, CREATE2: {4, 1},
	CALL: {7, 1}, CALLCODE: {7, 1}, DELEGATECALL: {6, 1},
	STATICCALL: {6, 1}, SELFDESTRUCT: {1, 0},
}
