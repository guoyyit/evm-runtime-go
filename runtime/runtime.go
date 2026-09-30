package runtime

import (
	"errors"
	"math/big"
	"sync"

	"github.com/guoyyit/evm-runtime-go/common"
	"github.com/guoyyit/evm-runtime-go/evm"
	"github.com/guoyyit/evm-runtime-go/state"
)

// Config configures an execution request.
type Config struct {
	From     common.Address
	To       *common.Address
	Value    *big.Int
	Data     []byte
	GasLimit uint64
	GasPrice *big.Int
	ReadOnly bool

	Block evm.BlockContext
}

// Result is the outcome of an execution.
type Result struct {
	ReturnData []byte
	GasUsed    uint64
	GasLeft    uint64
	Failed     bool
	Err        error
	Logs       []state.Log
	Address    *common.Address
}

// Runtime is the in-process entry point: it owns the persistent store and
// serializes commits while allowing concurrent independent executions.
type Runtime struct {
	store *state.FileStore

	commitMu sync.Mutex
	labelMu  sync.Mutex
	labels   map[int]string
}

// New opens a runtime backed by the given JSON state file.
func New(stateFile string) (*Runtime, error) {
	store, err := state.NewFileStore(stateFile)
	if err != nil {
		return nil, err
	}
	return &Runtime{store: store, labels: map[int]string{}}, nil
}

// Close releases the backing store.
func (r *Runtime) Close() error { return r.store.Close() }

// Execute runs a message or contract creation. On failure no state commits.
func (r *Runtime) Execute(cfg Config) (*Result, error) {
	if cfg.GasLimit == 0 {
		return nil, errors.New("runtime: gas limit must be positive")
	}
	db, err := state.NewStateDB(r.store)
	if err != nil {
		return nil, err
	}

	ev := evm.NewEVM(db, cfg.Block, evm.TxContext{
		Origin:   cfg.From,
		GasPrice: cfg.GasPrice,
	})

	value := cfg.Value
	if value == nil {
		value = new(big.Int)
	}

	result := &Result{}
	var (
		output []byte
		left   uint64
		runErr error
	)

	if cfg.To == nil {
		var addr common.Address
		output, addr, left, runErr = ev.Create(cfg.From, cfg.Data, cfg.GasLimit, value)
		result.Address = &addr
	} else if cfg.ReadOnly {
		output, left, runErr = ev.StaticCall(cfg.From, *cfg.To, cfg.Data, cfg.GasLimit)
	} else {
		output, left, runErr = ev.Call(cfg.From, *cfg.To, cfg.Data, cfg.GasLimit, value)
	}

	result.ReturnData = output
	result.GasLeft = left
	result.GasUsed = cfg.GasLimit - left

	if runErr != nil {
		result.Failed = true
		result.Err = runErr
		result.GasUsed = cfg.GasLimit
		result.GasLeft = 0
		result.ReturnData = evm.RevertData(runErr)
		// Discard the working state entirely; nothing is committed.
		return result, nil
	}

	// Storage clearing refunds only apply to successful executions.
	used := cfg.GasLimit - left
	used -= ev.RefundAdjustment(used)
	result.GasUsed = used
	result.GasLeft = cfg.GasLimit - used

	// Serialize commits; executions themselves use isolated working states.
	r.commitMu.Lock()
	logs, commitErr := db.Commit()
	r.commitMu.Unlock()
	if commitErr != nil {
		return nil, commitErr
	}
	result.Logs = logs
	return result, nil
}

// Query runs code in a read-only, never-committed snapshot.
func (r *Runtime) Query(cfg Config) (*Result, error) {
	cfg.ReadOnly = true
	db, err := state.NewStateDB(r.store)
	if err != nil {
		return nil, err
	}
	ev := evm.NewEVM(db, cfg.Block, evm.TxContext{
		Origin:   cfg.From,
		GasPrice: cfg.GasPrice,
	})
	if cfg.To == nil {
		return nil, errors.New("runtime: query requires a target contract")
	}
	output, left, runErr := ev.StaticCall(cfg.From, *cfg.To, cfg.Data, cfg.GasLimit)
	result := &Result{ReturnData: output, GasLeft: left, GasUsed: cfg.GasLimit - left}
	if runErr != nil {
		result.Failed = true
		result.Err = runErr
		result.GasUsed = cfg.GasLimit
		result.GasLeft = 0
		result.ReturnData = evm.RevertData(runErr)
	}
	return result, nil
}
