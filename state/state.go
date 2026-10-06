package state

import (
	"fmt"
	"math/big"

	"github.com/guoyyit/evm-runtime-go/common"
)

// StateDB is the mutable execution-time world state. Pending changes are not
// visible to other StateDB instances and are only durably committed through
// the backing Store.
type StateDB struct {
	store    Store
	revision int
	journal  *journal
	valid    bool

	accounts      map[common.Address]*Account
	storage       map[common.Address]map[common.Hash]common.Hash
	code          map[common.Address][]byte
	originStorage map[common.Address]map[common.Hash]common.Hash

	// baseAccounts captures nonce/balance as loaded from the store, so a
	// commit can express balance and nonce changes as signed deltas against
	// the base revision. Deltas merge additively under concurrency.
	baseAccounts map[common.Address]baseAccount
	codeDirty    map[common.Address]bool

	accessList map[common.Address]map[common.Hash]struct{}
	logs       []Log

	suicides map[common.Address]bool

	touchedAccounts map[common.Address]struct{}
	touchedSlots    map[common.Address]map[common.Hash]struct{}
}

// baseAccount is the committed nonce/balance of an account at load time.
type baseAccount struct {
	nonce   uint64
	balance *big.Int
}

// Log is an emitted contract event.
type Log struct {
	Address common.Address
	Topics  []common.Hash
	Data    []byte
}

// NewStateDB builds an execution state on top of a persistent store.
func NewStateDB(store Store) (*StateDB, error) {
	loaded, err := store.Load()
	if err != nil {
		return nil, err
	}
	s := &StateDB{
		store:           store,
		revision:        loaded.Revision,
		journal:         newJournal(),
		valid:           true,
		accounts:        make(map[common.Address]*Account),
		storage:         make(map[common.Address]map[common.Hash]common.Hash),
		code:            make(map[common.Address][]byte),
		originStorage:   make(map[common.Address]map[common.Hash]common.Hash),
		baseAccounts:    make(map[common.Address]baseAccount),
		codeDirty:       make(map[common.Address]bool),
		accessList:      make(map[common.Address]map[common.Hash]struct{}),
		suicides:        make(map[common.Address]bool),
		touchedAccounts: make(map[common.Address]struct{}),
		touchedSlots:    make(map[common.Address]map[common.Hash]struct{}),
	}
	for addr, pa := range loaded.Accounts {
		bal, ok := new(big.Int).SetString(pa.Balance, 10)
		if !ok {
			return nil, fmt.Errorf("state: invalid balance for %s: %q", addr.Hex(), pa.Balance)
		}
		s.accounts[addr] = &Account{Nonce: pa.Nonce, Balance: bal, CodeHash: pa.CodeHash}
		s.baseAccounts[addr] = baseAccount{nonce: pa.Nonce, balance: new(big.Int).Set(bal)}
		if len(pa.Code) > 0 {
			s.code[addr] = pa.Code
		}
		slots := make(map[common.Hash]common.Hash, len(pa.Storage))
		for k, v := range pa.Storage {
			slots[common.HexToHash(k)] = common.HexToHash(v)
		}
		s.storage[addr] = slots
		origin := make(map[common.Hash]common.Hash, len(slots))
		for k, v := range slots {
			origin[k] = v
		}
		s.originStorage[addr] = origin
	}
	return s, nil
}

// Snapshot captures a transactional rollback point.
func (s *StateDB) Snapshot() int { return s.journal.snapshot() }

// Revert restores every state change made after the given snapshot.
func (s *StateDB) Revert(id int) {
	target := s.journal.snapshots[id]
	reverted := s.journal.entries[target:]
	for i := len(reverted) - 1; i >= 0; i-- {
		reverted[i].revert(s)
	}
	s.journal.entries = s.journal.entries[:target]
	s.journal.snapshots = s.journal.snapshots[:id]
}

