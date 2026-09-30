package evm

import (
	"testing"
)

// A counter contract: SSTORE slot0 = <literal> on each call, STOP.
func setterCode(v byte) []byte {
	return []byte{
		0x60, v, // value
		0x60, 0x00, // key=0 (top)
		0x55, // SSTORE
		0x00,
	}
}

func TestSessionSnapshotRevertReplay(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000000"))
	c1 := deploy(t, r, from, setterCode(11))
	c2 := deploy(t, r, from, setterCode(22))

	s := r.NewSession()
	base := s.Snapshot()

	res1 := s.Call(from, c1, nil, nil, 100_000)
	if res1.Err != nil {
		t.Fatal(res1.Err)
	}
	var slot0 [32]byte
	if s.State().GetState(c1, slot0)[31] != 11 {
		t.Fatal("c1 should hold 11 before revert")
	}

	mid := s.Snapshot()
	res2 := s.Call(from, c2, nil, nil, 100_000)
	if res2.Err != nil {
		t.Fatal(res2.Err)
	}
	if s.State().GetState(c2, slot0)[31] != 22 {
		t.Fatal("c2 should hold 22 before revert")
	}

	// Roll back the second call entirely.
	s.Revert(mid)
	if s.State().GetState(c2, slot0) != [32]byte{} {
		t.Fatal("c2 change must roll back")
	}
	if s.State().GetState(c1, slot0)[31] != 11 {
		t.Fatal("c1 change must survive partial revert")
	}

	// Re-run the identical call; result and state must match the first run.
	res2b := s.Call(from, c2, nil, nil, 100_000)
	if res2b.Err != nil {
		t.Fatal(res2b.Err)
	}
	if res2b.GasUsed != res2.GasUsed {
		t.Fatalf("replay gas differs: %d vs %d", res2b.GasUsed, res2.GasUsed)
	}
	if s.State().GetState(c2, slot0)[31] != 22 {
		t.Fatal("replay must reproduce identical state")
	}

	// Full revert to base wipes everything in the session.
	s.Revert(base)
	if s.State().GetState(c1, slot0) != [32]byte{} {
		t.Fatal("base revert must clear c1")
	}
}

func TestUncommittedSessionInvisible(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000000"))
	c := deploy(t, r, from, setterCode(5))

	s := r.NewSession()
	res := s.Call(from, c, nil, nil, 100_000)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	// Committed readers must not observe the uncommitted write.
	if r.StorageAt(c, [32]byte{}) != [32]byte{} {
		t.Fatal("uncommitted session state leaked to committed store")
	}
}
