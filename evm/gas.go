package evm

// Gas schedule constants (post-Berlin style warm/cold access costs).
const (
	GasZero        uint64 = 0
	GasQuickStep   uint64 = 2
	GasFastestStep uint64 = 3
	GasFastStep    uint64 = 5
	GasLowStep     uint64 = 5
	GasMidStep     uint64 = 8
	GasHighStep    uint64 = 10
	GasSlowStep    uint64 = 10
	GasExtStep     uint64 = 20

	GasExpByte    uint64 = 50
	GasKeccak256  uint64 = 30
	GasKeccakWord uint64 = 6
	GasMemory     uint64 = 3
	GasCopy       uint64 = 3
	GasLog        uint64 = 375
	GasLogData    uint64 = 8
	GasLogTopic   uint64 = 375

	GasSLoadEIP2929      uint64 = 100
	GasWarmAccess        uint64 = 100
	GasColdAccountAccess uint64 = 2600
	GasAccessListAddr    uint64 = 2400
	GasAccessListSlot    uint64 = 1900

	GasSStoreSet   uint64 = 20000
	GasSStoreReset uint64 = 5000
	GasSStoreClear uint64 = 4800

	GasSelfDestruct uint64 = 5000
	GasCreate       uint64 = 32000
	GasCodeDeposit  uint64 = 200
	GasCall         uint64 = 700
	GasCallValue    uint64 = 9000
	GasCallStipend  uint64 = 2300
	GasNewAccount   uint64 = 25000

	MaxCallDepth = 1024
)

// memoryGasCost computes the gas cost of expanding memory to size bytes.
func memoryGasCost(size uint64) (uint64, error) {
	if size > ^uint64(0)-31 {
		return 0, ErrGasUintOverflow
	}
	words := toWordSize(size)
	if words > (1<<56)-1 {
		return 0, ErrGasUintOverflow
	}
	linear, ok := saturatingMul(words, GasMemory)
	if !ok {
		return 0, ErrGasUintOverflow
	}
	quadratic, ok := saturatingMul(words, words)
	if !ok {
		return 0, ErrGasUintOverflow
	}
	quadratic /= 512
	cost, ok := saturatingAdd(linear, quadratic)
	if !ok {
		return 0, ErrGasUintOverflow
	}
	return cost, nil
}

func saturatingMul(a, b uint64) (uint64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	if a > (^uint64(0))/b {
		return ^uint64(0), false
	}
	return a * b, true
}

func saturatingAdd(a, b uint64) (uint64, bool) {
	if a > ^uint64(0)-b {
		return ^uint64(0), false
	}
	return a + b, true
}

// toWordSize rounds bytes up to whole 32-byte words.
func toWordSize(size uint64) uint64 {
	return (size + 31) / 32
}

// expGasCost charges EXP per byte of the exponent.
func expGasCost(exponent []byte) uint64 {
	exponent = trimLeadingZeroBytes(exponent)
	return GasSlowStep + GasExpByte*uint64(len(exponent))
}

func trimLeadingZeroBytes(b []byte) []byte {
	i := 0
	for ; i < len(b)-1; i++ {
		if b[i] != 0 {
			break
		}
	}
	return b[i:]
}
