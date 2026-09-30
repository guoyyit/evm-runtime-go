package state

import (
	"math/big"

	"github.com/guoyyit/evm-runtime-go/common"
)

// journalEntry is a single reversible mutation.
type journalEntry interface {
	revert(s *StateDB)
}

// journal records reversible mutations plus snapshot boundaries.
type journal struct {
	entries   []journalEntry
	snapshots []int
}

func newJournal() *journal {
	return &journal{}
}

func (j *journal) append(entry journalEntry) {
	j.entries = append(j.entries, entry)
}

func (j *journal) snapshot() int {
	id := len(j.snapshots)
	j.snapshots = append(j.snapshots, len(j.entries))
	return id
}

type balanceChange struct {
	account common.Address
	prev    string
	existed bool
}

func (ch balanceChange) revert(s *StateDB) {
	acc := s.accounts[ch.account]
	if acc != nil {
		acc.Balance, _ = new(big.Int).SetString(ch.prev, 10)
		if acc.Balance == nil {
			acc.Balance = new(big.Int)
		}
	}
}

type nonceChange struct {
	account common.Address
	prev    uint64
	existed bool
}

func (ch nonceChange) revert(s *StateDB) {
	if acc := s.accounts[ch.account]; acc != nil {
		acc.Nonce = ch.prev
	}
}

type codeChange struct {
	account  common.Address
	prevCode []byte
	existed  bool
}

func (ch codeChange) revert(s *StateDB) {
	acc := s.accounts[ch.account]
	if !ch.existed {
		delete(s.code, ch.account)
		if acc != nil {
			acc.CodeHash = common.Hash{}
		}
		return
	}
	s.code[ch.account] = ch.prevCode
	if acc != nil {
		acc.CodeHash = common.Keccak256Hash(ch.prevCode)
	}
}

type storageChange struct {
	account common.Address
	key     common.Hash
	prev    common.Hash
	exists  bool
}

func (ch storageChange) revert(s *StateDB) {
	slots := s.storage[ch.account]
	if slots == nil {
		return
	}
	if !ch.exists {
		delete(slots, ch.key)
		return
	}
	slots[ch.key] = ch.prev
}

type createObjectChange struct {
	account common.Address
}

func (ch createObjectChange) revert(s *StateDB) {
	delete(s.accounts, ch.account)
	delete(s.storage, ch.account)
	delete(s.code, ch.account)
}

type suicideChange struct {
	account common.Address
}

func (ch suicideChange) revert(s *StateDB) {
	delete(s.suicides, ch.account)
}

type accessListChange struct {
	account common.Address
	slot    *common.Hash
}

func (ch accessListChange) revert(s *StateDB) {
	if ch.slot == nil {
		delete(s.accessList, ch.account)
		return
	}
	if slots, ok := s.accessList[ch.account]; ok {
		delete(slots, *ch.slot)
	}
}

type addLogChange struct{}

func (ch addLogChange) revert(s *StateDB) {
	if len(s.logs) > 0 {
		s.logs = s.logs[:len(s.logs)-1]
	}
}
