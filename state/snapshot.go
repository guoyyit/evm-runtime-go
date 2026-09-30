package state

// Snapshot is a named durable snapshot exposed to callers of the runtime.
// In this package it is represented by the on-disk Store revision id.
type Snapshot struct {
	ID   int
	Note string
}
