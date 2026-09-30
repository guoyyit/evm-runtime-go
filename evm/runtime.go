package evm

import (
	"math/big"
	"sync"

	"github.com/guoyyit/evm-runtime-go/state"
)

// Runtime is the in-process entry point: it owns the persistent store and
// accepts execution requests. Requests never share working state and only the
// store mutex briefly serializes the final atomic commit.
type Runtime struct {
	store   *state.Store
	block   BlockContext
	tx      TxContext
	mu      sync.Mutex
	execMu  sync.Mutex
	revs    map[uint64][]byte
	nextRev uint64
}

// DefaultBlockContext returns deterministic environmental values (fixed
// chain id, timestamp and number) so identical inputs produce identical
// results regardless of wall clock.
func DefaultBlockContext() BlockContext {
	return BlockContext{
		Number:     big.NewInt(1),
		Time:       big.NewInt(1_700_000_000),
		GasLimit:   new(big.Int).SetUint64(30_000_000),
		BaseFee:    big.NewInt(1),
		ChainID:    big.NewInt(1),
		Difficulty: big.NewInt(0),
	}
}

// Open opens (or creates) a persistent runtime backed by the file at path.
// An empty path creates an in-memory runtime.
func Open(path string) (*Runtime, error) {
	store, err := state.NewStore(path)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		store: store,
		block: DefaultBlockContext(),
		tx:    TxContext{GasPrice: big.NewInt(1)},
		revs:  make(map[uint64][]byte),
	}, nil
}

// SetBlockContext overrides the fixed block environment.
func (r *Runtime) SetBlockContext(b BlockContext) { r.block = b }

// SetTxDefaults overrides the transaction gas price and prewarmed origin.
func (r *Runtime) SetTxDefaults(t TxContext) { r.tx = t }

// Close flushes the persistent state to disk.
func (r *Runtime) Close() error { return r.store.Close() }

// executeOn runs one EVM invocation over an already-created working state.
// All failures (OOG, revert, bad jump, ...) consume gas but never commit.
func (r *Runtime) executeOn(st *state.State, msg Message) *Result {
	tx := r.tx
	tx.Origin = msg.From
	e := New(st, r.block, tx)
	e.SetOriginalLookup(r.store.CommittedStorageAt)

	gas := msg.GasLimit
	result := &Result{}

	if msg.To == nil {
		addr, ret, used, err := e.Create(msg.From, normalizeValue(msg.Value), msg.Data, gas, nil)
		result.CreatedAddress = &addr
		result.GasUsed = used
		result.Err = err
		if err == ErrExecutionReverted {
			result.RevertData = ret
		}
		if err == nil {
			result.ReturnData = ret
		}
	} else {
		ret, used, err := e.Call(msg.From, *msg.To, normalizeValue(msg.Value), msg.Data, gas)
		result.GasUsed = used
		result.Err = err
		if err == ErrExecutionReverted {
			result.RevertData = ret
			result.ReturnData = ret
		} else if err == nil {
			result.ReturnData = ret
		}
	}

	// EIP-3529 caps the gas refund at half of the gas consumed.
	refund := e.Refund()
	if refund > result.GasUsed/2 {
		refund = result.GasUsed / 2
	}
	result.GasRefund = refund
	result.Logs = e.Logs()
	return result
}

// Execute runs msg against an isolated working state and, on success,
// atomically commits it. On failure nothing is committed and the committed
// store is untouched. Gas used is reported in both outcomes.
func (r *Runtime) Execute(msg Message) (*Result, error) {
	r.execMu.Lock()
	defer r.execMu.Unlock()
	st := r.store.WorkingState()
	result := r.executeOn(st, msg)
	if result.Err != nil {
		return result, nil
	}
	if err := r.store.Commit(st, result.Logs); err != nil {
		return nil, err
	}
	return result, nil
}

// ExecuteStatic runs a read-only request that is never committed and must not
// mutate state; any write attempt returns ErrWriteInReadOnly.
func (r *Runtime) ExecuteStatic(from, to state.Address, data []byte, gas uint64) (*Result, error) {
	st := r.store.WorkingState()
	tx := r.tx
	tx.Origin = from
	e := New(st, r.block, tx)
	ret, used, err := e.StaticCall(from, to, data, gas)
	res := &Result{ReturnData: ret, GasUsed: used, Err: err}
	if err == ErrExecutionReverted {
		res.RevertData = ret
	}
	return res, nil
}

