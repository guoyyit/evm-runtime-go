package runtime

import (
	"errors"
	"math/big"
	"os"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
	"github.com/guoyyit/evm-runtime-go/evm"
)

// TestReplayDeterminism runs the same creation + call sequence twice against
// two independent runtimes and requires byte-identical state files.
func TestReplayDeterminism(t *testing.T) {
	path1 := t.TempDir() + "/a.json"
	path2 := t.TempDir() + "/b.json"
	blob1 := runScenario(t, path1)
	blob2 := runScenario(t, path2)
	if string(blob1) != string(blob2) {
		t.Fatalf("state files differ:\n%s\n----\n%s", blob1, blob2)
	}
}

func runScenario(t *testing.T, path string) []byte {
	t.Helper()
	r, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	from := common.HexToAddress("0xc0de")
	if err := r.setBalanceForTest(from, big.NewInt(1e18)); err != nil {
		t.Fatal(err)
	}

	// Contract stores keccak of block number + reads env values.
	runtimeCode := []byte{
		0x43, 0x60, 0x01, 0x52, // NUMBER -> mem[1]
		0x42, 0x60, 0x21, 0x52, // TIMESTAMP -> mem[0x21]
		0x33, 0x60, 0x41, 0x52, // CALLER -> mem[0x41]
		0x60, 0x60, 0x60, 0x01,
		0x20,             // KECCAK256(mem[1], 0x60)
		0x60, 0x00, 0x55, // SSTORE slot0
		0x00,
	}
	cfg := baseConfig(from)
	cfg.Data = buildInitCode(runtimeCode)
	res, err := r.Execute(cfg)
	if err != nil || res.Failed {
		t.Fatalf("deploy: %v %v", err, res.Err)
	}

	to := *res.Address
	for i := 0; i < 3; i++ {
		c := baseConfig(from)
		c.To = &to
		if res, err := r.Execute(c); err != nil || res.Failed {
			t.Fatalf("call %d: %v %v", i, err, res.Err)
		}
	}

	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return blob
}

// TestRestartReadCommitted funds, deploys, closes, reopens and reads back.
func TestRestartReadCommitted(t *testing.T) {
	path := t.TempDir() + "/state.json"
	r, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	from := common.HexToAddress("0xbeef")
	r.setBalanceForTest(from, big.NewInt(2e18))

	runtimeCode := []byte{0x60, 0x05, 0x60, 0x00, 0x55, 0x00}
	cfg := baseConfig(from)
	cfg.Data = buildInitCode(runtimeCode)
	res, _ := r.Execute(cfg)
	if res.Failed {
		t.Fatal(res.Err)
	}
	addr := *res.Address
	callCfg := baseConfig(from)
	callCfg.To = &addr
	if res, _ := r.Execute(callCfg); res.Failed {
		t.Fatal(res.Err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	r2, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()

	bal, err := r2.Balance(from)
	if err != nil {
		t.Fatal(err)
	}
	if bal.Cmp(big.NewInt(2e18)) != 0 {
		t.Fatalf("balance after restart = %s", bal)
	}
	code, _ := r2.Code(addr)
	if len(code) != len(runtimeCode) {
		t.Fatalf("code length mismatch %d vs %d", len(code), len(runtimeCode))
	}
	slot, _ := r2.StorageAt(addr, common.HexToHash("0x0"))
	if slot.Big().Cmp(big.NewInt(5)) != 0 {
		t.Fatalf("slot0 after restart = %s", slot.Hex())
	}
}

// TestGasReported ensures failed execution reports the gas actually consumed
// and the sentinel error is inspectable via errors.Is. A balance check fails
// before any instruction runs, so nothing may be reported as consumed.
func TestGasReported(t *testing.T) {
	r := testRuntime(t)
	from := common.HexToAddress("0x1111")
	to := common.HexToAddress("0x2222")
	r.setBalanceForTest(from, big.NewInt(0))
	cfg := baseConfig(from)
	cfg.To = &to
	cfg.Value = big.NewInt(1)
	res, err := r.Execute(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || !errors.Is(res.Err, evm.ErrInsufficientBalance) {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.GasUsed != 0 || res.GasLeft != cfg.GasLimit {
		t.Fatalf("gas accounting on failure: used=%d left=%d", res.GasUsed, res.GasLeft)
	}
}
