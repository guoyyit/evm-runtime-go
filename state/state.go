package state

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
)

type (
	Hash    [32]byte
	Address [20]byte
)

// Account is a single committed or in-progress account record.
type Account struct {
	Nonce   uint64
	Balance *big.Int
	Code    []byte
	Storage map[Hash]Hash
}

// Log is an emitted contract event.
type Log struct {
	Address Address
	Topics  []Hash
	Data    []byte
	TxIndex uint64
	Index   uint64
}

// Store is the persistent, committed world state.
type Store struct {
	path     string
	mu       sync.RWMutex
	accounts map[Address]*Account
	logs     []Log
	txCount  uint64
}

// State is an isolated, transactional working copy of the world state.
type State struct {
	accounts     map[Address]*Account
	committed    map[Address]*Account
	journal      []journalEntry
	snaps        []int
	selfDestruct map[Address]bool
}

type journalKind int

const (
	journalCreate journalKind = iota
	journalBalance
	journalNonce
	journalCode
	journalStorage
	journalSelfDestruct
)

type journalEntry struct {
	kind    journalKind
	addr    Address
	key     Hash
	balance *big.Int
	nonce   uint64
	code    []byte
	value   Hash
	hadDest bool
}

type diskAccount struct {
	Nonce   uint64          `json:"nonce"`
	Balance string          `json:"balance"`
	Code    []byte          `json:"code,omitempty"`
	Storage map[string]Hash `json:"storage,omitempty"`
}

type diskFile struct {
	Accounts map[Address]diskAccount `json:"accounts"`
	Logs     []Log                   `json:"logs"`
	TxCount  uint64                  `json:"tx_count"`
}

type diskFileJSON struct {
	Accounts map[string]diskAccount `json:"accounts"`
	Logs     []Log                  `json:"logs"`
	TxCount  uint64                 `json:"tx_count"`
}

// MarshalJSON encodes accounts keyed by hex address (JSON forbids array keys).
func (d diskFile) MarshalJSON() ([]byte, error) {
	dj := diskFileJSON{
		Accounts: make(map[string]diskAccount, len(d.Accounts)),
		Logs:     d.Logs,
		TxCount:  d.TxCount,
	}
	for addr, a := range d.Accounts {
		dj.Accounts[hexKey(addr[:])] = a
	}
	return json.Marshal(dj)
}

// UnmarshalJSON decodes the hex-keyed account map.
func (d *diskFile) UnmarshalJSON(raw []byte) error {
	var dj diskFileJSON
	if err := json.Unmarshal(raw, &dj); err != nil {
		return err
	}
	d.Accounts = make(map[Address]diskAccount, len(dj.Accounts))
	d.Logs = dj.Logs
	d.TxCount = dj.TxCount
	for key, a := range dj.Accounts {
		b, err := parseHex(key, 20)
		if err != nil {
			return fmt.Errorf("invalid account key %q: %w", key, err)
		}
		var addr Address
		copy(addr[:], b)
		d.Accounts[addr] = a
	}
	return nil
}

func hexKey(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[2*i] = digits[v>>4]
		out[2*i+1] = digits[v&0xf]
	}
	return string(out)
}

func parseHex(s string, n int) ([]byte, error) {
	if len(s) != n*2 {
		return nil, fmt.Errorf("want %d hex chars, got %d", n*2, len(s))
	}
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		hi, err := hexNibble(s[2*i])
		if err != nil {
			return nil, err
		}
		lo, err := hexNibble(s[2*i+1])
		if err != nil {
			return nil, err
		}
		out[i] = hi<<4 | lo
	}
	return out, nil
}

func hexNibble(c byte) (byte, error) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', nil
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, nil
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, nil
	}
	return 0, fmt.Errorf("invalid hex digit %q", c)
}

func storageToDisk(m map[Hash]Hash) map[string]Hash {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]Hash, len(m))
	for k, v := range m {
		out[hexKey(k[:])] = v
	}
	return out
}

func storageFromDisk(m map[string]Hash) map[Hash]Hash {
	out := make(map[Hash]Hash, len(m))
	for k, v := range m {
		b, err := parseHex(k, 32)
		if err != nil {
			continue
		}
		var key Hash
		copy(key[:], b)
		out[key] = v
	}
	return out
}

func newAccount() *Account {
	return &Account{
		Balance: new(big.Int),
		Storage: make(map[Hash]Hash),
	}
}

