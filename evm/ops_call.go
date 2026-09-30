package evm

import (
	"math/big"

	"github.com/guoyyit/evm-runtime-go/state"
)

// callGas computes the fixed (non-forwarded) gas charge of a CALL-family
// opcode: cold/warm account access, value-transfer premium, new-account
// premium and memory expansion for both input and output regions. The inner
// call gas is forwarded later under the EIP-150 63/64 rule.
func (in *Interpreter) callGas(op OpCode) (uint64, bool) {
	s := in.frame.stack

	toDepth := 1
	valueDepth := 2
	inOffDepth, inSizeDepth := 3, 4
	outOffDepth, outSizeDepth := 5, 6
	hasValue := op == CALL || op == CALLCODE
	if op == STATICCALL || op == DELEGATECALL {
		toDepth = 1
		inOffDepth, inSizeDepth = 2, 3
		outOffDepth, outSizeDepth = 4, 5
	}

	toV, err := s.Peek(toDepth)
	if err != nil {
		return 0, false
	}
	to := bigToAddress(toV)

	overhead := uint64(gasWarmAccess)
	if !in.evm.isWarm(to) {
		overhead = gasBalance
	}

	if hasValue {
		value, err := s.Peek(valueDepth)
		if err != nil {
			return 0, false
		}
		if value.Sign() != 0 {
			overhead += gasCallValue
			if op == CALL && !in.evm.State.Exists(to) {
				overhead += gasNewAccount
			}
		}
	}

	cost, ok := expansionCostFromTop(in, inOffDepth, inSizeDepth)
	if !ok {
		return 0, false
	}
	overhead += cost
	cost, ok = expansionCostFromTop(in, outOffDepth, outSizeDepth)
	if !ok {
		return 0, false
	}
	overhead += cost

	if overhead > in.frame.contract.Gas {
		return 0, false
	}
	in.frame.callOverhead = overhead
	return overhead, true
}

// forwardGas applies the EIP-150 cap and the value-call stipend.
// It reserves the forwarded amount from the caller frame so it cannot be
// spent twice; settleChild restores whatever the child leaves unspent.
func (in *Interpreter) reserveForward(requested uint64, withValue bool) uint64 {
	left := in.frame.contract.Gas
	cap := left - left/64
	gas := requested
	if gas > cap {
		gas = cap
	}
	in.frame.contract.Gas -= gas
	if withValue {
		gas += gasCallStipend
	}
	return gas
}

// reserveCreateGas forwards the full EIP-150-capped allowance to a new
// contract, since CREATE/CREATE2 take no explicit gas operand.
func (in *Interpreter) reserveCreateGas() uint64 {
	left := in.frame.contract.Gas
	cap := left - left/64
	in.frame.contract.Gas -= cap
	return cap
}

// settleChild applies the common post-call rules: revert state on failure,
// expose return/revert data, copy it into caller memory and return unused gas.
func (in *Interpreter) settleChild(snap int, res callResult, outOff, outSize uint64) *big.Int {
	c := in.frame.contract
	data := res.retData
	if res.err != nil {
		in.evm.State.Revert(snap)
		if res.err == ErrExecutionReverted {
			data = res.revertData
		} else {
			data = nil
		}
	}
	in.frame.lastReturn = data
	if outSize != 0 && len(data) > 0 {
		n := outSize
		if uint64(len(data)) < n {
			n = uint64(len(data))
		}
		in.frame.memory.Set(outOff, data[:n])
	}
	// Unused child gas returns to the caller; exceptional failures keep none.
	if res.err == nil || res.err == ErrExecutionReverted {
		c.Gas += res.gasLeft
	}
	if res.err != nil {
		return new(big.Int)
	}
	return big.NewInt(1)
}

func memArg(v *big.Int) (uint64, bool) {
	if v.BitLen() > 64 {
		return 0, false
	}
	return v.Uint64(), true
}

