package evm

import (
	"math/big"

	"github.com/guoyyit/evm-runtime-go/common"
	"github.com/guoyyit/evm-runtime-go/state"
)

// StateDB is the slice of world-state functionality the interpreter needs.
type StateDB interface {
	GetBalance(common.Address) *big.Int
	AddBalance(common.Address, *big.Int)
	SubBalance(common.Address, *big.Int) bool
	GetNonce(common.Address) uint64
	SetNonce(common.Address, uint64)
	GetCode(common.Address) []byte
	SetCode(common.Address, []byte)
	GetCodeHash(common.Address) common.Hash
	GetCodeSize(common.Address) int
	GetState(common.Address, common.Hash) common.Hash
	SetState(common.Address, common.Hash, common.Hash)
	GetCommittedState(common.Address, common.Hash) common.Hash
	Exist(common.Address) bool
	Empty(common.Address) bool
	CreateAccount(common.Address)
	Suicide(common.Address) bool
	HasSuicided(common.Address) bool
	AddLog(state.Log)
	Logs() []state.Log
	AddAddressToAccessList(common.Address)
	AddSlotToAccessList(common.Address, common.Hash)
	IsAddressWarm(common.Address) bool
	IsSlotWarm(common.Address, common.Hash) bool
	Snapshot() int
	Revert(int)
}

// Contract is a single executing call frame.
type Contract struct {
	CallerAddress common.Address
	self          common.Address
	Code          []byte
	CodeHash      common.Hash
	Input         []byte
	Gas           uint64
	value         *big.Int
	CodeMap       map[int]bool
}

func newContract(caller, self common.Address, value *big.Int, gas uint64) *Contract {
	v := value
	if v == nil {
		v = new(big.Int)
	}
	return &Contract{
		CallerAddress: caller,
		self:          self,
		value:         v,
		Gas:           gas,
	}
}

// setCode attaches bytecode and builds the jump-destination bitmap.
func (c *Contract) setCode(code []byte) {
	c.Code = code
	c.CodeHash = common.Keccak256Hash(code)
	c.CodeMap = make(map[int]bool)
	for i := 0; i < len(code); i++ {
		op := OpCode(code[i])
		if op == JUMPDEST {
			c.CodeMap[i] = true
		}
		if op.IsPush() {
			i += op.PushSize()
		}
	}
}

func (c *Contract) validJumpdest(dest uint64) bool {
	if dest >= uint64(len(c.Code)) {
		return false
	}
	return c.CodeMap[int(dest)]
}

func (c *Contract) Address() common.Address { return c.self }

func (c *Contract) Value() *big.Int { return new(big.Int).Set(c.value) }

// caller returns the calling contract's address.
func (c *Contract) caller() common.Address { return c.CallerAddress }
