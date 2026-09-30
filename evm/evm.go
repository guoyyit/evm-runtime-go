package evm

import (
	"math/big"

	"github.com/guoyyit/evm-runtime-go/crypto"
	"github.com/guoyyit/evm-runtime-go/state"
)

// BlockContext contains block-level environmental values.
type BlockContext struct {
	Number     *big.Int
	Time       *big.Int
	GasLimit   *big.Int
	CoinBase   state.Address
	BaseFee    *big.Int
	ChainID    *big.Int
	Difficulty *big.Int
}

// TxContext contains transaction-level environmental values.
type TxContext struct {
	Origin   state.Address
	GasPrice *big.Int
}

// Message is one execution request.
type Message struct {
	From     state.Address
	To       *state.Address
	Value    *big.Int
	Data     []byte
	GasLimit uint64
	Static   bool
}

// Result is the deterministic outcome of one execution.
type Result struct {
	ReturnData     []byte
	GasUsed        uint64
	GasRefund      uint64
	Err            error
	RevertData     []byte
	CreatedAddress *state.Address
	Logs           []state.Log
}

// EVM is a single-threaded, transaction-local virtual machine. Each request
// gets its own EVM over an isolated working State, so concurrent executions
// never share mutable data.
type EVM struct {
	State *state.State
	Block BlockContext
	Tx    TxContext

	depth          int
	logs           []state.Log
	refund         int64
	warmAcct       map[state.Address]struct{}
	warmSlot       map[slotKey]struct{}
	origVals       map[slotKey]state.Hash
	lookupOriginal func(state.Address, state.Hash) state.Hash
	activeFrames   []*Frame
}

type slotKey struct {
	addr state.Address
	key  state.Hash
}

// executionSnapshot captures every mutable aspect of an EVM at one instant:
// world-state journal point, emitted logs, refund counter, call depth and the
// full volatile state (stack, memory, pc, gas) of every active frame.
type executionSnapshot struct {
	stateID  int
	logCount int
	refund   int64
	depth    int
	frames   []frameSnapshot
	warmAcct map[state.Address]struct{}
	warmSlot map[slotKey]struct{}
	origVals map[slotKey]state.Hash
}

// Snapshot captures a transactional execution point. When called with no
// active interpreter frame it captures only world state/logs/refund; during a
// contract run it additionally captures every frame's stack and memory.
func (e *EVM) Snapshot() executionSnapshot {
	snap := executionSnapshot{
		stateID:  e.State.Snapshot(),
		logCount: len(e.logs),
		refund:   e.refund,
		depth:    e.depth,
	}
	for _, fr := range e.activeFrames {
		snap.frames = append(snap.frames, fr.snapshot())
	}
	snap.warmAcct = make(map[state.Address]struct{}, len(e.warmAcct))
	for k := range e.warmAcct {
		snap.warmAcct[k] = struct{}{}
	}
	snap.warmSlot = make(map[slotKey]struct{}, len(e.warmSlot))
	for k := range e.warmSlot {
		snap.warmSlot[k] = struct{}{}
	}
	snap.origVals = make(map[slotKey]state.Hash, len(e.origVals))
	for k, v := range e.origVals {
		snap.origVals[k] = v
	}
	return snap
}

// Revert restores an execution snapshot completely, including account state,
// logs, refund, call depth and per-frame stack/memory/pc/gas.
func (e *EVM) Revert(snap executionSnapshot) {
	e.State.Revert(snap.stateID)
	e.logs = e.logs[:snap.logCount]
	e.refund = snap.refund
	e.depth = snap.depth
	for i, fs := range snap.frames {
		if i < len(e.activeFrames) {
			e.activeFrames[i].restore(fs)
		}
	}
	e.activeFrames = e.activeFrames[:len(snap.frames)]
	e.warmAcct = snap.warmAcct
	e.warmSlot = snap.warmSlot
	e.origVals = snap.origVals
}

// New constructs an EVM over the given isolated working state.
func New(st *state.State, block BlockContext, tx TxContext) *EVM {
	e := &EVM{
		State:    st,
		Block:    block,
		Tx:       tx,
		warmAcct: make(map[state.Address]struct{}),
		warmSlot: make(map[slotKey]struct{}),
		origVals: make(map[slotKey]state.Hash),
	}
	// Pre-warm the transaction origin and coinbase per EIP-2929.
	e.warmAcct[tx.Origin] = struct{}{}
	e.warmAcct[block.CoinBase] = struct{}{}
	return e
}

