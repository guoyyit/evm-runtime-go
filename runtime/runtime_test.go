package runtime

import (
	"errors"
	"math/big"
	"path/filepath"
	"sync"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
	"github.com/guoyyit/evm-runtime-go/evm"
)

func testRuntime(t *testing.T) *Runtime {
	t.Helper()
	r, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func baseConfig(from common.Address) Config {
	return Config{
		From:     from,
		GasLimit: 10_000_000,
		Block: evm.BlockContext{
			Number:    big.NewInt(100),
			Timestamp: 1700000000,
			GasLimit:  10_000_000,
		},
	}
}

// TestDeployAndStore covers CREATE + SSTORE + SLOAD + persistence.
func TestDeployAndStore(t *testing.T) {
	r := testRuntime(t)
	from := common.HexToAddress("0xaaaa")

	// Init code: stores 0x2a at slot 7 then returns runtime code:
	// runtime code: PUSH1 0x2a PUSH1 7 SSTORE (deployed separately)
	runtimeCode := []byte{
		0x60, 0x2a, // 0x2a
		0x60, 0x07, // slot 7
		0x55,             // SSTORE
		0x60, 0x07, 0x54, // PUSH1 7 SLOAD
		0x60, 0x00, 0x52, // PUSH1 0 MSTORE
		0x60, 0x20, 0x60, 0x00, 0xf3, // RETURN 32 bytes
	}
	initCode := append([]byte{
		// Copy runtime code from end of init code via CODECOPY.
	}, buildInitCode(runtimeCode)...)

	cfg := baseConfig(from)
	cfg.Data = initCode
	res, err := r.Execute(cfg)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Failed {
		t.Fatalf("deploy failed: %v", res.Err)
	}
	if res.Address == nil {
		t.Fatal("missing created address")
	}

	// Call the getter and verify output.
	callCfg := baseConfig(from)
	to := *res.Address
	callCfg.To = &to
	res2, err := r.Execute(callCfg)
	if err != nil || res2.Failed {
		t.Fatalf("call: %v %v", err, res2.Err)
	}
	if new(big.Int).SetBytes(res2.ReturnData).Cmp(big.NewInt(0x2a)) != 0 {
		t.Fatalf("getter returned %x", res2.ReturnData)
	}

	got, err := r.StorageAt(to, common.HexToHash("0x7"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Big().Cmp(big.NewInt(0x2a)) != 0 {
		t.Fatalf("storage = %s, want 42", got.Hex())
	}
}

func TestAtomicFailureNoState(t *testing.T) {
	r := testRuntime(t)
	from := common.HexToAddress("0xfeed")

	// Init code performs SSTORE then hits INVALID, so creation fails and all
	// state changes (including the account itself) must be rolled back.
	code := []byte{
		0x60, 0x01, 0x60, 0x00, 0x55, // PUSH1 1 PUSH1 0 SSTORE
		0xfe, // INVALID -> creation failure
	}

	// Fund creator first.
	if err := r.setBalanceForTest(from, big.NewInt(1e18)); err != nil {
		t.Fatal(err)
	}

	cfg := baseConfig(from)
	cfg.Data = code
	res, err := r.Execute(cfg)
	if err != nil {
		t.Fatalf("execute returned transport error: %v", err)
	}
	if !res.Failed {
		t.Fatal("expected failure")
	}
	if !errors.Is(res.Err, evm.ErrInvalidOpcode) {
		t.Fatalf("want invalid opcode, got %v", res.Err)
	}

	// The funding step consumed revision 1; the failed execution must not
	// advance it further.
	if rev := r.Revision(); rev != 1 {
		t.Fatalf("revision = %d, want 1 after failed tx", rev)
	}
	bal, _ := r.Balance(from)
	if bal.Cmp(big.NewInt(1e18)) != 0 {
		t.Fatalf("creator balance changed to %s", bal)
	}
}

func TestInsufficientBalance(t *testing.T) {
	r := testRuntime(t)
	from := common.HexToAddress("0x0001")
	to := common.HexToAddress("0x0002")

	cfg := baseConfig(from)
	cfg.To = &to
	cfg.Value = big.NewInt(1000)
	res, _ := r.Execute(cfg)
	if !res.Failed || !errors.Is(res.Err, evm.ErrInsufficientBalance) {
		t.Fatalf("want insufficient balance, got failed=%v err=%v", res.Failed, res.Err)
	}
}

func TestReadOnlyWriteViolation(t *testing.T) {
	r := testRuntime(t)
	from := common.HexToAddress("0x00aa")
	r.setBalanceForTest(from, big.NewInt(1e18))

	// Deploy a contract that does SSTORE.
	runtimeCode := []byte{0x60, 0x01, 0x60, 0x00, 0x55, 0x00}
	cfg := baseConfig(from)
	cfg.Data = buildInitCode(runtimeCode)
	res, _ := r.Execute(cfg)
	if res.Failed {
		t.Fatalf("deploy: %v", res.Err)
	}
	to := *res.Address

	q := baseConfig(from)
	q.To = &to
	out, err := r.Query(q)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Failed || !errors.Is(out.Err, evm.ErrWriteInReadOnly) {
		t.Fatalf("want write-in-read-only, got failed=%v err=%v", out.Failed, out.Err)
	}
}

func TestConcurrentDeterminism(t *testing.T) {
	r := testRuntime(t)

	// 50 independent value transfers to disjoint accounts, all from a
	// well-funded treasury.
	treasury := common.HexToAddress("0x9999")
	if err := r.setBalanceForTest(treasury, new(big.Int).Mul(big.NewInt(1e18), big.NewInt(1000))); err != nil {
		t.Fatal(err)
	}

	const n = 50
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			to := common.Address{byte(i >> 8), byte(i)}
			cfg := baseConfig(treasury)
			cfg.To = &to
			cfg.Value = big.NewInt(int64(1000 + i))
			res, err := r.Execute(cfg)
			if err != nil {
				errs <- err
				return
			}
			if res.Failed {
				errs <- res.Err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent execute: %v", err)
	}

	for i := 0; i < n; i++ {
		to := common.Address{byte(i >> 8), byte(i)}
		bal, err := r.Balance(to)
		if err != nil {
			t.Fatal(err)
		}
		if bal.Cmp(big.NewInt(int64(1000+i))) != 0 {
			t.Fatalf("account %d balance = %s", i, bal)
		}
	}
}

// buildInitCode wraps runtime code into an init code blob that deploys it:
// PUSH2 <len> PUSH1 <offset> PUSH1 0 CODECOPY PUSH2 <len> PUSH1 0 RETURN <runtime>
func buildInitCode(runtime []byte) []byte {
	length := len(runtime)
	offset := 14
	init := []byte{
		0x61, byte(length >> 8), byte(length), // PUSH2 length
		0x60, byte(offset), // PUSH1 offset
		0x60, 0x00, // PUSH1 0
		0x39,                                  // CODECOPY
		0x61, byte(length >> 8), byte(length), // PUSH2 length
		0x60, 0x00, // PUSH1 0
		0xf3, // RETURN
	}
	return append(init, runtime...)
}
