# evm-runtime-go

A self-contained, Ethereum-Virtual-Machine-compatible bytecode execution
environment written in pure Go (standard library only). It loads and runs
contract bytecode, meters resource use per opcode, persists accounts, balances
and storage across restarts, and produces identical results for identical
inputs and initial state.

There is no network access: every dependency is part of the Go standard
library (Keccak-256 is implemented from scratch in `crypto/`).

## Packages

- `state` — committed world state, isolated transactional working copies,
  journaled snapshots/revert, JSON-file persistence (atomic write), logs.
- `crypto` — Keccak-256 (`SHA3` opcode, CREATE/CREATE2 addressing).
- `evm` — opcode definitions and gas table, stack/memory/frames, interpreter
  loop, CALL/CREATE lifecycle and the in-process `Runtime` entry point.

## Quick start

```go
rt, err := evm.Open("/var/lib/evm/state.json") // "" for in-memory
defer rt.Close()

// Privileged genesis-style funding.
rt.Mint(sender, big.NewInt(1_000_000))

// Contract creation: nil To means CREATE; Data is the init code.
res, _ := rt.Execute(evm.Message{
    From:     sender,
    To:       nil,
    Data:     initBytecode,
    GasLimit: 200_000,
})
if res.Err != nil { /* res.Err is a distinguishable sentinel error */ }
addr := *res.CreatedAddress

// Message call.
res, _ = rt.Execute(evm.Message{From: sender, To: &addr, GasLimit: 100_000})

// Read-only request (any SSTORE/LOG/value-CALL returns ErrWriteInReadOnly).
view, _ := rt.ExecuteStatic(sender, addr, calldata, 100_000)

rt.Balance(addr); rt.StorageAt(addr, key); rt.Code(addr); rt.QueryLogs(&addr)
```

## Guarantees at a glance

- **Deterministic execution:** fixed block/tx environment (no wall clock, no
  maps driving control flow), big.Int 256-bit words, canonical RLP/Keccak
  addresses. Identical initial state + message ⇒ identical result, gas, logs,
  and committed state, regardless of scheduling.
- **Atomicity:** each request runs on a deep-copied working state. Any failure
  (out of gas, revert, bad jump, stack fault, depth, insufficient balance)
  discards the whole working copy; the committed store is never touched.
- **Nested budget:** CALL/CREATE share one gas pool under the EIP-150 63/64
  rule; unspent child gas is returned, the value-call stipend is granted, and
  call depth is capped at 1024.
- **Persistence:** balances, nonces, code and storage are persisted to JSON
  atomically (temp file + rename) and survive process restarts.
- **Snapshots:** both committed-store revisions (`Runtime.Snapshot/RevertTo`)
  and arbitrary in-execution working-state revision points
  (`state.State.Snapshot/Revert`) restore stack-transitive balance/nonce/code/
  storage/account-creation changes.
- **Concurrency:** every execution owns an independent working state; the
  short read-commit section is serialized, so concurrent identical requests
  never overwrite one another.

See [`docs/SEMANTICS.md`](docs/SEMANTICS.md) for the opcode list, gas rules,
  error catalogue and determinism notes.

## Tests

```sh
go test ./...          # full suite
go test -race ./...    # concurrency / data-race checks
```

Coverage includes resource exhaustion, call-depth cap, insufficient balance,
revert rollback, restart recovery, concurrent interleaving, read-only write
violations, random bytecode fuzzing, CREATE2, SELFDESTRUCT and refunds.