// SetOriginalLookup supplies the committed-state baseline used for EIP-2200
// SSTORE gas/refund computation.
func (e *EVM) SetOriginalLookup(f func(state.Address, state.Hash) state.Hash) {
	e.lookupOriginal = f
}

func (e *EVM) isWarm(addr state.Address) bool {
	_, ok := e.warmAcct[addr]
	return ok
}

// touchAccount charges the cold/warm access differential and marks the account
// warm. Returns false when there is not enough gas; the caller must not
// continue execution on false.
func (e *EVM) touchAccount(addr state.Address, gas *uint64) bool {
	if e.isWarm(addr) {
		return useGas(gas, gasWarmAccess)
	}
	if !useGas(gas, gasBalance) {
		return false
	}
	e.warmAcct[addr] = struct{}{}
	return true
}

func (e *EVM) isWarmSlot(addr state.Address, key state.Hash) bool {
	_, ok := e.warmSlot[slotKey{addr, key}]
	return ok
}

// original returns the committed storage value at the start of this
// transaction, lazily caching the first observation (EIP-2200 basis).
func (e *EVM) original(addr state.Address, key state.Hash) state.Hash {
	sk := slotKey{addr, key}
	if v, ok := e.origVals[sk]; ok {
		return v
	}
	var v state.Hash
	if e.lookupOriginal != nil {
		v = e.lookupOriginal(addr, key)
	} else {
		v = e.State.GetState(addr, key)
	}
	e.origVals[sk] = v
	return v
}

func (e *EVM) touchSlot(addr state.Address, key state.Hash, gas *uint64) bool {
	if e.isWarmSlot(addr, key) {
		return useGas(gas, gasWarmAccess)
	}
	if !useGas(gas, gasSload) {
		return false
	}
	e.warmSlot[slotKey{addr, key}] = struct{}{}
	return true
}

// addRefund / subRefund track the EIP-3529 capped storage refund counter.
func (e *EVM) addRefund(n uint64) { e.refund += int64(n) }
func (e *EVM) subRefund(n uint64) { e.refund -= int64(n) }

// useGas deducts amount from gasLeft, returning false on insufficient funds.
func useGas(gasLeft *uint64, amount uint64) bool {
	if amount > *gasLeft {
		*gasLeft = 0
		return false
	}
	*gasLeft -= amount
	return true
}

// transfer moves value from from to to, failing atomically on insufficient
// balance. State mutations are journaled and reverted by the caller.
func (e *EVM) transfer(from, to state.Address, value *big.Int) error {
	if value.Sign() == 0 {
		return nil
	}
	if e.State.GetBalance(from).Cmp(value) < 0 {
		return ErrInsufficientBalance
	}
	e.State.SubBalance(from, value)
	e.State.AddBalance(to, value)
	return nil
}

// canExecute reports whether another nested frame is allowed.
func (e *EVM) canExecute() bool { return e.depth < MaxCallDepth }

// callCore performs the shared pre-checks/transfer for CALL variants and then
// runs the callee code. It returns return data, gas consumed by the inner
// frame (not including the caller-side base/value gas), success and error.
type callOpts struct {
	caller   state.Address
	to       state.Address
	value    *big.Int
	input    []byte
	gas      uint64
	readOnly bool
	delegate bool // DELEGATECALL: code runs in caller's storage/value context
	callCode bool // CALLCODE: code of `to`, but self stays caller
}

// callResult carries everything a CALL-family opcode needs to settle.
type callResult struct {
	retData    []byte
	revertData []byte
	gasLeft    uint64
	err        error
}

func (e *EVM) callCore(o callOpts) callResult {
	if !e.canExecute() {
		return callResult{err: ErrCallDepth}
	}

	self := o.to
	codeAddr := o.to
	if o.delegate || o.callCode {
		self = o.caller
	}

	value := o.value
	if value == nil {
		value = new(big.Int)
	}
	if !o.delegate {
		if err := e.transfer(o.caller, self, value); err != nil {
			return callResult{err: err}
		}
	}

	code := e.State.GetCode(codeAddr)
	contract := &Contract{
		CodeAddress: codeAddr,
		Caller:      o.caller,
		Self:        self,
		Input:       o.input,
		Code:        code,
		Gas:         o.gas,
	}
	contract.Value = value

	e.depth++
	in := newInterpreter(e)
	ret, err := in.Run(contract, o.readOnly)
	e.depth--

	res := callResult{
		retData: ret,
		gasLeft: contract.Gas,
		err:     err,
	}
	if err == ErrExecutionReverted {
		res.revertData = ret
	}
	return res
}

