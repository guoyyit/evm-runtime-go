package evm

import (
	"math/big"

	"github.com/guoyyit/evm-runtime-go/common"
)

// BlockContext carries deterministic per-block environment values.
type BlockContext struct {
	Number      *big.Int
	Timestamp   uint64
	Coinbase    common.Address
	Difficulty  *big.Int
	GasLimit    uint64
	BaseFee     *big.Int
	ChainID     *big.Int
	BlockHashes map[uint64]common.Hash
}

// TxContext carries per-transaction environment values.
type TxContext struct {
	Origin   common.Address
	GasPrice *big.Int
}

// EVM is the execution environment shared by nested frames.
type EVM struct {
	StateDB     StateDB
	Block       BlockContext
	Tx          TxContext
	depth       int
	refund      uint64
	precompiles map[common.Address]PrecompiledContract
}

// NewEVM creates an execution environment with the default precompiles.
func NewEVM(db StateDB, block BlockContext, tx TxContext) *EVM {
	if block.Number == nil {
		block.Number = new(big.Int)
	}
	if block.BaseFee == nil {
		block.BaseFee = new(big.Int)
	}
	if block.ChainID == nil {
		block.ChainID = big.NewInt(1)
	}
	if block.Difficulty == nil {
		block.Difficulty = new(big.Int)
	}
	if tx.GasPrice == nil {
		tx.GasPrice = new(big.Int)
	}
	return &EVM{
		StateDB:     db,
		Block:       block,
		Tx:          tx,
		precompiles: defaultPrecompiles(),
	}
}

// Depth returns the current call nesting depth (top call is 0).
func (e *EVM) Depth() int { return e.depth }

func (e *EVM) isPrecompile(addr common.Address) bool {
	_, ok := e.precompiles[addr]
	return ok
}

// transfer moves value between two accounts, refusing an overdraft.
func (e *EVM) transfer(from, to common.Address, value *big.Int) error {
	if value.Sign() == 0 {
		return nil
	}
	if e.StateDB.GetBalance(from).Cmp(value) < 0 {
		return ErrInsufficientBalance
	}
	e.StateDB.SubBalance(from, value)
	e.StateDB.AddBalance(to, value)
	return nil
}

// Call runs a message call and returns output, remaining gas and error.
func (e *EVM) Call(caller, addr common.Address, input []byte, gas uint64, value *big.Int) ([]byte, uint64, error) {
	if e.depth >= MaxCallDepth {
		return nil, gas, ErrDepthLimitExceeded
	}
	if value == nil {
		value = new(big.Int)
	}
	if e.StateDB.GetBalance(caller).Cmp(value) < 0 {
		return nil, gas, ErrInsufficientBalance
	}

	snapshot := e.StateDB.Snapshot()
	refundSnap := e.refund
	if !e.StateDB.Exist(addr) {
		if value.Sign() != 0 || !e.isPrecompile(addr) {
			e.StateDB.CreateAccount(addr)
		}
	}
	if err := e.transfer(caller, addr, value); err != nil {
		e.StateDB.Revert(snapshot)
		e.refund = refundSnap
		return nil, gas, err
	}

	if precompile, ok := e.precompiles[addr]; ok {
		if gas < precompile.RequiredGas(input) {
			e.StateDB.Revert(snapshot)
			e.refund = refundSnap
			return nil, gas, ErrOutOfGas
		}
		gas -= precompile.RequiredGas(input)
		output, err := precompile.Run(input)
		if err != nil {
			e.StateDB.Revert(snapshot)
			e.refund = refundSnap
			return nil, gas, nil
		}
		return output, gas, nil
	}

	code := e.StateDB.GetCode(addr)
	if len(code) == 0 {
		return nil, gas, nil
	}

	contract := newContract(caller, addr, value, gas)
	contract.setCode(code)
	contract.Input = input

	in := newInterpreter(e, false)
	e.depth++
	output, err := in.Run(contract, input, false)
	e.depth--
	gas = contract.Gas

	if err != nil {
		// A reverted or halted child frame loses everything it did,
		// including the gas refunds it accumulated.
		e.StateDB.Revert(snapshot)
		e.refund = refundSnap
	}
	return output, gas, err
}

// CallCode runs code of addr in the caller's storage context.
func (e *EVM) CallCode(caller, addr common.Address, input []byte, gas uint64, value *big.Int) ([]byte, uint64, error) {
	// CALLCODE executes addr's code but inside the caller's account context.
	if e.depth >= MaxCallDepth {
		return nil, gas, ErrDepthLimitExceeded
	}
	if value == nil {
		value = new(big.Int)
	}
	if e.StateDB.GetBalance(caller).Cmp(value) < 0 {
		return nil, gas, ErrInsufficientBalance
	}

	snapshot := e.StateDB.Snapshot()
	refundSnap := e.refund
	if err := e.transfer(caller, caller, value); err != nil {
		e.StateDB.Revert(snapshot)
		e.refund = refundSnap
		return nil, gas, err
	}

	code := e.StateDB.GetCode(addr)
	if len(code) == 0 {
		return nil, gas, nil
	}
	contract := newContract(caller, caller, value, gas)
	contract.setCode(code)
	contract.Input = input

	in := newInterpreter(e, false)
	e.depth++
	output, err := in.Run(contract, input, false)
	e.depth--
	gas = contract.Gas
	if err != nil {
		e.StateDB.Revert(snapshot)
		e.refund = refundSnap
	}
	return output, gas, err
}

