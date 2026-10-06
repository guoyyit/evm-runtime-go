package state

import (
	"math/big"
	"path/filepath"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
)

func newTestDB(t *testing.T) (*StateDB, *FileStore) {
	t.Helper()
	store, err := NewFileStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := NewStateDB(store)
	if err != nil {
		t.Fatal(err)
	}
	return db, store
}

func TestSnapshotRollbackBalance(t *testing.T) {
	db, _ := newTestDB(t)
	a := common.Address{0x01}
	b := common.Address{0x02}
	db.AddBalance(a, big.NewInt(100))

	snap := db.Snapshot()
	db.SubBalance(a, big.NewInt(40))
	db.AddBalance(b, big.NewInt(40))
	db.SetNonce(a, 9)

	db.Revert(snap)

	if db.GetBalance(a).Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("a balance after revert = %s", db.GetBalance(a))
	}
	if db.GetBalance(b).Sign() != 0 {
		t.Fatalf("b should not exist, got %s", db.GetBalance(b))
	}
	if db.GetNonce(a) != 0 {
		t.Fatalf("nonce after revert = %d", db.GetNonce(a))
	}
}

func TestSnapshotRollbackStorageAndCode(t *testing.T) {
	db, _ := newTestDB(t)
	a := common.Address{0x03}
	db.CreateAccount(a)

	snap := db.Snapshot()
	db.SetCode(a, []byte{0x60, 0x00})
	db.SetState(a, common.Hash{1}, common.Hash{2})
	db.Revert(snap)

	if len(db.GetCode(a)) != 0 {
		t.Fatalf("code should be gone: %x", db.GetCode(a))
	}
	if db.GetState(a, common.Hash{1}) != (common.Hash{}) {
		t.Fatal("storage should be gone after revert")
	}

	// Second snapshot: modify an existing slot, then revert restores prior value.
	db.SetState(a, common.Hash{5}, common.Hash{50})
	snap2 := db.Snapshot()
	db.SetState(a, common.Hash{5}, common.Hash{60})
	db.SetState(a, common.Hash{6}, common.Hash{70})
	db.Revert(snap2)

	if db.GetState(a, common.Hash{5}) != (common.Hash{50}) {
		t.Fatal("slot 5 should be restored to 50")
	}
	if db.GetState(a, common.Hash{6}) != (common.Hash{}) {
		t.Fatal("new slot 6 should be removed")
	}
}

func TestNestedSnapshots(t *testing.T) {
	db, _ := newTestDB(t)
	a := common.Address{0x04}
	db.AddBalance(a, big.NewInt(1000))

	s0 := db.Snapshot()
	db.AddBalance(a, big.NewInt(10))
	db.Snapshot()
	db.AddBalance(a, big.NewInt(20))
	s2 := db.Snapshot()
	db.AddBalance(a, big.NewInt(30))

	// Revert to s2 removes only the +30.
	db.Revert(s2)
	if db.GetBalance(a).Cmp(big.NewInt(1030)) != 0 {
		t.Fatalf("after revert s2 = %s", db.GetBalance(a))
	}
	// Revert to s0 removes +10 and +20.
	db.Revert(s0)
	if db.GetBalance(a).Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("after revert s0 = %s", db.GetBalance(a))
	}
}

func TestRestartPersistence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state.json")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := common.Address{0x05}
	db, _ := NewStateDB(store)
	db.AddBalance(a, big.NewInt(777))
	db.SetNonce(a, 3)
	db.CreateAccount(a)
	db.SetCode(a, []byte{0xfe})
	db.SetState(a, common.Hash{9}, common.Hash{11})
	db.AddLog(Log{Address: a, Data: []byte("event")})
	if _, err := db.Commit(); err != nil {
		t.Fatal(err)
	}

	// Reopen from disk.
	store2, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	db2, err := NewStateDB(store2)
	if err != nil {
		t.Fatal(err)
	}
	if db2.GetBalance(a).Cmp(big.NewInt(777)) != 0 {
		t.Fatal("balance lost across restart")
	}
	if db2.GetNonce(a) != 3 {
		t.Fatal("nonce lost across restart")
	}
	if string(db2.GetCode(a)) != string([]byte{0xfe}) {
		t.Fatal("code lost across restart")
	}
	if db2.GetState(a, common.Hash{9}) != (common.Hash{11}) {
		t.Fatal("storage lost across restart")
	}
	logs, _ := store2.GetLogs()
	if len(logs) != 1 || string(logs[0].Data) != "event" {
		t.Fatal("logs lost across restart")
	}
}

func TestUncommittedInvisible(t *testing.T) {
	db, store := newTestDB(t)
	a := common.Address{0x06}
	db.AddBalance(a, big.NewInt(50))

	// A fresh working state built on the same store must not see uncommitted data.
	other, err := NewStateDB(store)
	if err != nil {
		t.Fatal(err)
	}
	if other.GetBalance(a).Sign() != 0 {
		t.Fatal("uncommitted balance leaked")
	}
}

