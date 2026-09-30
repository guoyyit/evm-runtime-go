package state

import (
	"math/big"

	"github.com/guoyyit/evm-runtime-go/common"
)

// Account holds the persistent fields of one Ethereum-like account.
type Account struct {
	Nonce    uint64
	Balance  *big.Int
	CodeHash common.Hash
}

// IsContract reports whether the account stores deployed bytecode.
func (a *Account) IsContract() bool {
	return a.CodeHash != common.Hash{}
}
