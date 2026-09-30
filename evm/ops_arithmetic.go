package evm

import "math/big"

var (
	big1       = big.NewInt(1)
	big32      = big.NewInt(32)
	big256     = new(big.Int).Lsh(big.NewInt(1), 8)
	big2e255   = new(big.Int).Lsh(big.NewInt(1), 255)
	big2e256   = new(big.Int).Lsh(big.NewInt(1), 256)
	big2e256m1 = new(big.Int).Sub(big2e256, big1)
)

func (in *Interpreter) opArithmetic(contract *Contract, op OpCode) error {
	st := in.stack
	switch op {
	case ADD:
		x, y := st.pop(), st.pop()
		sum := new(big.Int).Add(x, y)
		st.push(sum.And(sum, big2e256m1))
	case MUL:
		x, y := st.pop(), st.pop()
		product := new(big.Int).Mul(x, y)
		st.push(product.And(product, big2e256m1))
	case SUB:
		x, y := st.pop(), st.pop()
		r := new(big.Int).Sub(x, y)
		st.push(mod256(r))
	case DIV:
		x, y := st.pop(), st.pop()
		if y.Sign() == 0 {
			st.push(new(big.Int))
		} else {
			st.push(new(big.Int).Quo(x, y))
		}
	case SDIV:
		x, y := u256ToSigned(st.pop()), u256ToSigned(st.pop())
		if y.Sign() == 0 {
			st.push(new(big.Int))
		} else {
			q := new(big.Int).Quo(x, y)
			st.push(signedToU256(q))
		}
	case MOD:
		x, y := st.pop(), st.pop()
		if y.Sign() == 0 {
			st.push(new(big.Int))
		} else {
			st.push(new(big.Int).Mod(x, y))
		}
	case SMOD:
		x, y := u256ToSigned(st.pop()), u256ToSigned(st.pop())
		if y.Sign() == 0 {
			st.push(new(big.Int))
		} else {
			m := new(big.Int).Mod(x, y)
			st.push(signedToU256(m))
		}
	case ADDMOD:
		x, y, z := st.pop(), st.pop(), st.pop()
		if z.Sign() == 0 {
			st.push(new(big.Int))
		} else {
			add := new(big.Int).Add(x, y)
			st.push(new(big.Int).Mod(add, z))
		}
	case MULMOD:
		x, y, z := st.pop(), st.pop(), st.pop()
		if z.Sign() == 0 {
			st.push(new(big.Int))
		} else {
			mul := new(big.Int).Mul(x, y)
			st.push(new(big.Int).Mod(mul, z))
		}
	case EXP:
		base, exponent := st.pop(), st.pop()
		st.push(new(big.Int).Exp(base, exponent, big2e256))
	case SIGNEXTEND:
		back := st.pop()
		if back.Cmp(big32) < 0 {
			x := st.pop()
			bits := new(big.Int).Mul(big.NewInt(8), new(big.Int).Add(back, big1))
			mask := new(big.Int).Sub(new(big.Int).Lsh(big1, uint(bits.Uint64())), big1)
			if x.Bit(int(bits.Uint64())-1) == 1 {
				x.Or(x, new(big.Int).Not(mask))
			} else {
				x.And(x, mask)
			}
			st.push(mod256(x))
		} else {
			st.push(st.pop())
		}

	case LT:
		x, y := st.pop(), st.pop()
		st.push(boolToBig(x.Cmp(y) < 0))
	case GT:
		x, y := st.pop(), st.pop()
		st.push(boolToBig(x.Cmp(y) > 0))
	case SLT:
		x, y := u256ToSigned(st.pop()), u256ToSigned(st.pop())
		st.push(boolToBig(x.Cmp(y) < 0))
	case SGT:
		x, y := u256ToSigned(st.pop()), u256ToSigned(st.pop())
		st.push(boolToBig(x.Cmp(y) > 0))
	case EQ:
		x, y := st.pop(), st.pop()
		st.push(boolToBig(x.Cmp(y) == 0))
	case ISZERO:
		st.push(boolToBig(st.pop().Sign() == 0))

	case AND:
		x, y := st.pop(), st.pop()
		st.push(new(big.Int).And(x, y))
	case OR:
		x, y := st.pop(), st.pop()
		st.push(new(big.Int).Or(x, y))
	case XOR:
		x, y := st.pop(), st.pop()
		st.push(new(big.Int).Xor(x, y))
	case NOT:
		x := st.pop()
		st.push(new(big.Int).And(new(big.Int).Not(x), big2e256m1))

	case BYTE:
		i, x := st.pop(), st.pop()
		if i.Cmp(big32) < 0 {
			shift := uint(248 - i.Uint64()*8)
			masked := new(big.Int).Rsh(x, shift)
			masked.And(masked, big.NewInt(0xff))
			st.push(masked)
		} else {
			st.push(new(big.Int))
		}
	case SHL:
		shift, value := st.pop(), st.pop()
		if shift.Cmp(big.NewInt(256)) >= 0 {
			st.push(new(big.Int))
		} else {
			r := new(big.Int).Lsh(value, uint(shift.Uint64()))
			st.push(mod256(r))
		}
	case SHR:
		shift, value := st.pop(), st.pop()
		if shift.Cmp(big.NewInt(256)) >= 0 {
			st.push(new(big.Int))
		} else {
			st.push(new(big.Int).Rsh(value, uint(shift.Uint64())))
		}
	case SAR:
		shift := st.pop()
		value := u256ToSigned(st.pop())
		if shift.Cmp(big.NewInt(256)) >= 0 {
			if value.Sign() < 0 {
				st.push(new(big.Int).Set(big2e256m1))
			} else {
				st.push(new(big.Int))
			}
		} else {
			r := new(big.Int).Rsh(value, uint(shift.Uint64()))
			st.push(signedToU256(r))
		}
	default:
		return ErrInvalidOpcode
	}
	return nil
}

func mod256(x *big.Int) *big.Int {
	return new(big.Int).And(x, big2e256m1)
}

func u256ToSigned(x *big.Int) *big.Int {
	if x.Cmp(big2e255) < 0 {
		return new(big.Int).Set(x)
	}
	return new(big.Int).Sub(x, big2e256)
}

func signedToU256(x *big.Int) *big.Int {
	if x.Sign() >= 0 {
		return mod256(x)
	}
	return new(big.Int).Add(x, big2e256)
}

func boolToBig(b bool) *big.Int {
	if b {
		return big.NewInt(1)
	}
	return new(big.Int)
}
