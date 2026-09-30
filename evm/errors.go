package evm

import (
	"errors"
	"fmt"
)

// Execution errors carry distinct, machine-readable failure causes.
var (
	ErrInvalidOpcode        = errors.New("evm: invalid opcode")
	ErrInvalidJump          = errors.New("evm: invalid jump destination")
	ErrStackOverflow        = errors.New("evm: stack overflow")
	ErrStackUnderflow       = errors.New("evm: stack underflow")
	ErrOutOfGas             = errors.New("evm: out of gas")
	ErrDepthLimitExceeded   = errors.New("evm: call depth limit exceeded")
	ErrInsufficientBalance  = errors.New("evm: insufficient balance for value transfer")
	ErrWriteInReadOnly      = errors.New("evm: write operation in read-only context")
	ErrReturnDataOutOfBound = errors.New("evm: return data out of bounds")
	ErrExecutionReverted    = errors.New("evm: execution reverted")
	ErrCreateCollision      = errors.New("evm: contract address collision")
	ErrInvalidCode          = errors.New("evm: invalid contract code")
	ErrGasUintOverflow      = errors.New("evm: gas uint64 overflow")
)

// Error wraps an EVM failure with execution context.
type Error struct {
	Err        error
	GasUsed    uint64
	PC         uint64
	Op         OpCode
	RevertData []byte
}

func (e *Error) Error() string {
	if e.Err == nil {
		return "evm: unknown error"
	}
	if e.Op != 0 {
		return fmt.Sprintf("%s (pc=%d op=%s gasUsed=%d)", e.Err.Error(), e.PC, e.Op, e.GasUsed)
	}
	return e.Err.Error()
}
func (e *Error) Unwrap() error { return e.Err }

// IsRevert reports whether err is a contract-level REVERT (vs. an EVM fault).
func IsRevert(err error) bool {
	if err == nil {
		return false
	}
	var rev *revertError
	if errors.As(err, &rev) {
		return true
	}
	var evmErr *Error
	if errors.As(err, &evmErr) {
		return errors.Is(evmErr.Err, ErrExecutionReverted)
	}
	return false
}

// RevertData extracts the revert payload, if any.
func RevertData(err error) []byte {
	var rev *revertError
	if errors.As(err, &rev) {
		return rev.data
	}
	var evmErr *Error
	if errors.As(err, &evmErr) && evmErr.RevertData != nil {
		return evmErr.RevertData
	}
	return nil
}