// Commit atomically persists pending changes and returns the new logs.
// Balance and nonce changes are expressed as signed deltas against the
// base revision this state was loaded from, so concurrent executions
// writing the same account merge additively instead of losing updates.
func (s *StateDB) Commit() ([]Log, error) {
	if !s.valid {
		return nil, nil
	}
	out := make(map[common.Address]*AccountDelta, len(s.touchedAccounts))
	for addr := range s.touchedAccounts {
		acc := s.accounts[addr]
		if s.suicides[addr] {
			continue
		}
		base := s.baseAccounts[addr]
		baseBalance := base.balance
		if baseBalance == nil {
			baseBalance = new(big.Int)
		}
		d := &AccountDelta{
			BalanceDelta: new(big.Int).Sub(acc.Balance, baseBalance),
			NonceDelta:   int64(acc.Nonce) - int64(base.nonce),
		}
		if s.codeDirty[addr] {
			d.CodeSet = true
			d.Code = s.code[addr]
			d.CodeHash = acc.CodeHash
		}
		if touched := s.touchedSlots[addr]; len(touched) > 0 {
			d.Storage = make(map[string]string, len(touched))
			for k := range touched {
				v := s.storage[addr][k]
				// A zero value explicitly clears the committed slot; it is
				// represented as the zero hash and deleted on merge.
				d.Storage[k.Hex()] = v.Hex()
			}
		}
		out[addr] = d
	}
	logs := append([]Log(nil), s.logs...)
	suicides := make(map[common.Address]struct{}, len(s.suicides))
	for addr, yes := range s.suicides {
		if yes {
			suicides[addr] = struct{}{}
		}
	}
	if _, err := s.store.Commit(out, suicides, s.logs); err != nil {
		return nil, err
	}
	s.valid = false
	return logs, nil
}

// Logs returns the logs emitted so far in this execution state.
func (s *StateDB) Logs() []Log { return append([]Log(nil), s.logs...) }

func (s *StateDB) getOrCreate(addr common.Address) *Account {
	if acc, ok := s.accounts[addr]; ok {
		return acc
	}
	// Journal the implicit creation so a revert restores non-existence;
	// otherwise a zombie zero account would leak past the rollback point
	// and change Exist-dependent gas metering on re-execution.
	s.journal.append(createObjectChange{account: addr})
	acc := &Account{Balance: new(big.Int)}
	s.accounts[addr] = acc
	if s.storage[addr] == nil {
		s.storage[addr] = map[common.Hash]common.Hash{}
	}
	return acc
}

func (s *StateDB) touchAccount(addr common.Address) {
	if _, ok := s.touchedAccounts[addr]; ok {
		return
	}
	s.journal.append(touchAccountChange{account: addr})
	s.touchedAccounts[addr] = struct{}{}
}

func (s *StateDB) touchSlot(addr common.Address, key common.Hash) {
	slots := s.touchedSlots[addr]
	if slots == nil {
		slots = make(map[common.Hash]struct{})
		s.touchedSlots[addr] = slots
	}
	if _, ok := slots[key]; ok {
		return
	}
	s.journal.append(touchSlotChange{account: addr, key: key})
	slots[key] = struct{}{}
}

// Account / balance / nonce accessors are intentionally empty in the skeleton.
func (s *StateDB) GetBalance(addr common.Address) *big.Int {
	if acc, ok := s.accounts[addr]; ok {
		return new(big.Int).Set(acc.Balance)
	}
	return new(big.Int)
}

func (s *StateDB) AddBalance(addr common.Address, amount *big.Int) {
	acc := s.getOrCreate(addr)
	s.touchAccount(addr)
	s.journal.append(balanceChange{account: addr, prev: acc.Balance.String()})
	acc.Balance = new(big.Int).Add(acc.Balance, amount)
}

func (s *StateDB) SubBalance(addr common.Address, amount *big.Int) bool {
	acc := s.getOrCreate(addr)
	if acc.Balance.Cmp(amount) < 0 {
		return false
	}
	s.touchAccount(addr)
	s.journal.append(balanceChange{account: addr, prev: acc.Balance.String()})
	acc.Balance = new(big.Int).Sub(acc.Balance, amount)
	return true
}

func (s *StateDB) GetNonce(addr common.Address) uint64 {
	if acc, ok := s.accounts[addr]; ok {
		return acc.Nonce
	}
	return 0
}

