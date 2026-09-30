package evm

import (
	"encoding/hex"
	"errors"
	"testing"

	"github.com/guoyyit/evm-runtime-go/state"
)

func TestSHA3Opcode(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000000"))
	// store "abc" at mem0, SHA3 mem0 size3, mstore result at 0? simpler:
	// PUSH 0x636261 ("abc") MSTORE at 0; SHA3(0,3); PUSH 32; PUSH 0; MSTORE; RETURN
	code := []byte{
		0x61, 0x63, 0x62, 0x60, 0x00, 0x52, // MSTORE (note value truncated to word)
	}
	// Build precisely: store the 3 bytes then hash.
	code = []byte{
		0x60, 0x61, // 'a'
		0x60, 0x00, 0x53, // MSTORE8 0
		0x60, 0x62, 0x60, 0x01, 0x53,
		0x60, 0x63, 0x60, 0x02, 0x53,
		0x60, 0x03, 0x60, 0x00, 0x20, // SHA3 (offset top=0? order: size,offset)
		0x60, 0x00, 0x52,
		0x60, 0x20, 0x60, 0x00, 0xf3,
	}
	res, _ := r.Execute(Message{From: from, Data: code, GasLimit: 200_000})
	if res.Err != nil {
		t.Fatalf("sha3: %v", res.Err)
	}
	want := "4e03657aea45a94fc7d47ba826c8d667c0d1e6e33a64a036ec44f58fa12d6c45"
	if hex.EncodeToString(res.ReturnData) != want {
		t.Fatalf("sha3(abc)=%x", res.ReturnData)
	}
}

func TestCreate2AddressDeterministic(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000000000"))
	// Runtime code to deploy is a single STOP; init returns it.
	// init: PUSH1 0; PUSH1 0; MSTORE (stores 0x00 at byte 31)
	//       PUSH1 1; PUSH1 31; RETURN -> runtime = 0x00
	// CREATE2 from memory: copy init from calldata at runtime level is hard;
	// instead deploy a factory contract.
	factory := buildCreate2Factory()
	faddr := deploy(t, r, from, factory)
	res, _ := r.Execute(Message{From: from, To: &faddr, GasLimit: 2_000_000})
	if res.Err != nil {
		t.Fatalf("factory: %v used=%d", res.Err, res.GasUsed)
	}
	firstAddr := retToAddr(res.ReturnData)
	// Two runs with same salt/initcode from same factory must agree.
	r2 := testRuntime(t)
	from2 := addrAt(1)
	_ = r2.Mint(from2, bigWei("1000000000"))
	faddr2 := deploy(t, r2, from2, factory)
	res2, _ := r2.Execute(Message{From: from2, To: &faddr2, GasLimit: 2_000_000})
	if res2.Err != nil {
		t.Fatal(res2.Err)
	}
	deployed := firstAddr
	if len(r.Code(deployed)) != 1 || r.Code(deployed)[0] != 0x00 {
		t.Fatalf("expected 1-byte STOP runtime code, got %x", r.Code(deployed))
	}
	// CREATE2 address must be identical across independent runtimes.
	if retToAddr(res2.ReturnData) != firstAddr {
		t.Fatal("CREATE2 addresses differ across identical runs")
	}
}

func retToAddr(b []byte) state.Address {
	var a state.Address
	copy(a[:], b[12:])
	return a
}

