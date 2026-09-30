package runtime

import (
	"math/big"

	"github.com/guoyyit/evm-runtime-go/common"
	"github.com/guoyyit/evm-runtime-go/state"
)

// StateView exposes committed account and storage data.
type StateView struct {
	store *state.FileStore
}

// Snapshot/restore and committed-state queries are placeholders.
// Balance returns the committed balance of addr.
func (r *Runtime) Balance(addr common.Address) (*big.Int, error) {
	data, err := r.store.Load()
	if err != nil {
		return nil, err
	}
	if acc, ok := data.Accounts[addr]; ok {
		bal, ok := new(big.Int).SetString(acc.Balance, 10)
		if !ok {
			return nil, nil
		}
		return bal, nil
	}
	return new(big.Int), nil
}

// Nonce returns the committed nonce of addr.
func (r *Runtime) Nonce(addr common.Address) (uint64, error) {
	data, err := r.store.Load()
	if err != nil {
		return 0, err
	}
	if acc, ok := data.Accounts[addr]; ok {
		return acc.Nonce, nil
	}
	return 0, nil
}

// Code returns the committed deployed bytecode of addr.
func (r *Runtime) Code(addr common.Address) ([]byte, error) {
	data, err := r.store.Load()
	if err != nil {
		return nil, err
	}
	if acc, ok := data.Accounts[addr]; ok {
		return acc.Code, nil
	}
	return nil, nil
}

func (r *Runtime) StorageAt(addr common.Address, key common.Hash) (common.Hash, error) {
	data, err := r.store.Load()
	if err != nil {
		return common.Hash{}, err
	}
	if acc, ok := data.Accounts[addr]; ok && acc.Storage != nil {
		if v, ok := acc.Storage[key.Hex()]; ok {
			return common.HexToHash(v), nil
		}
	}
	return common.Hash{}, nil
}

// Logs returns every event committed so far.
func (r *Runtime) Logs() ([]state.Log, error) { return r.store.GetLogs() }

var _ = StateView{}
