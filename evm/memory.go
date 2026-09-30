package evm

import "fmt"

// Memory is the volatile, word-addressable linear memory of one call frame.
// It grows in 32-byte words and always reads as zero before being written.
type Memory struct {
	store []byte
}

// NewMemory returns an empty memory.
func NewMemory() *Memory {
	return &Memory{}
}

// Resize grows (never shrinks) memory to exactly size bytes, zero-filling.
func (m *Memory) Resize(size uint64) {
	if size <= uint64(len(m.store)) {
		return
	}
	grown := make([]byte, size)
	copy(grown, m.store)
	m.store = grown
}

// Set copies value to memory starting at offset, after bounds are ensured.
func (m *Memory) Set(offset uint64, value []byte) {
	if uint64(len(value)) == 0 {
		return
	}
	end := offset + uint64(len(value))
	if end > uint64(len(m.store)) {
		panic(fmt.Sprintf("memory write out of bounds: %d > %d", end, len(m.store)))
	}
	copy(m.store[offset:end], value)
}

// Set8 writes a single byte at offset.
func (m *Memory) Set8(offset uint64, value byte) {
	if offset >= uint64(len(m.store)) {
		panic(fmt.Sprintf("memory write out of bounds: %d >= %d", offset, len(m.store)))
	}
	m.store[offset] = value
}

// Get returns a copy of size bytes starting at offset; untouched bytes read 0.
func (m *Memory) Get(offset, size uint64) []byte {
	if size == 0 {
		return nil
	}
	out := make([]byte, size)
	end := offset + size
	if end > uint64(len(m.store)) {
		panic(fmt.Sprintf("memory read out of bounds: %d > %d", end, len(m.store)))
	}
	copy(out, m.store[offset:end])
	return out
}

// Len returns the current byte length (always a multiple of 32).
func (m *Memory) Len() uint64 { return uint64(len(m.store)) }

// toWordSize rounds byte length up to a whole 32-byte word count.
func toWordSize(size uint64) uint64 {
	return (size + 31) / 32
}

// memoryGas returns the cumulative gas cost of memory at the given word size:
// 3 gas per word plus words^2 / 512.
func memoryGas(words uint64) uint64 {
	return 3*words + words*words/512
}

// expansionCost returns the *additional* gas needed so that memory covers the
// range [offset, offset+size), given its current length. Overflow of the
// offset+size addition is reported as a huge cost so the caller goes OOG.
func expansionCost(currentSize, offset, size uint64) (uint64, bool) {
	if size == 0 {
		return 0, true
	}
	end, overflow := addOverflow(offset, size)
	if overflow {
		return 0, false
	}
	newSize := toWordSize(end) * 32
	if newSize <= currentSize {
		return 0, true
	}
	newCost := memoryGas(toWordSize(newSize))
	oldCost := memoryGas(toWordSize(currentSize))
	return newCost - oldCost, true
}

// requireMemory grows memory as needed and deducts the incremental expansion
// gas from *gasLeft. It returns false (without mutating) when gas is short
// or the access overflows.
func requireMemory(m *Memory, gasLeft *uint64, offset, size uint64) bool {
	cost, ok := expansionCost(m.Len(), offset, size)
	if !ok || cost > *gasLeft {
		return false
	}
	if size != 0 {
		end := offset + size
		newSize := toWordSize(end) * 32
		m.Resize(newSize)
	}
	*gasLeft -= cost
	return true
}

// addOverflow reports whether a+b overflows uint64.
func addOverflow(a, b uint64) (uint64, bool) {
	sum := a + b
	if sum < a {
		return 0, true
	}
	return sum, false
}