// TryExecute exposes the full isolated execution without committing, so
// callers can inspect or roll back the working state themselves.
func (r *Runtime) TryExecute(msg Message) (*Result, *state.State) {
	st := r.store.WorkingState()
	return r.executeOn(st, msg), st
}

// Session is an isolated execution context over one working state. It lets a
// caller run several messages, take transactional snapshots at arbitrary
// points (including mid-execution frame state), revert and re-run with the
// same inputs to obtain identical results. Nothing is visible to the
// committed store until Commit is called.
type Session struct {
	rt  *Runtime
	st  *state.State
	evm *EVM
}

// NewSession opens an isolated execution session seeded from committed state.
func (r *Runtime) NewSession() *Session {
	st := r.store.WorkingState()
	tx := r.tx
	e := New(st, r.block, tx)
	e.SetOriginalLookup(r.store.CommittedStorageAt)
	return &Session{rt: r, st: st, evm: e}
}

// State returns the session's working state.
func (s *Session) State() *state.State { return s.st }

// Snapshot captures a full execution snapshot (world state, logs, refund,
// depth and active-frame stack/memory/pc/gas).
func (s *Session) Snapshot() executionSnapshot { return s.evm.Snapshot() }

// Revert restores a snapshot taken in this session.
func (s *Session) Revert(snap executionSnapshot) { s.evm.Revert(snap) }

// Run executes a message inside the session without committing.
func (s *Session) Run(msg Message) *Result {
	return s.rt.executeOn(s.st, msg)
}

// Call runs a direct message call on the session's own EVM, so snapshots
// taken around it share frame/state continuity.
func (s *Session) Call(from, to state.Address, value *big.Int, data []byte, gas uint64) *Result {
	ret, used, err := s.evm.Call(from, to, normalizeValue(value), data, gas)
	res := &Result{ReturnData: ret, GasUsed: used, Err: err, Logs: s.evm.Logs()}
	if err == ErrExecutionReverted {
		res.RevertData = ret
	}
	refund := s.evm.Refund()
	if refund > used/2 {
		refund = used / 2
	}
	res.GasRefund = refund
	return res
}

// Logs returns logs emitted within the session so far.
func (s *Session) Logs() []state.Log { return s.evm.Logs() }

// Commit atomically publishes the session's working state and logs.
func (s *Session) Commit(logs []state.Log) error {
	return s.rt.store.Commit(s.st, logs)
}

// CommitWorkingState atomically commits a working state produced by
// TryExecute. The state must originate from this runtime.
func (r *Runtime) CommitWorkingState(st *state.State, logs []state.Log) error {
	return r.store.Commit(st, logs)
}

// DiscardWorkingState drops an uncommitted working state without effect.
func (r *Runtime) DiscardWorkingState(*state.State) {}

func normalizeValue(v *big.Int) *big.Int {
	if v == nil {
		return new(big.Int)
	}
	return v
}

// Mint credits an address in the committed store. It is the only privileged
// state-building entry point (e.g. test/genesis funding).
func (r *Runtime) Mint(addr state.Address, amount *big.Int) error {
	r.execMu.Lock()
	defer r.execMu.Unlock()
	st := r.store.WorkingState()
	st.AddBalance(addr, amount)
	return r.store.Commit(st, nil)
}

// Balance returns the committed balance as a decimal string.
func (r *Runtime) Balance(addr state.Address) string {
	return r.store.Balance(addr).String()
}

// StorageAt reads a committed storage slot.
func (r *Runtime) StorageAt(addr state.Address, key state.Hash) state.Hash {
	return r.store.StorageAt(addr, key)
}

// Code returns the committed code of addr.
func (r *Runtime) Code(addr state.Address) []byte {
	return r.store.Code(addr)
}

// Nonce returns the committed nonce of addr.
func (r *Runtime) Nonce(addr state.Address) uint64 {
	return r.store.Nonce(addr)
}

// QueryLogs returns committed logs, optionally filtered by emitting address.
func (r *Runtime) QueryLogs(addr *state.Address) []state.Log {
	return r.store.QueryLogs(addr)
}

// Snapshot captures the committed store file and returns a revision id.
// In-memory runtimes copy the serialized state instead.
func (r *Runtime) Snapshot() (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	data, err := r.store.Serialize()
	if err != nil {
		return 0, err
	}
	id := r.nextRev
	r.nextRev++
	r.revs[id] = data
	return id, nil
}

// RevertTo restores the committed store to a revision from Snapshot.
func (r *Runtime) RevertTo(id uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	data, ok := r.revs[id]
	if !ok {
		return ErrUnknownRevision
	}
	return r.store.Replace(data)
}
