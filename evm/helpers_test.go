package evm

import (
	"testing"

	"github.com/guoyyit/evm-runtime-go/state"
)

// deploy creates a contract whose runtime bytecode is exactly code (the
// constructor just returns code inline).
func deploy(t *testing.T, r *Runtime, from state.Address, code []byte) state.Address {
	t.Helper()
	init := buildInitCode(code)
	res, err := r.Execute(Message{From: from, Data: init, GasLimit: 5_000_000})
	if err != nil {
		t.Fatalf("deploy error: %v", err)
	}
	if res.Err != nil {
		t.Fatalf("deploy evm error: %v used=%d", res.Err, res.GasUsed)
	}
	return *res.CreatedAddress
}

func deployFail(t *testing.T, r *Runtime, from state.Address, initCode []byte) state.Address {
	t.Helper()
	res, err := r.Execute(Message{From: from, Data: initCode, GasLimit: 5_000_000})
	if err != nil {
		t.Fatal(err)
	}
	if res.CreatedAddress == nil {
		t.Fatal("expected address even on failed create")
	}
	return *res.CreatedAddress
}

// buildInitCode wraps runtime code as an in-memory constructor:
//
//	PUSH<n> runtime
//	PUSH1 0
//	MSTORE
//	PUSH1 len
//	PUSH1 (32-len)
//	RETURN
func buildInitCode(code []byte) []byte {
	n := len(code)
	if n == 0 || n > 32 {
		// Fall back to CODECOPY-style constructor for longer runtime code.
		return buildCopyInit(code)
	}
	// Right-align the code in a 32-byte word, then return it.
	word := make([]byte, 32)
	copy(word[32-n:], code)
	out := []byte{byte(PUSH1) - 1 + byte(32)}
	out = append(out, word...)
	out = append(out,
		byte(PUSH1), 0x00, byte(MSTORE),
		byte(PUSH1), byte(n),
		byte(PUSH1), byte(32-n),
		byte(RETURN),
	)
	return out
}

// buildCopyInit creates runtime code of arbitrary length (< 256 bytes) by
// copying the constructor's own deployed code region via CODECOPY.
func buildCopyInit(code []byte) []byte {
	n := len(code)
	// PUSH1 n, PUSH1 <offset of code>, PUSH1 0, CODECOPY
	// PUSH1 n, PUSH1 0, RETURN ; then runtime code
	// offsets: instructions occupy 14 bytes before the runtime payload.
	prefix := []byte{
		byte(PUSH1), byte(n), // length
		byte(PUSH1), 0x0c, // CODECOPY source offset (prefix is 12 bytes)
		byte(PUSH1), 0x00, // dest 0
		byte(CODECOPY),
		byte(PUSH1), byte(n),
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	return append(prefix, code...)
}
