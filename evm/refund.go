package evm

func (e *EVM) addRefund(amount uint64) {
	// Saturating add guards against overflow across many SSTOREs.
	if ^uint64(0)-e.refund < amount {
		e.refund = ^uint64(0)
		return
	}
	e.refund += amount
}

// Refund returns the accumulated gas refund for the current execution.
func (e *EVM) Refund() uint64 { return e.refund }

// applyRefund caps the refund at one fifth of the gas actually consumed and
// returns the number of gas units that should be given back.
func applyRefund(gasUsed, refund uint64) uint64 {
	cap := gasUsed / 5
	if refund > cap {
		return cap
	}
	return refund
}

// RefundAdjustment returns how much gas is given back after applying the
// one-fifth cap, given gas actually consumed.
func (e *EVM) RefundAdjustment(gasUsed uint64) uint64 {
	return applyRefund(gasUsed, e.refund)
}