// NewStore opens or creates a persistent world state at path.
// An empty path creates an in-memory store.
func NewStore(path string) (*Store, error) {
	s := &Store{
		path:     path,
		accounts: make(map[Address]*Account),
	}
	if path == "" {
		return s, nil
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		var df diskFile
		if err := json.Unmarshal(raw, &df); err != nil {
			return nil, err
		}
		s.txCount = df.TxCount
		s.logs = df.Logs
		for addr, da := range df.Accounts {
			bal, ok := new(big.Int).SetString(da.Balance, 10)
			if !ok {
				return nil, fmt.Errorf("invalid balance for %x: %q", addr, da.Balance)
			}
			storage := storageFromDisk(da.Storage)
			if storage == nil {
				storage = make(map[Hash]Hash)
			}
			s.accounts[addr] = &Account{
				Nonce:   da.Nonce,
				Balance: bal,
				Code:    da.Code,
				Storage: storage,
			}
		}
	case os.IsNotExist(err):
	default:
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	if s.path == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persist()
}

func (s *Store) persist() error {
	df := diskFile{
		Accounts: make(map[Address]diskAccount, len(s.accounts)),
		Logs:     s.logs,
		TxCount:  s.txCount,
	}
	for addr, a := range s.accounts {
		df.Accounts[addr] = diskAccount{
			Nonce:   a.Nonce,
			Balance: a.Balance.String(),
			Code:    a.Code,
			Storage: storageToDisk(a.Storage),
		}
	}
	raw, err := json.MarshalIndent(&df, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// WorkingState returns a fresh, isolated deep copy of the committed state.
func (s *Store) WorkingState() *State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := &State{
		accounts:     make(map[Address]*Account, len(s.accounts)),
		selfDestruct: make(map[Address]bool),
	}
	for addr, a := range s.accounts {
		st.accounts[addr] = cloneAccount(a)
	}
	return st
}

func cloneAccount(a *Account) *Account {
	cp := &Account{
		Nonce:   a.Nonce,
		Balance: new(big.Int).Set(a.Balance),
		Code:    append([]byte(nil), a.Code...),
		Storage: make(map[Hash]Hash, len(a.Storage)),
	}
	for k, v := range a.Storage {
		cp.Storage[k] = v
	}
	return cp
}

// CommittedStorageAt returns a storage slot as it exists in the store before
// the current working state diverged. It is the EIP-2200 "original" basis.
func (s *Store) CommittedStorageAt(addr Address, key Hash) Hash {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if a, ok := s.accounts[addr]; ok {
		return a.Storage[key]
	}
	return Hash{}
}

// Commit atomically replaces the committed state. Logs are appended and
// assigned their global transaction and event indexes.
func (s *Store) Commit(st *State, logs []Log) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	committed := make(map[Address]*Account, len(st.accounts))
	for addr, a := range st.accounts {
		if st.selfDestruct[addr] {
			continue
		}
		committed[addr] = cloneAccount(a)
	}
	for i := range logs {
		logs[i].TxIndex = s.txCount
		logs[i].Index = uint64(i)
	}
	s.accounts = committed
	s.logs = append(s.logs, logs...)
	s.txCount++
	if s.path != "" {
		return s.persist()
	}
	return nil
}

func (s *Store) Balance(addr Address) *big.Int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if a, ok := s.accounts[addr]; ok {
		return new(big.Int).Set(a.Balance)
	}
	return new(big.Int)
}

func (s *Store) Nonce(addr Address) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if a, ok := s.accounts[addr]; ok {
		return a.Nonce
	}
	return 0
}

func (s *Store) Code(addr Address) []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if a, ok := s.accounts[addr]; ok {
		return append([]byte(nil), a.Code...)
	}
	return nil
}

func (s *Store) StorageAt(addr Address, key Hash) Hash {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if a, ok := s.accounts[addr]; ok {
		return a.Storage[key]
	}
	return Hash{}
}

func (s *Store) QueryLogs(addr *Address) []Log {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Log, 0)
	for _, l := range s.logs {
		if addr != nil && l.Address != *addr {
			continue
		}
		out = append(out, Log{
			Address: l.Address,
			Topics:  append([]Hash(nil), l.Topics...),
			Data:    append([]byte(nil), l.Data...),
			TxIndex: l.TxIndex,
			Index:   l.Index,
		})
	}
	return out
}

// Logs returns all committed event logs.
func (s *Store) Logs() []Log {
	return s.QueryLogs(nil)
}

// Serialize returns the canonical JSON bytes of the committed store.
func (s *Store) Serialize() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	df := diskFile{
		Accounts: make(map[Address]diskAccount, len(s.accounts)),
		Logs:     s.logs,
		TxCount:  s.txCount,
	}
	for addr, a := range s.accounts {
		df.Accounts[addr] = diskAccount{
			Nonce:   a.Nonce,
			Balance: a.Balance.String(),
			Code:    a.Code,
			Storage: storageToDisk(a.Storage),
		}
	}
	return json.MarshalIndent(&df, "", "  ")
}