// TestRollbackRestoresImplicitAccounts: accounts created implicitly by
// balance/storage writes must disappear again on revert, and the touched
// sets must return to the snapshot point so a later commit writes nothing.
func TestRollbackRestoresImplicitAccounts(t *testing.T) {
	db, store := newTestDB(t)
	fresh := common.Address{0xaa}

	snap := db.Snapshot()
	db.AddBalance(fresh, big.NewInt(5))                // implicit create
	db.SetState(fresh, common.Hash{1}, common.Hash{9}) // implicit create + slot
	db.Revert(snap)

	if db.Exist(fresh) {
		t.Fatal("implicitly created account survived rollback")
	}
	if db.GetBalance(fresh).Sign() != 0 {
		t.Fatal("balance survived rollback")
	}
	if db.GetState(fresh, common.Hash{1}) != (common.Hash{}) {
		t.Fatal("storage survived rollback")
	}

	// A commit right after the rollback must be a no-op for accounts.
	if _, err := db.Commit(); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.Accounts[fresh]; ok {
		t.Fatal("rolled-back account was committed")
	}
}

// TestRollbackRestoresFullState reverts a batch of heterogeneous mutations
// and requires the rolled-back state to be indistinguishable from a pristine
// one, and re-application to reproduce the first execution exactly.
func TestRollbackRestoresFullState(t *testing.T) {
	db, _ := newTestDB(t)
	a := common.Address{0x01}
	b := common.Address{0x02}
	db.AddBalance(a, big.NewInt(1000))
	db.SetNonce(a, 1)

	mutate := func(s *StateDB) {
		s.SubBalance(a, big.NewInt(300))
		s.AddBalance(b, big.NewInt(300))
		s.SetNonce(a, 7)
		s.SetCode(b, []byte{0x60, 0x00})
		s.SetState(a, common.Hash{3}, common.Hash{33})
		s.AddAddressToAccessList(b)
		s.AddSlotToAccessList(a, common.Hash{3})
		s.AddLog(Log{Address: a, Data: []byte("x")})
	}

	snap := db.Snapshot()
	mutate(db)
	db.Revert(snap)

	// Every dimension must be back at the snapshot point.
	if got := db.GetBalance(a); got.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("a balance = %s", got)
	}
	if db.Exist(b) {
		t.Fatal("b must not exist after rollback")
	}
	if got := db.GetNonce(a); got != 1 {
		t.Fatalf("a nonce = %d", got)
	}
	if got := db.GetState(a, common.Hash{3}); got != (common.Hash{}) {
		t.Fatalf("slot 3 = %s", got.Hex())
	}
	if db.IsAddressWarm(b) || db.IsSlotWarm(a, common.Hash{3}) {
		t.Fatal("access list entries survived rollback")
	}
	if len(db.Logs()) != 0 {
		t.Fatal("logs survived rollback")
	}

	// Re-executing the same mutations reproduces the first run exactly.
	mutate(db)
	if got := db.GetBalance(a); got.Cmp(big.NewInt(700)) != 0 {
		t.Fatalf("re-applied a balance = %s", got)
	}
	if got := db.GetBalance(b); got.Cmp(big.NewInt(300)) != 0 {
		t.Fatalf("re-applied b balance = %s", got)
	}
	if got := db.GetNonce(a); got != 7 {
		t.Fatalf("re-applied nonce = %d", got)
	}
	if got := db.GetState(a, common.Hash{3}); got != (common.Hash{33}) {
		t.Fatalf("re-applied slot 3 = %s", got.Hex())
	}
	if len(db.Logs()) != 1 {
		t.Fatal("re-applied log missing")
	}
}

// TestConcurrentCommitMergesCodeAndBalance: an execution that loaded its
// base before a concurrent code deployment must not erase that code when it
// commits an unrelated balance change afterwards.
func TestConcurrentCommitMergesCodeAndBalance(t *testing.T) {
	db1, store := newTestDB(t)
	x := common.Address{0xcc}

	// db2 deploys code to X and commits first.
	db2, err := NewStateDB(store)
	if err != nil {
		t.Fatal(err)
	}
	db2.CreateAccount(x)
	db2.SetCode(x, []byte{0x60, 0x00})
	if _, err := db2.Commit(); err != nil {
		t.Fatal(err)
	}

	// db1 still works from the older base and only moves balance.
	db1.AddBalance(x, big.NewInt(42))
	if _, err := db1.Commit(); err != nil {
		t.Fatal(err)
	}

	check, err := NewStateDB(store)
	if err != nil {
		t.Fatal(err)
	}
	if string(check.GetCode(x)) != string([]byte{0x60, 0x00}) {
		t.Fatalf("concurrent balance commit erased code: %x", check.GetCode(x))
	}
	if check.GetBalance(x).Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("balance = %s, want 42", check.GetBalance(x))
	}
}
