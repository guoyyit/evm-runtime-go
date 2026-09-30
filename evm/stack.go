package evm

import "math/big"

// Stack is the 1024-element 256-bit operand stack of one call frame.
// Values are always unsigned canonical 256-bit big.Ints (0 <= v < 2^256).
type Stack struct {
	items []*big.Int
}

const maxStackSize = 1024

// NewStack returns an empty operand stack.
func NewStack() *Stack {
	return &Stack{items: make([]*big.Int, 0, maxStackSize)}
}

// Push appends v. It returns ErrStackOverflow when the stack already holds
// 1024 items; ownership of v is transferred to the stack.
func (st *Stack) Push(v *big.Int) error {
	if len(st.items) >= maxStackSize {
		return ErrStackOverflow
	}
	st.items = append(st.items, v)
	return nil
}

// Pop removes and returns the top value, or ErrStackUnderflow.
func (st *Stack) Pop() (*big.Int, error) {
	n := len(st.items)
	if n == 0 {
		return nil, ErrStackUnderflow
	}
	v := st.items[n-1]
	st.items = st.items[:n-1]
	return v, nil
}

// Peek returns the n-th element from the top (0 = top) without removing it.
func (st *Stack) Peek(n int) (*big.Int, error) {
	if n < 0 || n >= len(st.items) {
		return nil, ErrStackUnderflow
	}
	return st.items[len(st.items)-1-n], nil
}

// Len returns the current stack height.
func (st *Stack) Len() int { return len(st.items) }

// Swap exchanges the top element with the element n positions below it
// (Swap1: top and second). Returns ErrStackUnderflow when too shallow.
func (st *Stack) Swap(n int) error {
	if len(st.items) <= n {
		return ErrStackUnderflow
	}
	i := len(st.items) - 1
	st.items[i], st.items[i-n] = st.items[i-n], st.items[i]
	return nil
}

// Dup copies the n-th element from the top (Dup1: top) and pushes it.
func (st *Stack) Dup(n int) error {
	if len(st.items) < n {
		return ErrStackUnderflow
	}
	if len(st.items) >= maxStackSize {
		return ErrStackOverflow
	}
	v := new(big.Int).Set(st.items[len(st.items)-n])
	st.items = append(st.items, v)
	return nil
}