func (in *Interpreter) opCall(op OpCode) (bool, error) {
	c := in.frame.contract
	args, err := in.popN(7)
	if err != nil {
		return false, err
	}
	requested, ok := memArg(args[0])
	if !ok {
		return false, ErrOutOfGas
	}
	to := bigToAddress(args[1])
	value := args[2]
	inOff, ok := memArg(args[3])
	if !ok {
		return false, ErrOutOfGas
	}
	inSize, ok := memArg(args[4])
	if !ok {
		return false, ErrOutOfGas
	}
	outOff, ok := memArg(args[5])
	if !ok {
		return false, ErrOutOfGas
	}
	outSize, ok := memArg(args[6])
	if !ok {
		return false, ErrOutOfGas
	}

	if in.frame.readOnly && value.Sign() != 0 {
		return false, ErrWriteInReadOnly
	}

	forward := in.reserveForward(requested, value.Sign() != 0)
	input := in.frame.memory.Get(inOff, inSize)
	snap := in.evm.State.Snapshot()
	in.evm.warmAcct[to] = struct{}{}

	var res callResult
	if op == CALL {
		res = in.evm.callCore(callOpts{
			caller:   c.Caller,
			to:       to,
			value:    value,
			input:    input,
			gas:      forward,
			readOnly: in.frame.readOnly,
		})
	} else { // CALLCODE
		res = in.evm.callCore(callOpts{
			caller:   c.Caller,
			to:       to,
			value:    value,
			input:    input,
			gas:      forward,
			readOnly: in.frame.readOnly,
			callCode: true,
		})
	}

	success := in.settleChild(snap, res, outOff, outSize)
	return false, in.push(success)
}

func (in *Interpreter) opStaticCall() (bool, error) {
	c := in.frame.contract
	args, err := in.popN(6)
	if err != nil {
		return false, err
	}
	requested, _ := memArg(args[0])
	to := bigToAddress(args[1])
	inOff, _ := memArg(args[2])
	inSize, _ := memArg(args[3])
	outOff, _ := memArg(args[4])
	outSize, _ := memArg(args[5])

	forward := in.reserveForward(requested, false)
	input := in.frame.memory.Get(inOff, inSize)
	snap := in.evm.State.Snapshot()
	in.evm.warmAcct[to] = struct{}{}

	res := in.evm.callCore(callOpts{
		caller:   c.Caller,
		to:       to,
		value:    new(big.Int),
		input:    input,
		gas:      forward,
		readOnly: true,
	})
	success := in.settleChild(snap, res, outOff, outSize)
	return false, in.push(success)
}

func (in *Interpreter) opDelegateCall() (bool, error) {
	c := in.frame.contract
	args, err := in.popN(6)
	if err != nil {
		return false, err
	}
	requested, _ := memArg(args[0])
	to := bigToAddress(args[1])
	inOff, _ := memArg(args[2])
	inSize, _ := memArg(args[3])
	outOff, _ := memArg(args[4])
	outSize, _ := memArg(args[5])

	forward := in.reserveForward(requested, false)
	input := in.frame.memory.Get(inOff, inSize)
	snap := in.evm.State.Snapshot()
	in.evm.warmAcct[to] = struct{}{}

	res := in.evm.callCore(callOpts{
		caller:   c.Caller,
		to:       to,
		value:    c.Value,
		input:    input,
		gas:      forward,
		readOnly: in.frame.readOnly,
		delegate: true,
	})
	success := in.settleChild(snap, res, outOff, outSize)
	return false, in.push(success)
}

func (in *Interpreter) opCreate(create2 bool) (bool, error) {
	c := in.frame.contract
	if in.frame.readOnly {
		return false, ErrWriteInReadOnly
	}
	n := 3
	if create2 {
		n = 4
	}
	args, err := in.popN(n)
	if err != nil {
		return false, err
	}
	// CREATE stack (top->bottom): offset, size, value.
	inOff, ok := memArg(args[0])
	if !ok {
		return false, ErrOutOfGas
	}
	inSize, ok := memArg(args[1])
	if !ok {
		return false, ErrOutOfGas
	}
	value := args[2]
	var salt *state.Hash
	if create2 {
		// CREATE2 stack (top->bottom): salt, size, offset, value.
		h := bigToHash(trim256(args[0]))
		salt = &h
		value = args[3]
		inOff, ok = memArg(args[2])
		if !ok {
			return false, ErrOutOfGas
		}
		inSize, ok = memArg(args[1])
		if !ok {
			return false, ErrOutOfGas
		}
	}

	initCode := in.frame.memory.Get(inOff, inSize)

	forward := in.reserveCreateGas()
	snap := in.evm.State.Snapshot()
	addr, _, _, createErr := in.evm.Create(c.Self, value, initCode, forward, salt)
	if createErr != nil {
		in.evm.State.Revert(snap)
		return false, in.push(new(big.Int))
	}
	return false, in.push(addressBig(addr))
}