// Replace atomically swaps the committed store for the data produced by
// Serialize and persists it when the store is file-backed.
func (s *Store) Replace(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var df diskFile
	if err := json.Unmarshal(data, &df); err != nil {
		return err
	}
	accounts := make(map[Address]*Account, len(df.Accounts))
	for addr, da := range df.Accounts {
		bal, ok := new(big.Int).SetString(da.Balance, 10)
		if !ok {
			return fmt.Errorf("invalid balance for %x: %q", addr, da.Balance)
		}
		storage := storageFromDisk(da.Storage)
		if storage == nil {
			storage = make(map[Hash]Hash)
		}
		accounts[addr] = &Account{
			Nonce:   da.Nonce,
			Balance: bal,
			Code:    da.Code,
			Storage: storage,
		}
	}
	s.accounts = accounts
	s.logs = df.Logs
	s.txCount = df.TxCount
	if s.path != "" {
		return s.persist()
	}
	return nil
}

func (st *State) account(addr Address) *Account {
	a, ok := st.accounts[addr]
	if !ok {
		a = newAccount()
		st.accounts[addr] = a
	}
	return a
}

// Exists reports whether the account currently exists in the working state.
func (st *State) Exists(addr Address) bool {
	_, ok := st.accounts[addr]
	return ok
}

func (st *State) GetBalance(addr Address) *big.Int {
	if a, ok := st.accounts[addr]; ok {
		return new(big.Int).Set(a.Balance)
	}
	return new(big.Int)
}

func (st *State) AddBalance(addr Address, amount *big.Int) {
	a := st.account(addr)
	st.journal = append(st.journal, journalEntry{
		kind:    journalBalance,
		addr:    addr,
		balance: new(big.Int).Set(a.Balance),
	})
	a.Balance.Add(a.Balance, amount)
}

func (st *State) SubBalance(addr Address, amount *big.Int) {
	a := st.account(addr)
	st.journal = append(st.journal, journalEntry{
		kind:    journalBalance,
		addr:    addr,
		balance: new(big.Int).Set(a.Balance),
	})
	a.Balance.Sub(a.Balance, amount)
}

func (st *State) GetNonce(addr Address) uint64 {
	if a, ok := st.accounts[addr]; ok {
		return a.Nonce
	}
	return 0
}

func (st *State) SetNonce(addr Address, n uint64) {
	a := st.account(addr)
	st.journal = append(st.journal, journalEntry{
		kind:  journalNonce,
		addr:  addr,
		nonce: a.Nonce,
	})
	a.Nonce = n
}

func (st *State) GetCode(addr Address) []byte {
	if a, ok := st.accounts[addr]; ok {
		return a.Code
	}
	return nil
}

func (st *State) SetCode(addr Address, code []byte) {
	a := st.account(addr)
	st.journal = append(st.journal, journalEntry{
		kind: journalCode,
		addr: addr,
		code: append([]byte(nil), a.Code...),
	})
	a.Code = code
}

func (st *State) GetState(addr Address, key Hash) Hash {
	if a, ok := st.accounts[addr]; ok {
		return a.Storage[key]
	}
	return Hash{}
}

func (st *State) SetState(addr Address, key, value Hash) {
	a := st.account(addr)
	st.journal = append(st.journal, journalEntry{
		kind:  journalStorage,
		addr:  addr,
		key:   key,
		value: a.Storage[key],
	})
	a.Storage[key] = value
}

// CreateAccount records the account as newly created in the journal so a
// revert removes it entirely.
func (st *State) CreateAccount(addr Address) {
	if _, ok := st.accounts[addr]; ok {
		return
	}
	st.journal = append(st.journal, journalEntry{kind: journalCreate, addr: addr})
	st.accounts[addr] = newAccount()
}

// MarkSelfDestruct schedules the account for removal at commit.
func (st *State) MarkSelfDestruct(addr Address) {
	st.journal = append(st.journal, journalEntry{
		kind:    journalSelfDestruct,
		addr:    addr,
		hadDest: st.selfDestruct[addr],
	})
	st.selfDestruct[addr] = true
}

// DeleteAccount schedules the account for removal at commit.
func (st *State) DeleteAccount(addr Address) {
	st.MarkSelfDestruct(addr)
}

// Snapshot returns a revision id capturing every change made so far.
func (st *State) Snapshot() int {
	id := len(st.journal)
	st.snaps = append(st.snaps, id)
	return id
}

// Revert restores the working state to revision id, undoing balance, nonce,
// code, storage and account-creation changes in reverse order.
func (st *State) Revert(id int) {
	for i := len(st.journal) - 1; i >= id; i-- {
		e := st.journal[i]
		switch e.kind {
		case journalCreate:
			delete(st.accounts, e.addr)
		case journalBalance:
			st.accounts[e.addr].Balance.Set(e.balance)
		case journalNonce:
			st.accounts[e.addr].Nonce = e.nonce
		case journalCode:
			st.accounts[e.addr].Code = e.code
		case journalStorage:
			st.accounts[e.addr].Storage[e.key] = e.value
		case journalSelfDestruct:
			st.selfDestruct[e.addr] = e.hadDest
		}
	}
	st.journal = st.journal[:id]
	for len(st.snaps) > 0 {
		last := st.snaps[len(st.snaps)-1]
		st.snaps = st.snaps[:len(st.snaps)-1]
		if last == id {
			break
		}
	}
}