// DelegateCall preserves caller and value of the parent frame.
// DelegateCall executes addr's code while preserving the parent frame's
// caller address, value, storage context and read-only mode.
func (e *EVM) DelegateCall(parent *Contract, addr common.Address, input []byte, gas uint64, readOnly bool) ([]byte, uint64, error) {
	if e.depth >= MaxCallDepth {
		return nil, gas, ErrDepthLimitExceeded
	}

	snapshot := e.StateDB.Snapshot()
	refundSnap := e.refund

	if precompile, ok := e.precompiles[addr]; ok {
		if gas < precompile.RequiredGas(input) {
			e.StateDB.Revert(snapshot)
			e.refund = refundSnap
			return nil, gas, ErrOutOfGas
		}
		gas -= precompile.RequiredGas(input)
		output, err := precompile.Run(input)
		if err != nil {
			e.StateDB.Revert(snapshot)
			e.refund = refundSnap
			return nil, gas, nil
		}
		return output, gas, nil
	}

	code := e.StateDB.GetCode(addr)
	if len(code) == 0 {
		return nil, gas, nil
	}
	contract := newContract(parent.CallerAddress, parent.Address(), parent.Value(), gas)
	contract.setCode(code)
	contract.Input = input

	in := newInterpreter(e, false)
	e.depth++
	output, err := in.Run(contract, input, readOnly)
	e.depth--
	gas = contract.Gas
	if err != nil {
		e.StateDB.Revert(snapshot)
		e.refund = refundSnap
	}
	return output, gas, err
}

// StaticCall executes without any state mutation allowed.
func (e *EVM) StaticCall(caller, addr common.Address, input []byte, gas uint64) ([]byte, uint64, error) {
	if e.depth >= MaxCallDepth {
		return nil, gas, ErrDepthLimitExceeded
	}

	snapshot := e.StateDB.Snapshot()
	refundSnap := e.refund
	if precompile, ok := e.precompiles[addr]; ok {
		if gas < precompile.RequiredGas(input) {
			e.StateDB.Revert(snapshot)
			e.refund = refundSnap
			return nil, gas, ErrOutOfGas
		}
		gas -= precompile.RequiredGas(input)
		output, err := precompile.Run(input)
		if err != nil {
			e.StateDB.Revert(snapshot)
			e.refund = refundSnap
			return nil, gas, nil
		}
		return output, gas, nil
	}

	code := e.StateDB.GetCode(addr)
	if len(code) == 0 {
		return nil, gas, nil
	}
	contract := newContract(caller, addr, new(big.Int), gas)
	contract.setCode(code)
	contract.Input = input

	in := newInterpreter(e, true)
	e.depth++
	output, err := in.Run(contract, input, true)
	e.depth--
	gas = contract.Gas
	if err != nil {
		e.StateDB.Revert(snapshot)
		e.refund = refundSnap
	}
	return output, gas, err
}

// Create executes init code and deploys the returned runtime code.
func (e *EVM) Create(caller common.Address, code []byte, gas uint64, value *big.Int) ([]byte, common.Address, uint64, error) {
	return e.create(caller, code, gas, value, common.Hash{}, false)
}

// Create2 deploys at a salted deterministic address.
func (e *EVM) Create2(caller common.Address, code []byte, gas uint64, endowment *big.Int, salt *big.Int) ([]byte, common.Address, uint64, error) {
	saltHash := common.BytesToHash(new(big.Int).And(salt, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))).Bytes())
	return e.create(caller, code, gas, endowment, saltHash, true)
}

func (e *EVM) create(caller common.Address, initCode []byte, gas uint64, value *big.Int, salt common.Hash, isCreate2 bool) ([]byte, common.Address, uint64, error) {
	if e.depth >= MaxCallDepth {
		return nil, common.Address{}, gas, ErrDepthLimitExceeded
	}
	if value == nil {
		value = new(big.Int)
	}
	if e.StateDB.GetBalance(caller).Cmp(value) < 0 {
		return nil, common.Address{}, gas, ErrInsufficientBalance
	}

	var addr common.Address
	if isCreate2 {
		addr = create2Address(caller, salt, common.Keccak256Hash(initCode))
	} else {
		nonce := e.StateDB.GetNonce(caller)
		addr = createAddress(caller, nonce)
	}

	snapshot := e.StateDB.Snapshot()
	refundSnap := e.refund
	if !isCreate2 {
		// The nonce increment is itself reverted when creation fails.
		e.StateDB.SetNonce(caller, e.StateDB.GetNonce(caller)+1)
	}

	// A contract may not deploy on top of existing code or non-empty storage.
	if e.StateDB.Exist(addr) && (!e.StateDB.Empty(addr) || len(e.StateDB.GetCode(addr)) > 0) {
		e.StateDB.Revert(snapshot)
		e.refund = refundSnap
		return nil, common.Address{}, gas, ErrCreateCollision
	}
	e.StateDB.CreateAccount(addr)

	if err := e.transfer(caller, addr, value); err != nil {
		e.StateDB.Revert(snapshot)
		e.refund = refundSnap
		return nil, common.Address{}, gas, err
	}

	contract := newContract(caller, addr, value, gas)
	contract.setCode(initCode)

	in := newInterpreter(e, false)
	e.depth++
	ret, err := in.Run(contract, nil, false)
	e.depth--
	gas = contract.Gas

	if err != nil {
		e.StateDB.Revert(snapshot)
		e.refund = refundSnap
		return nil, common.Address{}, gas, err
	}

	depositCost := GasCodeDeposit * uint64(len(ret))
	if gas < depositCost {
		e.StateDB.Revert(snapshot)
		e.refund = refundSnap
		return nil, common.Address{}, gas, ErrOutOfGas
	}
	gas -= depositCost
	e.StateDB.SetCode(addr, ret)
	return ret, addr, gas, nil
}
