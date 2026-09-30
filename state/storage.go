package state

import (
	"encoding/json"
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
	Commit(accounts map[common.Address]*PersistedAccount, suicides map[common.Address]struct{}, logs []Log) (int, error)
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

func (f *FileStore) Commit(accounts map[common.Address]*PersistedAccount, suicides map[common.Address]struct{}, logs []Log) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	newRevision := f.data.Revision + 1

	next := persistedData{
		Revision: newRevision,
		Accounts: make(map[common.Address]PersistedAccount, len(f.data.Accounts)+len(accounts)),
		Logs:     append(append([]Log(nil), f.data.Logs...), logs...),
	}
	for addr, acc := range f.data.Accounts {
		copied := acc
		copied.Storage = copyStorage(acc.Storage)
		next.Accounts[addr] = copied
	}
	for addr, acc := range accounts {
		current := next.Accounts[addr]
		merged := *acc
		if len(current.Storage) > 0 || len(merged.Storage) > 0 {
			slots := copyStorage(current.Storage)
			for k, v := range merged.Storage {
				if v == zeroHashHex {
					delete(slots, k)
					continue
				}
				slots[k] = v
			}
			merged.Storage = slots
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
var _ = big.NewInt
