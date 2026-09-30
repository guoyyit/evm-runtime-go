package state

import (
	"math/big"
	"path/filepath"
	"testing"
)

func TestSnapshotRevertBalanceStorage(t *testing.T) {
	s, _ := NewStore("")
	var a Address
	a[19] = 1
	st := s.WorkingState()
	st.AddBalance(a, big.NewInt(100))

	snap := st.Snapshot()
	st.AddBalance(a, big.NewInt(50))
	var k Hash
	k[31] = 9
	st.SetState(a, k, Hash{31: 7})
	st.SetNonce(a, 3)
	st.SetCode(a, []byte{1, 2, 3})

	if st.GetBalance(a).Int64() != 150 {
		t.Fatal("precondition balance")
	}
	st.Revert(snap)
	if st.GetBalance(a).Int64() != 100 {
		t.Fatalf("balance after revert = %d", st.GetBalance(a).Int64())
	}
	if got := st.GetState(a, k); got != (Hash{}) {
		t.Fatalf("storage after revert = %x", got)
	}
	if st.GetNonce(a) != 0 {
		t.Fatalf("nonce after revert = %d", st.GetNonce(a))
	}
	if len(st.GetCode(a)) != 0 {
		t.Fatalf("code after revert = %x", st.GetCode(a))
	}
}

func TestCreateAccountRevertRemovesIt(t *testing.T) {
	s, _ := NewStore("")
	var a Address
	a[19] = 2
	st := s.WorkingState()
	id := st.Snapshot()
	st.CreateAccount(a)
	st.AddBalance(a, big.NewInt(5))
	if !st.Exists(a) {
		t.Fatal("account should exist")
	}
	st.Revert(id)
	if st.Exists(a) {
		t.Fatal("account creation must be fully undone")
	}
}

func TestNestedSnapshots(t *testing.T) {
	s, _ := NewStore("")
	var a Address
	a[19] = 3
	st := s.WorkingState()
	st.AddBalance(a, big.NewInt(0))
	s0 := st.Snapshot()
	st.AddBalance(a, big.NewInt(10))
	s1 := st.Snapshot()
	st.AddBalance(a, big.NewInt(20))
	if st.GetBalance(a).Int64() != 30 {
		t.Fatal("precondition")
	}
	st.Revert(s1)
	if st.GetBalance(a).Int64() != 10 {
		t.Fatalf("revert to s1: %d", st.GetBalance(a).Int64())
	}
	st.Revert(s0)
	if st.GetBalance(a).Int64() != 0 {
		t.Fatalf("revert to s0: %d", st.GetBalance(a).Int64())
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	var a Address
	a[19] = 4
	st := s.WorkingState()
	st.AddBalance(a, big.NewInt(12345))
	st.SetCode(a, []byte{0xde, 0xad})
	var k Hash
	k[0] = 1
	st.SetState(a, k, Hash{31: 0xff})
	st.SetNonce(a, 9)
	if err := s.Commit(st, []Log{{Address: a, Data: []byte("hi")}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if s2.Balance(a).Int64() != 12345 {
		t.Fatal("balance")
	}
	if string(s2.Code(a)) != string([]byte{0xde, 0xad}) {
		t.Fatal("code")
	}
	if s2.StorageAt(a, k)[31] != 0xff {
		t.Fatal("storage")
	}
	if s2.Nonce(a) != 9 {
		t.Fatal("nonce")
	}
	logs := s2.QueryLogs(nil)
	if len(logs) != 1 || string(logs[0].Data) != "hi" {
		t.Fatal("logs")
	}
}

func TestWorkingStateIsolation(t *testing.T) {
	s, _ := NewStore("")
	var a Address
	a[19] = 5
	base := s.WorkingState()
	base.AddBalance(a, big.NewInt(100))
	if err := s.Commit(base, nil); err != nil {
		t.Fatal(err)
	}
	w1 := s.WorkingState()
	w2 := s.WorkingState()
	w1.AddBalance(a, big.NewInt(50))
	if w2.GetBalance(a).Int64() != 100 {
		t.Fatal("w2 must not see w1 uncommitted change")
	}
}
