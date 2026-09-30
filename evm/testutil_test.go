package evm

import "math/big"

func bigWei(s string) *big.Int {
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic("bad wei: " + s)
	}
	return v
}
