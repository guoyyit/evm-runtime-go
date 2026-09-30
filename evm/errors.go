package evm

import "errors"

var (
	ErrInvalidOpCode       = errors.New("invalid opcode")
	ErrInvalidJump         = errors.New("invalid jump destination")
	ErrStackOverflow       = errors.New("stack overflow")
	ErrStackUnderflow      = errors.New("stack underflow")
	ErrOutOfGas            = errors.New("out of gas")
	ErrCallDepth           = errors.New("call depth exceeded")
	ErrInsufficientBalance = errors.New("insufficient balance for value transfer")
	ErrWriteInReadOnly     = errors.New("write operation in static context")
	ErrExecutionReverted   = errors.New("execution reverted")
	ErrReturnDataOOB       = errors.New("return data out of bounds")
	ErrCodeStoreOOG        = errors.New("out of gas for code storage")
	ErrInvalidMemAccess    = errors.New("invalid memory access")
	ErrUnknownRevision     = errors.New("unknown snapshot revision")
)
