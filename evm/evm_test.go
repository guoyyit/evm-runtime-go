package evm

import (
	"errors"
	"math/big"
	"math/rand"
	"path/filepath"
	"sync"
	"testing"

	"github.com/guoyyit/evm-runtime-go/state"
)

func TestOutOfGas(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000000"))
	code := []byte{0x60, 0x01, 0x60, 0x00, 0x55, 0x00}
	res, _ := r.Execute(Message{From: from, Data: code, GasLimit: 100})
	if !errors.Is(res.Err, ErrOutOfGas) {
		t.Fatalf("want OOG, got %v", res.Err)
	}
	if res.GasUsed != 100 {
		t.Fatalf("want all gas consumed on OOG, used=%d", res.GasUsed)
	}
}

func TestStackOverflow(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000000"))
	var code []byte
	for i := 0; i < 1025; i++ {
		code = append(code, 0x60, 0x00)
	}
	res, _ := r.Execute(Message{From: from, Data: code, GasLimit: 10_000_000})
	if !errors.Is(res.Err, ErrStackOverflow) {
		t.Fatalf("want stack overflow, got %v", res.Err)
	}
}

func TestStackUnderflow(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000000"))
	res, _ := r.Execute(Message{From: from, Data: []byte{0x01, 0x00}, GasLimit: 100_000})
	if !errors.Is(res.Err, ErrStackUnderflow) {
		t.Fatalf("want underflow, got %v", res.Err)
	}
}

func TestInsufficientBalance(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	to := addrAt(2)
	_ = r.Mint(from, bigWei("10"))
	res, _ := r.Execute(Message{
		From: from, To: &to, Value: bigWei("100"), Data: nil, GasLimit: 100_000,
	})
	if !errors.Is(res.Err, ErrInsufficientBalance) {
		t.Fatalf("want insufficient balance, got %v", res.Err)
	}
	if r.Balance(from) != "10" {
		t.Fatalf("balance changed after failed transfer: %s", r.Balance(from))
	}
}

func TestCallDepth(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000000000000"))
	ws := r.store.WorkingState()
	e := New(ws, DefaultBlockContext(), TxContext{Origin: from, GasPrice: big.NewInt(1)})
	to := addrAt(99)
	for i := 0; i < MaxCallDepth; i++ {
		e.depth++
	}
	if e.canExecute() {
		t.Fatal("canExecute should be false at MaxCallDepth")
	}
	_, _, err := e.Call(from, to, big.NewInt(0), nil, 1_000_000)
	if !errors.Is(err, ErrCallDepth) {
		t.Fatalf("want call-depth error, got %v", err)
	}

	// A real recursive contract surfaces depth exhaustion as a failed inner
	// CALL (success flag 0), and the depth counter must return to zero after.
	e2 := New(r.store.WorkingState(), DefaultBlockContext(), TxContext{Origin: from, GasPrice: big.NewInt(1)})
	e2.depth = MaxCallDepth
	res := e2.callCore(callOpts{caller: from, to: to, gas: 100000})
	if !errors.Is(res.err, ErrCallDepth) {
		t.Fatalf("callCore at max depth: %v", res.err)
	}
}

func TestValueTransferSuccess(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	to := addrAt(2)
	_ = r.Mint(from, bigWei("1000"))
	res, _ := r.Execute(Message{
		From: from, To: &to, Value: bigWei("300"), GasLimit: 100_000,
	})
	if res.Err != nil {
		t.Fatalf("transfer: %v", res.Err)
	}
	if r.Balance(from) != "700" || r.Balance(to) != "300" {
		t.Fatalf("balances: from=%s to=%s", r.Balance(from), r.Balance(to))
	}
}

func TestRevertRollback(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000000"))
	// Store 7 at slot 1 then REVERT; creation must not persist.
	code := []byte{
		0x60, 0x07, 0x60, 0x01, 0x55,
		0x60, 0x00, 0x60, 0x00, 0xfd,
	}
	addr := deployFail(t, r, from, code)
	var slot state.Hash
	slot[31] = 1
	if got := r.StorageAt(addr, slot); got != (state.Hash{}) {
		t.Fatalf("revert must not persist storage, got %x", got)
	}
	if code := r.Code(addr); len(code) != 0 {
		t.Fatalf("reverted create must leave no code, got %x", code)
	}
}

func TestRestartRecovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	from := addrAt(7)
	_ = r.Mint(from, bigWei("555"))
	code := []byte{
		0x60, 0x09, 0x60, 0x02, 0x55,
		0x60, 0x01, 0x60, 0x00, 0xf3, // return 1 byte STOP
	}
	res, _ := r.Execute(Message{From: from, Data: code, GasLimit: 200_000})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	addr := *res.CreatedAddress
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	r2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	if r2.Balance(from) != "555" {
		t.Fatalf("balance lost across restart: %s", r2.Balance(from))
	}
	var slot state.Hash
	slot[31] = 2
	if got := r2.StorageAt(addr, slot); got[31] != 9 {
		t.Fatalf("storage lost across restart: %x", got)
	}
	if len(r2.Code(addr)) != 1 {
		t.Fatal("code lost across restart")
	}
}

