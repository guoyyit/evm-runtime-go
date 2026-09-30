package evm

import "fmt"

// Memory is the EVM volatile byte-addressable memory.
type Memory struct {
	store       []byte
	lastGasCost uint64
}

func newMemory() *Memory { return &Memory{} }

func (m *Memory) resize(size uint64) {
	if size <= uint64(len(m.store)) {
		return
	}
	if size > 0xffffffff {
		panic(fmt.Errorf("memory: requested size %d exceeds maximum", size))
	}
	// Memory is allocated in 32-byte word units.
	words := toWordSize(size)
	newStore := make([]byte, words*32)
	copy(newStore, m.store)
	m.store = newStore
}

func (m *Memory) set(offset, size uint64, value []byte) {
	if offset+size < offset {
		panic(ErrGasUintOverflow)
	}
	m.resize(offset + size)
	copy(m.store[offset:offset+size], value)
}

func (m *Memory) set32(offset uint64, val []byte) {
	if offset+32 < offset {
		panic(ErrGasUintOverflow)
	}
	m.resize(offset + 32)
	copy(m.store[offset:offset+32], val)
}

func (m *Memory) getCopy(offset, size int64) []byte {
	if offset+size < offset {
		panic(ErrGasUintOverflow)
	}
	m.resize(uint64(offset + size))
	out := make([]byte, size)
	copy(out, m.store[offset:offset+size])
	return out
}

func (m *Memory) getData(offset, size int64) []byte {
	if size == 0 {
		return nil
	}
	if offset < 0 || size < 0 || offset > int64(len(m.store)) || offset+size > int64(len(m.store)) {
		return nil
	}
	out := make([]byte, size)
	copy(out, m.store[offset:offset+size])
	return out
}

func (m *Memory) len() int { return len(m.store) }