// buildCreate2Factory deploys a 1-byte (STOP) contract with CREATE2 salt 0
// and returns the new address padded to 32 bytes.
func buildCreate2Factory() []byte {
	// memory[0:32] = initcode = constructor that returns a STOP runtime.
	// Place initcode at mem offset 0x40 (8 bytes), runtime placeholder at 0x20.
	// Simpler: initcode 8 bytes:
	//   60 00 60 00 53 60 01 60 00 f3  (MSTORE8 0,0; RETURN 0,1) -> 9 bytes
	initcode := []byte{0x60, 0x00, 0x60, 0x00, 0x53, 0x60, 0x01, 0x60, 0x00, 0xf3}
	var c []byte
	// store initcode bytes via a PUSH of the word-aligned blob (9 bytes -> pad)
	blob := make([]byte, 32)
	copy(blob[32-len(initcode):], initcode)
	c = append(c, 0x7f) // PUSH32
	c = append(c, blob...)
	c = append(c, 0x60, 0x00, 0x52) // MSTORE 0
	// CREATE2 stack: salt(top), offset, size, value
	c = append(c,
		0x5f,                         // PUSH0 value
		0x60, byte(32-len(initcode)), // offset of initcode in memory
		0x60, byte(len(initcode)), // size
		0x5f, // salt 0
		0xf5, // CREATE2
	)
	// returned address (0 on failure) -> store and return
	c = append(c,
		0x60, 0x00, 0x52, // MSTORE 0
		0x60, 0x20, 0x60, 0x00, 0xf3,
	)
	return c
}

func TestSelfDestruct(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000000"))
	ben := addrAt(9)
	// Contract with balance that self-destructs to ben.
	code := []byte{
		0x73, // PUSH20 beneficiary
	}
	word := make([]byte, 20)
	copy(word[:], ben[:])
	code = append(code, word...)
	code = append(code, 0xff) // SELFDESTRUCT
	contract := deploy(t, r, from, code)

	// Give the contract balance independently of running its self-destruct
	// code: mint directly into the committed account (privileged genesis
	// style funding), so a later message triggers SELFDESTRUCT.
	if err := r.Mint(contract, bigWei("500")); err != nil {
		t.Fatal(err)
	}
	// Now run it to self-destruct.
	res2, _ := r.Execute(Message{From: from, To: &contract, GasLimit: 100_000})
	if res2.Err != nil {
		t.Fatalf("selfdestruct: %v", res2.Err)
	}
	if r.Balance(ben) != "500" {
		t.Fatalf("beneficiary got %s, want 500", r.Balance(ben))
	}
	if r.Balance(contract) != "0" {
		t.Fatalf("destroyed contract retains balance %s", r.Balance(contract))
	}
}

func TestStorageRefundOnClear(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000000"))
	// A single deployed contract that first sets slot1=1, then clears it
	// within the same transaction (original=0, current goes 0->1->0).
	code := []byte{
		0x60, 0x01, 0x60, 0x01, 0x55, // push value1, key1 -> slot1=1
		0x60, 0x00, // value=0 (pushed first)
		0x60, 0x01, // key=1 (on top)
		0x55,
		0x00,
	}
	contract := deploy(t, r, from, code)
	res, _ := r.Execute(Message{From: from, To: &contract, GasLimit: 100_000})
	if res.Err != nil {
		t.Fatalf("clear: %v", res.Err)
	}
	if res.GasRefund == 0 {
		t.Fatal("clearing a nonzero slot must produce a refund")
	}
	var slot1 [32]byte
	slot1[31] = 1
	if got := r.StorageAt(contract, slot1); got != [32]byte{} {
		t.Fatalf("slot not cleared: %x", got)
	}
}

func TestMidExecutionSnapshot(t *testing.T) {
	r, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000"))
	res, st := r.TryExecute(Message{From: from, Data: []byte{0x00}, GasLimit: 100_000})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	// Mutate then snapshot/revert on the uncommitted working state.
	var to [20]byte
	to[19] = 5
	id := st.Snapshot()
	st.AddBalance(to, bigWei("777"))
	st.Revert(id)
	if st.GetBalance(to).Sign() != 0 {
		t.Fatal("working-state revert failed")
	}
}

func TestInvalidOpBurnsGas(t *testing.T) {
	r := testRuntime(t)
	from := addrAt(1)
	_ = r.Mint(from, bigWei("1000000"))
	res, _ := r.Execute(Message{From: from, Data: []byte{0xfe}, GasLimit: 12345})
	if !errors.Is(res.Err, ErrInvalidOpCode) {
		t.Fatalf("want invalid, got %v", res.Err)
	}
	if res.GasUsed != 12345 {
		t.Fatalf("INVALID must burn all gas, used=%d", res.GasUsed)
	}
}