// Call executes a message call with value transfer.
func (e *EVM) Call(caller, to state.Address, value *big.Int, input []byte, gas uint64) ([]byte, uint64, error) {
	res := e.callCore(callOpts{
		caller: caller,
		to:     to,
		value:  value,
		input:  input,
		gas:    gas,
	})
	return res.revertOrData(), gas - res.gasLeft, res.err
}

// StaticCall runs code that must not mutate state.
func (e *EVM) StaticCall(caller, to state.Address, input []byte, gas uint64) ([]byte, uint64, error) {
	res := e.callCore(callOpts{
		caller:   caller,
		to:       to,
		value:    new(big.Int),
		input:    input,
		gas:      gas,
		readOnly: true,
	})
	return res.revertOrData(), gas - res.gasLeft, res.err
}

func (r callResult) revertOrData() []byte {
	if r.err == ErrExecutionReverted {
		return r.revertData
	}
	return r.retData
}

// createAddress deterministically derives a contract address:
// keccak256(rlp([sender, nonce]))[12:]. Only the last 20 bytes are used and
// the nonce is incremented before derivation (post-increment semantics).
func createAddress(caller state.Address, nonce uint64) state.Address {
	enc := rlpAddressNonce(caller, nonce)
	hash := crypto.Keccak256(enc)
	var addr state.Address
	copy(addr[:], hash[12:])
	return addr
}

// create2Address derives keccak256(0xff ++ address ++ salt ++ keccak(code))[12:].
func create2Address(caller state.Address, salt state.Hash, code []byte) state.Address {
	buf := make([]byte, 0, 1+20+32+32)
	buf = append(buf, 0xff)
	buf = append(buf, caller[:]...)
	buf = append(buf, salt[:]...)
	codeHash := crypto.Keccak256(code)
	buf = append(buf, codeHash...)
	hash := crypto.Keccak256(buf)
	var addr state.Address
	copy(addr[:], hash[12:])
	return addr
}

// Create executes initCode, stores its returned runtime code at the derived
// address and returns the address, deployed code, gas consumed and error.
func (e *EVM) Create(caller state.Address, value *big.Int, initCode []byte, gas uint64, salt *state.Hash) (state.Address, []byte, uint64, error) {
	if !e.canExecute() {
		return state.Address{}, nil, 0, ErrCallDepth
	}

	nonce := e.State.GetNonce(caller)
	var addr state.Address
	if salt == nil {
		addr = createAddress(caller, nonce)
	} else {
		addr = create2Address(caller, *salt, initCode)
	}
	e.State.SetNonce(caller, nonce+1)

	if err := e.transfer(caller, addr, value); err != nil {
		return addr, nil, 0, err
	}

	contract := &Contract{
		CodeAddress: addr,
		Caller:      caller,
		Self:        addr,
		Value:       value,
		Input:       nil,
		Code:        initCode,
		Gas:         gas,
	}

	e.depth++
	in := newInterpreter(e)
	ret, err := in.Run(contract, false)
	e.depth--

	used := gas - contract.Gas
	if err != nil {
		return addr, nil, used, err
	}

	// Code deposit is billed separately against the remaining creation gas.
	depositGas := uint64(len(ret)) * gasCodeDeposit
	if depositGas > contract.Gas {
		return addr, nil, gas, ErrCodeStoreOOG
	}
	contract.Gas -= depositGas

	// Re-creation of an account that already holds code fails. Empty accounts
	// (or previously self-destructed ones in this transaction) may be reused.
	if existing := e.State.GetCode(addr); len(existing) > 0 {
		return addr, nil, gas, ErrInvalidOpCode
	}
	e.State.SetCode(addr, ret)
	return addr, ret, used, nil
}

// Refund returns the accumulated storage refund (never negative).
func (e *EVM) Refund() uint64 {
	if e.refund < 0 {
		return 0
	}
	return uint64(e.refund)
}

// Logs returns the logs emitted during this execution in emission order.
func (e *EVM) Logs() []state.Log { return e.logs }
