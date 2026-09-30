package evm

import "math/big"

// StackLimit is the maximum EVM operand-stack depth.
const StackLimit = 1024

// Stack is the EVM operand stack.
type Stack struct {
	data []*big.Int
}

func newstack() *Stack { return &Stack{} }

func (st *Stack) push(d *big.Int) {
	if len(st.data) >= StackLimit {
		panic(ErrStackOverflow)
	}
	st.data = append(st.data, d)
}

func (st *Stack) pop() *big.Int {
	n := len(st.data)
	if n == 0 {
		panic(ErrStackUnderflow)
	}
	v := st.data[n-1]
	st.data = st.data[:n-1]
	return v
}

func (st *Stack) len() int { return len(st.data) }

func (st *Stack) peek(n int) *big.Int { return st.data[len(st.data)-n] }

func (st *Stack) dup(n int) {
	st.push(new(big.Int).Set(st.peek(n)))
}

func (st *Stack) swap(n int) {
	st.data[len(st.data)-n], st.data[len(st.data)-1] =
		st.data[len(st.data)-1], st.data[len(st.data)-n]
}

func (st *Stack) back(n int) *big.Int { return st.data[len(st.data)-1-n] }

// require checks pop/push room before executing an instruction.
func (st *Stack) require(pops, pushes int) error {
	if len(st.data) < pops {
		return ErrStackUnderflow
	}
	if pushes-pops > 0 && len(st.data)-pops+pushes > StackLimit {
		return ErrStackOverflow
	}
	return nil
}