func TestDeterminism(t *testing.T) {
	dep := []byte{
		0x60, 0x2a, 0x60, 0x00, 0x55,
		0x60, 0x01, 0x60, 0x00, 0xf3,
	}
	var addrs [3]state.Address
	var slots [3]state.Hash
	from := addrAt(10)
	for i := range addrs {
		r, _ := Open(t.TempDir() + "/s.json")
		_ = r.Mint(from, bigWei("1000000"))
		res, _ := r.Execute(Message{From: from, Data: dep, GasLimit: 200_000})
		if res.Err != nil {
			t.Fatal(res.Err)
		}
		addrs[i] = *res.CreatedAddress
		slots[i] = r.StorageAt(addrs[i], state.Hash{})
		_ = r.Close()
	}
	if addrs[0] != addrs[1] || addrs[1] != addrs[2] {
		t.Fatalf("addresses not deterministic: %x %x %x", addrs[0], addrs[1], addrs[2])
	}
	for i := range slots {
		if slots[i][31] != 42 {
			t.Fatal("storage mismatch across identical runs")
		}
	}
}

func TestConcurrentIsolation(t *testing.T) {
	r := testRuntime(t)
	var wg sync.WaitGroup
	const n = 16
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			from := addrAt(byte(0x20 + idx))
			if err := r.Mint(from, bigWei("1000")); err != nil {
				errs[idx] = err
				return
			}
			if r.Balance(from) != "1000" {
				errs[idx] = errors.New("mint not visible before transfer: " + r.Balance(from))
				return
			}
			to := addrAt(byte(0x40 + idx))
			_, errs[idx] = r.Execute(Message{
				From: from, To: &to, Value: bigWei("250"), GasLimit: 100_000,
			})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	for i := 0; i < n; i++ {
		from := addrAt(byte(0x20 + i))
		to := addrAt(byte(0x40 + i))
		if r.Balance(from) != "750" || r.Balance(to) != "250" {
			t.Fatalf("account %d balances wrong: %s/%s", i, r.Balance(from), r.Balance(to))
		}
	}
}

func TestStaticWriteViolation(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000000"))
	code := []byte{
		0x60, 0x01, 0x60, 0x00, 0x55,
		0x00,
	}
	addr := deploy(t, r, from, code)
	res, _ := r.ExecuteStatic(from, addr, nil, 100_000)
	if !errors.Is(res.Err, ErrWriteInReadOnly) {
		t.Fatalf("want write-in-readonly, got %v", res.Err)
	}
}

func TestRandomBytecode(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000000000000"))
	rng := rand.New(rand.NewSource(42))
	allowed := []byte{
		0x01, 0x02, 0x03, 0x04, 0x10, 0x14, 0x15, 0x16, 0x19,
		0x50, 0x56, 0x57, 0x5b, 0x5f, 0x60,
		0x80, 0x81, 0x90, 0x00, 0xf3, 0xfd,
	}
	for trial := 0; trial < 200; trial++ {
		l := 1 + rng.Intn(40)
		code := make([]byte, l)
		for i := range code {
			op := allowed[rng.Intn(len(allowed))]
			code[i] = op
			if op == 0x60 && i+1 < l {
				i++
				code[i] = byte(rng.Intn(256))
			}
		}
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					t.Fatalf("panic on random code %x: %v", code, rec)
				}
			}()
			res, _ := r.Execute(Message{From: from, Data: code, GasLimit: 500_000})
			if res.Err != nil {
				switch res.Err {
				case ErrOutOfGas, ErrInvalidOpCode, ErrInvalidJump,
					ErrStackOverflow, ErrStackUnderflow,
					ErrExecutionReverted, ErrReturnDataOOB:
				default:
					t.Fatalf("unexpected error for %x: %v", code, res.Err)
				}
			}
		}()
	}
}

func TestSnapshotRevertCommitted(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000"))
	rev, err := r.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	to := addrAt(2)
	_, _ = r.Execute(Message{From: from, To: &to, Value: bigWei("400"), GasLimit: 100_000})
	if r.Balance(to) != "400" {
		t.Fatal("precondition")
	}
	if err := r.RevertTo(rev); err != nil {
		t.Fatal(err)
	}
	if r.Balance(from) != "1000" || r.Balance(to) != "0" {
		t.Fatalf("snapshot revert failed: %s/%s", r.Balance(from), r.Balance(to))
	}
}

func TestLogsRecorded(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000000"))
	// PUSH1 0xaa, PUSH1 0, MSTORE ; PUSH1 1 (size), PUSH1 31 (off), PUSH1 1 (topic) ... LOG1
	code := []byte{
		0x60, 0xaa, 0x60, 0x00, 0x52,
		0x60, 0x2a, // topic
		0x60, 0x01, // size
		0x60, 0x1f, // offset
		0xa1, // LOG1
		0x00,
	}
	addr := deploy(t, r, from, code)
	res, _ := r.Execute(Message{From: from, To: &addr, GasLimit: 200_000})
	if res.Err != nil {
		t.Fatalf("log call: %v used=%d", res.Err, res.GasUsed)
	}
	logs := r.QueryLogs(&addr)
	if len(logs) != 1 {
		t.Fatalf("want 1 log, got %d", len(logs))
	}
	if logs[0].Topics[0][31] != 0x2a || len(logs[0].Data) != 1 || logs[0].Data[0] != 0xaa {
		t.Fatalf("log content wrong: %+v", logs[0])
	}
}
