package state

import (
	"math/big"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
)

// TestRevertClearsCommitMarkers: a write that is rolled back must not leave
// a commit marker behind. Otherwise the rolled-back (stale) value would be
// written on commit and could clobber a concurrent committer's newer value.
func TestRevertClearsCommitMarkers(t *testing.T) {
	db, store := newTestDB(t)
	a := common.Address{0x0a}
	k := common.Hash{1}
	db.AddBalance(a, big.NewInt(100))
	db.SetState(a, k, common.Hash{5})
	if _, err := db.Commit(); err != nil {
		t.Fatal(err)
	}

	// db2 rolls its writes back; nothing may be committed from it.
	db2, err := NewStateDB(store)
	if err != nil {
		t.Fatal(err)
	}
	snap := db2.Snapshot()
	db2.SetState(a, k, common.Hash{99})
	db2.AddBalance(a, big.NewInt(50))
	db2.Revert(snap)

	// db3 commits a newer value for the same slot and balance concurrently.
	db3, err := NewStateDB(store)
	if err != nil {
		t.Fatal(err)
	}
	db3.SetState(a, k, common.Hash{77})
	db3.AddBalance(a, big.NewInt(20))
	if _, err := db3.Commit(); err != nil {
		t.Fatal(err)
	}

	// The rolled-back execution commits after the concurrent one.
	if _, err := db2.Commit(); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	acc := loaded.Accounts[a]
	if bal, _ := new(big.Int).SetString(acc.Balance, 10); bal.Cmp(big.NewInt(120)) != 0 {
		t.Fatalf("balance = %s, want 120 (rolled-back +50 must not clobber)", bal)
	}
	if got := acc.Storage[k.Hex()]; got != (common.Hash{77}).Hex() {
		t.Fatalf("slot = %s, want 77 (rolled-back write must not clobber)", got)
	}
}

// TestRevertRestoresLogsAndAccessList covers the journal restoration of logs
// and the EIP-2929 warm/cold access list, which decides gas costs on any
// re-execution.
func TestRevertRestoresLogsAndAccessList(t *testing.T) {
	db, _ := newTestDB(t)
	a := common.Address{0x0b}
	slot := common.Hash{2}

	snap := db.Snapshot()
	db.AddLog(Log{Address: a, Data: []byte{1, 2, 3}})
	db.AddAddressToAccessList(a)
	db.AddSlotToAccessList(a, slot)
	db.Revert(snap)

	if len(db.Logs()) != 0 {
		t.Fatalf("logs after revert = %d, want 0", len(db.Logs()))
	}
	if db.IsAddressWarm(a) {
		t.Fatal("address must be cold again after revert")
	}
	if db.IsSlotWarm(a, slot) {
		t.Fatal("slot must be cold again after revert")
	}
}

// TestReapplyAfterRevertMatchesCleanRun: mutations applied, rolled back and
// then re-applied must yield exactly the state of a clean run that never
// reverted — covering balances, nonces, code, storage and logs.
func TestReapplyAfterRevertMatchesCleanRun(t *testing.T) {
	a := common.Address{0x0c}
	b := common.Address{0x0d}
	k := common.Hash{3}

	base := func(db *StateDB) {
		db.AddBalance(a, big.NewInt(1000))
		db.SetState(a, k, common.Hash{7})
	}
	m2 := func(db *StateDB) {
		db.SubBalance(a, big.NewInt(250))
		db.AddBalance(b, big.NewInt(250))
		db.SetNonce(b, 3)
		db.SetCode(b, []byte{0x60, 0x00})
		db.SetState(a, k, common.Hash{8})
		db.AddLog(Log{Address: a, Data: []byte{9}})
	}

	// Run 1: base, snapshot, M2, revert, M2 again.
	db1, _ := newTestDB(t)
	base(db1)
	snap := db1.Snapshot()
	m2(db1)
	db1.Revert(snap)
	m2(db1)

	// Run 2: base, M2 — no snapshot/revert at all.
	db2, _ := newTestDB(t)
	base(db2)
	m2(db2)

	for _, addr := range []common.Address{a, b} {
		if db1.GetBalance(addr).Cmp(db2.GetBalance(addr)) != 0 {
			t.Fatalf("balance %s: %s vs %s", addr.Hex(), db1.GetBalance(addr), db2.GetBalance(addr))
		}
		if db1.GetNonce(addr) != db2.GetNonce(addr) {
			t.Fatalf("nonce %s: %d vs %d", addr.Hex(), db1.GetNonce(addr), db2.GetNonce(addr))
		}
		if string(db1.GetCode(addr)) != string(db2.GetCode(addr)) {
			t.Fatalf("code %s differs", addr.Hex())
		}
	}
	if db1.GetState(a, k) != db2.GetState(a, k) {
		t.Fatalf("storage differs: %s vs %s", db1.GetState(a, k).Hex(), db2.GetState(a, k).Hex())
	}
	l1, l2 := db1.Logs(), db2.Logs()
	if len(l1) != len(l2) || len(l1) != 1 {
		t.Fatalf("logs differ: %d vs %d", len(l1), len(l2))
	}
	if l1[0].Address != l2[0].Address || string(l1[0].Data) != string(l2[0].Data) {
		t.Fatal("log content differs")
	}
}