func (s *StateDB) SetNonce(addr common.Address, nonce uint64) {
	acc := s.getOrCreate(addr)
	s.touchAccount(addr)
	s.journal.append(nonceChange{account: addr, prev: acc.Nonce, existed: true})
	acc.Nonce = nonce
}

func (s *StateDB) GetCode(addr common.Address) []byte {
	return s.code[addr]
}

func (s *StateDB) SetCode(addr common.Address, code []byte) {
	prev, existed := s.code[addr]
	s.touchAccount(addr)
	s.journal.append(codeChange{account: addr, prevCode: prev, existed: existed})
	acc := s.getOrCreate(addr)
	s.journal.append(codeDirtyChange{account: addr, prev: s.codeDirty[addr]})
	s.codeDirty[addr] = true
	if len(code) == 0 {
		delete(s.code, addr)
		acc.CodeHash = common.Hash{}
		return
	}
	h := common.Keccak256Hash(code)
	s.code[addr] = code
	acc.CodeHash = h
}

func (s *StateDB) GetCodeHash(addr common.Address) common.Hash {
	if acc, ok := s.accounts[addr]; ok {
		return acc.CodeHash
	}
	return common.Hash{}
}

func (s *StateDB) GetCodeSize(addr common.Address) int {
	return len(s.code[addr])
}

func (s *StateDB) GetState(addr common.Address, key common.Hash) common.Hash {
	if slots, ok := s.storage[addr]; ok {
		return slots[key]
	}
	return common.Hash{}
}

func (s *StateDB) SetState(addr common.Address, key, value common.Hash) {
	s.getOrCreate(addr)
	slots := s.storage[addr]
	prev, existed := slots[key]
	s.touchAccount(addr)
	s.touchSlot(addr, key)
	s.journal.append(storageChange{account: addr, key: key, prev: prev, exists: existed})
	slots[key] = value
}

func (s *StateDB) GetCommittedState(addr common.Address, key common.Hash) common.Hash {
	if slots, ok := s.originStorage[addr]; ok {
		return slots[key]
	}
	return common.Hash{}
}

func (s *StateDB) AddLog(log Log) {
	s.journal.append(addLogChange{})
	s.logs = append(s.logs, log)
}

func (s *StateDB) Exist(addr common.Address) bool {
	_, ok := s.accounts[addr]
	return ok
}

func (s *StateDB) Empty(addr common.Address) bool {
	acc, ok := s.accounts[addr]
	if !ok {
		return true
	}
	return acc.Nonce == 0 && acc.Balance.Sign() == 0 && acc.CodeHash == (common.Hash{})
}

func (s *StateDB) CreateAccount(addr common.Address) {
	// getOrCreate journals the creation, keeping rollback complete.
	s.getOrCreate(addr)
}

func (s *StateDB) Suicide(addr common.Address) bool {
	if !s.Exist(addr) {
		return false
	}
	if !s.suicides[addr] {
		s.journal.append(suicideChange{account: addr})
		s.suicides[addr] = true
	}
	return true
}

func (s *StateDB) HasSuicided(addr common.Address) bool { return s.suicides[addr] }

func (s *StateDB) AddAddressToAccessList(addr common.Address) {
	if _, ok := s.accessList[addr]; ok {
		return
	}
	s.journal.append(accessListChange{account: addr})
	s.accessList[addr] = map[common.Hash]struct{}{}
}

func (s *StateDB) AddSlotToAccessList(addr common.Address, slot common.Hash) {
	slots, ok := s.accessList[addr]
	if !ok {
		s.journal.append(accessListChange{account: addr})
		slots = map[common.Hash]struct{}{}
		s.accessList[addr] = slots
	}
	if _, ok := slots[slot]; ok {
		return
	}
	s.journal.append(accessListChange{account: addr, slot: &slot})
	slots[slot] = struct{}{}
}

func (s *StateDB) IsAddressWarm(addr common.Address) bool {
	_, ok := s.accessList[addr]
	return ok
}

func (s *StateDB) IsSlotWarm(addr common.Address, slot common.Hash) bool {
	slots, ok := s.accessList[addr]
	if !ok {
		return false
	}
	_, ok = slots[slot]
	return ok
}
