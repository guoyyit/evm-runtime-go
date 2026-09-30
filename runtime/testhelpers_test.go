package runtime

import (
	"math/big"

	"github.com/guoyyit/evm-runtime-go/common"
	"github.com/guoyyit/evm-runtime-go/state"
)

// setBalanceForTest directly credits an account through the committed store,
// simulating a pre-funded genesis account.
func (r *Runtime) setBalanceForTest(addr common.Address, amount *big.Int) error {
	db, err := state.NewStateDB(r.store)
	if err != nil {
		return err
	}
	db.AddBalance(addr, amount)
	_, err = db.Commit()
	return err
}

var _ = big.NewInt
