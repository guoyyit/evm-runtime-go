package runtime

import (
	"math/big"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
)

func TestSelfDestructPersistence(t *testing.T) {
	r := testRuntime(t)
	from := common.HexToAddress("0x1234")
	r.setBalanceForTest(from, big.NewInt(1e18))

	// Contract self-destructs immediately, sending balance to `from`.
	// Runtime: PUSH20 beneficiary; SELFDESTRUCT
	var beneficiary [20]byte
	beneficiary[19] = 0xff
	runtimeCode := append([]byte{0x73}, beneficiary[:]...)
	runtimeCode = append(runtimeCode, 0xff)

	cfg := baseConfig(from)
	cfg.Data = buildInitCode(runtimeCode)
	res, _ := r.Execute(cfg)
	if res.Failed {
		t.Fatal(res.Err)
	}
	contract := *res.Address

	// Fund the contract and call it to trigger SELFDESTRUCT.
	fundCfg := baseConfig(from)
	fundCfg.To = &contract
	fundCfg.Value = big.NewInt(777)
	if res, _ := r.Execute(fundCfg); res.Failed {
		t.Fatal(res.Err)
	}

	code, _ := r.Code(contract)
	if len(code) != 0 {
		t.Fatalf("code should be removed, got %d bytes", len(code))
	}
	bal, _ := r.Balance(contract)
	if bal.Sign() != 0 {
		t.Fatalf("contract balance = %s after destruct", bal)
	}
	got, _ := r.Balance(common.Address(beneficiary))
	if got.Cmp(big.NewInt(777)) != 0 {
		t.Fatalf("beneficiary = %s, want 777", got)
	}
}
