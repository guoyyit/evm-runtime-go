package evm

import (
	"math/big"
)

var (
	tt255   = new(big.Int).Lsh(big.NewInt(1), 255) // 2^255
	tt256   = new(big.Int).Lsh(big.NewInt(1), 256) // 2^256
	tt256m1 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
)

// trim256 reduces v modulo 2^256 to keep every stack value a canonical word.
func trim256(v *big.Int) *big.Int {
	if v == nil {
		return nil
	}
	if v.Sign() >= 0 && v.BitLen() <= 256 {
		return v
	}
	return new(big.Int).And(v, tt256m1)
}

// toSigned converts an unsigned 256-bit word to its two's-complement value.
func toSigned(v *big.Int) *big.Int {
	if v.Cmp(tt255) < 0 {
		return new(big.Int).Set(v)
	}
	return new(big.Int).Sub(v, tt256)
}

// fromSigned encodes a signed value as its unsigned 256-bit representation.
func fromSigned(v *big.Int) *big.Int {
	if v.Sign() >= 0 {
		return new(big.Int).Set(v)
	}
	return new(big.Int).Add(v, tt256)
}

func wordBool(b bool) *big.Int {
	if b {
		return big.NewInt(1)
	}
	return new(big.Int)
}

// opArithmetic handles STOP/arithmetic/comparison/bitwise opcodes. It reports
// (halt, error); halt is true only for STOP.
func (in *Interpreter) opArithmetic(op OpCode) (bool, error) {
	need := arity(op)
	args, err := in.popN(need)
	if err != nil {
		return false, err
	}

	var res *big.Int
	switch op {
	case STOP:
		return true, nil

	case ADD:
		res = new(big.Int).Add(args[0], args[1])
	case MUL:
		res = new(big.Int).Mul(args[0], args[1])
	case SUB:
		res = new(big.Int).Sub(args[0], args[1])
	case DIV:
		res = new(big.Int)
		if args[1].Sign() != 0 {
			res.Div(args[0], args[1])
		}
	case SDIV:
		res = sdiv(args[0], args[1])
	case MOD:
		res = new(big.Int)
		if args[1].Sign() != 0 {
			res.Mod(args[0], args[1])
		}
	case SMOD:
		res = smod(args[0], args[1])
	case ADDMOD:
		if args[2].Sign() == 0 {
			res = new(big.Int)
		} else {
			res = new(big.Int).Add(args[0], args[1])
			res.Mod(res, args[2])
		}
	case MULMOD:
		if args[2].Sign() == 0 {
			res = new(big.Int)
		} else {
			res = new(big.Int).Mul(args[0], args[1])
			res.Mod(res, args[2])
		}
	case EXP:
		res = new(big.Int).Exp(args[0], args[1], tt256)
	case SIGNEXTEND:
		res = signExtend(args[0], args[1])

	case LT:
		res = wordBool(args[0].Cmp(args[1]) < 0)
	case GT:
		res = wordBool(args[0].Cmp(args[1]) > 0)
	case SLT:
		x, y := toSigned(args[0]), toSigned(args[1])
		res = wordBool(x.Cmp(y) < 0)
	case SGT:
		x, y := toSigned(args[0]), toSigned(args[1])
		res = wordBool(x.Cmp(y) > 0)
	case EQ:
		res = wordBool(args[0].Cmp(args[1]) == 0)
	case ISZERO:
		res = wordBool(args[0].Sign() == 0)

	case AND:
		res = new(big.Int).And(args[0], args[1])
	case OR:
		res = new(big.Int).Or(args[0], args[1])
	case XOR:
		res = new(big.Int).Xor(args[0], args[1])
	case NOT:
		res = new(big.Int).Xor(args[0], tt256m1)
	case BYTE:
		res = opByte(args[0], args[1])
	case SHL:
		n := shiftAmount(args[0])
		if n >= 256 {
			res = new(big.Int)
		} else {
			res = new(big.Int).Lsh(args[1], uint(n))
		}
	case SHR:
		n := shiftAmount(args[0])
		if n >= 256 {
			res = new(big.Int)
		} else {
			res = new(big.Int).Rsh(args[1], uint(n))
		}
	case SAR:
		res = sar(shiftAmount(args[0]), args[1])
	}

	if op != STOP && res != nil {
		if err := in.push(res); err != nil {
			return false, err
		}
	}
	return false, nil
}

// shiftAmount clamps a 256-bit shift operand to a Go int and caps shifts
// above 256 so big.Int never attempts an enormous allocation.
func shiftAmount(v *big.Int) uint64 {
	if v.BitLen() > 16 { // >= 2^16 is certainly far beyond 256
		return 256
	}
	n := v.Uint64()
	if n > 256 {
		return 256
	}
	return n
}

func arity(op OpCode) int {
	switch op {
	case ISZERO, NOT:
		return 1
	case ADDMOD, MULMOD, SIGNEXTEND:
		return 3
	case STOP:
		return 0
	default:
		return 2
	}
}

// dynamicArithmeticGas handles the per-byte EXP gas premium.
func (in *Interpreter) dynamicArithmeticGas(op OpCode) (uint64, bool) {
	if op != EXP {
		return 0, true
	}
	base, err := in.frame.stack.Peek(1)
	if err != nil {
		return 0, false
	}
	return gasExpBase + gasExpByte*uint64((base.BitLen()+7)/8), true
}

func sdiv(a, b *big.Int) *big.Int {
	if b.Sign() == 0 {
		return new(big.Int)
	}
	x, y := toSigned(a), toSigned(b)
	q := new(big.Int).Quo(x, y)
	return fromSigned(q)
}

func smod(a, b *big.Int) *big.Int {
	if b.Sign() == 0 {
		return new(big.Int)
	}
	x, y := toSigned(a), toSigned(b)
	r := new(big.Int).Rem(x, y)
	return fromSigned(r)
}

func signExtend(back, word *big.Int) *big.Int {
	if back.Cmp(big.NewInt(31)) > 0 {
		return new(big.Int).Set(word)
	}
	bb := uint(back.Uint64())
	bit := uint(bb*8 + 7)
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bit), big.NewInt(1))
	if word.Bit(int(bit)) == 1 {
		return new(big.Int).Or(word, new(big.Int).Xor(tt256m1, mask))
	}
	return new(big.Int).And(word, mask)
}

func opByte(index, word *big.Int) *big.Int {
	i := index.Uint64()
	if i >= 32 {
		return new(big.Int)
	}
	b := byte(new(big.Int).Rsh(word, uint(248-i*8)).Uint64())
	return big.NewInt(int64(b))
}

func sar(n uint64, value *big.Int) *big.Int {
	signed := toSigned(value)
	if n >= 256 {
		if signed.Sign() < 0 {
			return tt256m1
		}
		return new(big.Int)
	}
	r := new(big.Int).Rsh(signed, uint(n))
	return fromSigned(r)
}
