package runtime

import (
	"math/big"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
)

func TestSStoreClearRefund(t *testing.T) {
	r := testRuntime(t)
	from := common.HexToAddress("0x5501")
	r.setBalanceForTest(from, big.NewInt(1e18))

	// Runtime code writes calldataload(0) into slot0. First call sets 1,
	// second call sets 0 (clearing), which earns the EIP-3529 refund.
	runtimeCode := []byte{
		0x60, 0x00, 0x35, // CALLDATALOAD 0
		0x60, 0x00, 0x55, // SSTORE slot0
		0x00,
	}
	cfg := baseConfig(from)
	cfg.Data = buildInitCode(runtimeCode)
	deploy, _ := r.Execute(cfg)
	if deploy.Failed {
		t.Fatal(deploy.Err)
	}
	to := *deploy.Address

	setCall := baseConfig(from)
	setCall.To = &to
	setCall.Data = []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	if res, _ := r.Execute(setCall); res.Failed {
		t.Fatal(res.Err)
	}

	clearCall := baseConfig(from)
	clearCall.To = &to
	clearCall.Data = make([]byte, 32)
	clearCall.GasLimit = 100_000
	res, err := r.Execute(clearCall)
	if err != nil || res.Failed {
		t.Fatalf("clear call: %v %v", err, res.Err)
	}

	// Without refund the cold clear would cost around 2600+5000.
	// EIP-3529 refunds 4800 capped at gasUsed/5.
	// We assert a refund actually lowered the used amount below the raw charge.
	upperBound := uint64(3 + 3 + 2600 + 5000)
	if res.GasUsed >= upperBound {
		t.Fatalf("expected refund to reduce gas, used=%d >= %d", res.GasUsed, upperBound)
	}

	slot, _ := r.StorageAt(to, common.HexToHash("0x0"))
	if slot.Big().Sign() != 0 {
		t.Fatalf("slot not cleared: %s", slot.Hex())
	}
}
