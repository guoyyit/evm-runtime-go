package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"

	"github.com/guoyyit/evm-runtime-go/common"
)

// PersistedAccount is the on-disk representation of an account.
type PersistedAccount struct {
	Nonce    uint64            `json:"nonce"`
	Balance  string            `json:"balance"`
	CodeHash common.Hash       `json:"codeHash"`
	Storage  map[string]string `json:"storage,omitempty"`
	Code     []byte            `json:"code,omitempty"`
}

// AccountDelta is one account's committed change relative to the base
// revision the executing StateDB was loaded from. Balance and nonce are
// signed deltas so that concurrent executions writing the same account
// merge additively instead of overwriting each other; storage slots and
// code are absolute values merged per key (last committer wins per key).
type AccountDelta struct {
	BalanceDelta *big.Int
	NonceDelta   int64
	CodeSet      bool
	CodeHash     common.Hash
	Code         []byte
	Storage      map[string]string
}

// persistedData is the serialized world-state document.
type persistedData struct {
	Revision int                                 `json:"revision"`
	Accounts map[common.Address]PersistedAccount `json:"accounts"`
	Logs     []Log                               `json:"logs"`
}

// zeroHashHex is the on-disk tombstone for a cleared storage slot.
const zeroHashHex = "0x0000000000000000000000000000000000000000000000000000000000000000"

// Store provides durable, isolated world-state persistence.
type Store interface {
	Load() (*persistedData, error)
	Commit(deltas map[common.Address]*AccountDelta, suicides map[common.Address]struct{}, logs []Log) (int, error)
	Revision() int
	GetLogs() ([]Log, error)
	Close() error
}

// FileStore persists the world state to a single JSON document on local disk.
type FileStore struct {
	mu   sync.Mutex
	path string
	data persistedData
}

// NewFileStore opens (creating if needed) a JSON file-backed store.
func NewFileStore(path string) (*FileStore, error) {
	f := &FileStore{path: path}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return nil, fmt.Errorf("state: create state directory: %w", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			f.data = persistedData{Accounts: map[common.Address]PersistedAccount{}}
			return f, nil
		}
		return nil, fmt.Errorf("state: read state file: %w", err)
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &f.data); err != nil {
			return nil, fmt.Errorf("state: decode state file: %w", err)
		}
	}
	if f.data.Accounts == nil {
		f.data.Accounts = map[common.Address]PersistedAccount{}
	}
	return f, nil
}

func (f *FileStore) Load() (*persistedData, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data := f.data
	if data.Accounts == nil {
		data.Accounts = map[common.Address]PersistedAccount{}
	}
	return &data, nil
}

// ErrBalanceUnderflow is returned when merging a commit would drive an
// account balance below zero: the concurrent executions overdraft the
// account relative to one another, so this commit is rejected (the caller
// may re-execute on the newer base and retry). No state is changed.
var ErrBalanceUnderflow = errors.New("state: concurrent commit would drive balance negative")

// ErrNonceUnderflow is returned when merging a commit would drive an
// account nonce below zero.
var ErrNonceUnderflow = errors.New("state: concurrent commit would drive nonce negative")

// Commit merges one execution's account deltas into the latest committed
// state. Balance and nonce deltas apply additively so concurrent commits
// to the same account cannot lose updates; storage slots merge per key and
// code replaces only when the execution actually set it. The merge and the
// durable write happen atomically under the store lock.
func (f *FileStore) Commit(deltas map[common.Address]*AccountDelta, suicides map[common.Address]struct{}, logs []Log) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	newRevision := f.data.Revision + 1

	next := persistedData{
		Revision: newRevision,
		Accounts: make(map[common.Address]PersistedAccount, len(f.data.Accounts)+len(deltas)),
		Logs:     append(append([]Log(nil), f.data.Logs...), logs...),
	}
	for addr, acc := range f.data.Accounts {
		copied := acc
		copied.Storage = copyStorage(acc.Storage)
		next.Accounts[addr] = copied
	}
	for addr, d := range deltas {
		merged := next.Accounts[addr]
		merged.Storage = copyStorage(merged.Storage)

		balance := new(big.Int)
		if merged.Balance != "" {
			if _, ok := balance.SetString(merged.Balance, 10); !ok {
				return f.data.Revision, fmt.Errorf("state: corrupt committed balance for %s", addr.Hex())
			}
		}
		if d.BalanceDelta != nil {
			balance.Add(balance, d.BalanceDelta)
		}
		if balance.Sign() < 0 {
			return f.data.Revision, fmt.Errorf("%w: %s", ErrBalanceUnderflow, addr.Hex())
		}
		merged.Balance = balance.String()

		nonce := new(big.Int).Add(new(big.Int).SetUint64(merged.Nonce), big.NewInt(d.NonceDelta))
		if nonce.Sign() < 0 || !nonce.IsUint64() {
			return f.data.Revision, fmt.Errorf("%w: %s", ErrNonceUnderflow, addr.Hex())
		}
		merged.Nonce = nonce.Uint64()

		if d.CodeSet {
			merged.Code = d.Code
			merged.CodeHash = d.CodeHash
		}
		for k, v := range d.Storage {
			if merged.Storage == nil {
				merged.Storage = make(map[string]string)
			}
			if v == zeroHashHex {
				delete(merged.Storage, k)
				continue
			}
			merged.Storage[k] = v
		}
		if len(merged.Storage) == 0 {
			merged.Storage = nil
		}
		next.Accounts[addr] = merged
	}
	for addr := range suicides {
		delete(next.Accounts, addr)
	}

	encoded, err := json.MarshalIndent(&next, "", "  ")
	if err != nil {
		return f.data.Revision, fmt.Errorf("state: encode state: %w", err)
	}
	dir := filepath.Dir(f.path)
	tmp, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return f.data.Revision, fmt.Errorf("state: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(encoded); err != nil {
		tmp.Close()
		return f.data.Revision, fmt.Errorf("state: write state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return f.data.Revision, fmt.Errorf("state: fsync state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return f.data.Revision, fmt.Errorf("state: close state: %w", err)
	}
	if err := os.Rename(tmpName, f.path); err != nil {
		return f.data.Revision, fmt.Errorf("state: atomic rename: %w", err)
	}
	f.data = next
	return newRevision, nil
}

func (f *FileStore) Revision() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.data.Revision
}

func (f *FileStore) GetLogs() ([]Log, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Log(nil), f.data.Logs...), nil
}

func copyStorage(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (f *FileStore) Close() error { return nil }

var _ Store = (*FileStore)(nil)
