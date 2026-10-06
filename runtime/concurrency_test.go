package runtime

import (
	"math/big"
	"sync"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
)

// TestConcurrentSameAccountTransfers hammers a single recipient account with
// hundreds of concurrent value transfers from a single treasury. Delta-based
// commits must preserve every update: the final balances equal the exact
// serial totals regardless of interleaving or commit order.
func TestConcurrentSameAccountTransfers(t *testing.T) {
	r := testRuntime(t)
	treasury := common.HexToAddress("0x7ea5")
	recipient := common.HexToAddress("0xec1f")

	const n = 300
	const amount = 1000
	total := new(big.Int).Mul(big.NewInt(n), big.NewInt(amount))
	if err := r.setBalanceForTest(treasury, total); err != nil {
		t.Fatal(err)
	}
	baseRevision := r.Revision()

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cfg := baseConfig(treasury)
			cfg.To = &recipient
			cfg.Value = big.NewInt(amount)
			res, err := r.Execute(cfg)
			if err != nil {
				errs <- err
				return
			}
			if res.Failed {
				errs <- res.Err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent execute: %v", err)
	}

	got, err := r.Balance(recipient)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmp(total) != 0 {
		t.Fatalf("recipient balance = %s, want %s (lost updates)", got, total)
	}
	left, err := r.Balance(treasury)
	if err != nil {
		t.Fatal(err)
	}
	if left.Sign() != 0 {
		t.Fatalf("treasury balance = %s, want 0 (lost updates)", left)
	}
	if rev := r.Revision(); rev != baseRevision+n {
		t.Fatalf("revision = %d, want %d (one per commit)", rev, baseRevision+n)
	}
}

// TestConcurrentSameContractCalls runs hundreds of concurrent calls against
// one contract account. Each call transfers value and writes its own storage
// slot. The committed state must contain every slot and the exact sum of all
// transferred values — identical to any serial execution of the same calls.
func TestConcurrentSameContractCalls(t *testing.T) {
	r := testRuntime(t)
	treasury := common.HexToAddress("0x7ea5")

	const n = 300
	const amount = 1000
	total := new(big.Int).Mul(big.NewInt(n), big.NewInt(amount))
	if err := r.setBalanceForTest(treasury, total); err != nil {
		t.Fatal(err)
	}

	// Contract: SSTORE key=calldata[0:32], value=calldata[32:64]; STOP.
	runtimeCode := []byte{
		0x60, 0x20, 0x35, // CALLDATALOAD 32 (value)
		0x60, 0x00, 0x35, // CALLDATALOAD 0 (key)
		0x55, // SSTORE
		0x00, // STOP
	}
	contract := deployCode(t, r, treasury, runtimeCode)
	baseRevision := r.Revision()

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 1; i <= n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := big.NewInt(int64(i)).Bytes()
			val := big.NewInt(int64(i * 10)).Bytes()
			data := make([]byte, 64)
			copy(data[32-len(key):32], key)
			copy(data[64-len(val):64], val)

			cfg := baseConfig(treasury)
			cfg.To = &contract
			cfg.Value = big.NewInt(amount)
			cfg.Data = data
			cfg.GasLimit = 200_000
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
		t.Fatalf("concurrent call: %v", err)
	}

	bal, err := r.Balance(contract)
	if err != nil {
		t.Fatal(err)
	}
	if bal.Cmp(total) != 0 {
		t.Fatalf("contract balance = %s, want %s (lost updates)", bal, total)
	}
	left, err := r.Balance(treasury)
	if err != nil {
		t.Fatal(err)
	}
	if left.Sign() != 0 {
		t.Fatalf("treasury balance = %s, want 0 (lost updates)", left)
	}
	for i := 1; i <= n; i++ {
		key := common.BytesToHash(big.NewInt(int64(i)).Bytes())
		slot, err := r.StorageAt(contract, key)
		if err != nil {
			t.Fatal(err)
		}
		if slot.Big().Cmp(big.NewInt(int64(i*10))) != 0 {
			t.Fatalf("slot %d = %s, want %d", i, slot.Hex(), i*10)
		}
	}
	if rev := r.Revision(); rev != baseRevision+n {
		t.Fatalf("revision = %d, want %d", rev, baseRevision+n)
	}
}
