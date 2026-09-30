# Execution semantics

This document records the supported opcodes, resource-metering rules, error
semantics and determinism guarantees of the runtime.

## Word and data model

- Every stack element is a 256-bit unsigned integer (`math/big.Int`, reduced
  modulo 2^256). Signed operations (`SDIV`, `SMOD`, `SLT`, `SGT`, `SAR`,
  `SIGNEXTEND`) use two's-complement interpretation.
- Memory is byte-addressed, zero-initialized, and grows in 32-byte words.
- Storage is a per-account `bytes32 → bytes32` map, strictly isolated by
  account address.
- Calls carry an optional 256-bit wei value that is transferred atomically.

## Supported opcodes

| Range | Opcodes |
| --- | --- |
| Stop/arithmetic (0x00–0x0b) | `STOP ADD MUL SUB DIV SDIV MOD SMOD ADDMOD MULMOD EXP SIGNEXTEND` |
| Comparison/bitwise (0x10–0x1d) | `LT GT SLT SGT EQ ISZERO AND OR XOR NOT BYTE SHL SHR SAR` |
| Hashing (0x20) | `SHA3` (Keccak-256) |
| Context (0x30–0x3e) | `ADDRESS BALANCE ORIGIN CALLER CALLVALUE CALLDATALOAD CALLDATASIZE CALLDATACOPY CODESIZE CODECOPY GASPRICE EXTCODESIZE EXTCODECOPY RETURNDATASIZE RETURNDATACOPY` |
| Block (0x40–0x48) | `BLOCKHASH COINBASE TIMESTAMP NUMBER DIFFICULTY GASLIMIT CHAINID SELFBALANCE BASEFEE` |
| Stack/memory/storage/flow (0x50–0x5f) | `POP MLOAD MSTORE MSTORE8 SLOAD SSTORE JUMP JUMPI PC MSIZE GAS JUMPDEST PUSH0` |
| Push/Dup/Swap (0x60–0x9f) | `PUSH1..PUSH32`, `DUP1..DUP16`, `SWAP1..SWAP16` |
| Logs (0xa0–0xa4) | `LOG0..LOG4` |
| System (0xf0–0xff) | `CREATE CALL CALLCODE RETURN DELEGATECALL CREATE2 STATICCALL REVERT INVALID SELFDESTRUCT` |

`BLOCKHASH` returns a deterministic keccak-derived pseudo-hash for the 256
most recent block numbers and zero otherwise (no external block history is
available). Unrecognized opcodes and `INVALID` (0xfe) are rejected.

## Gas metering

Costs follow the Berlin-era schedule (EIP-2929 warm/cold access, EIP-2200/3529
storage metering, EIP-150 call gas cap):

- **Static tier:** 1 (`JUMPDEST`), 2 (most environment/info ops), 3
  (ADD/SUB/comparison/bitwise/PUSH/DUP/SWAP/POP), 5 (`MID`), 8
  (MUL/DIV/MOD/...), 700 (cold account access), 800 (cold `SLOAD`).
- **Memory expansion:** words `w = ceil(size/32)`, cumulative
  `3·w + w²/512`; only the incremental cost is charged as memory grows.
- **`SHA3`:** 30 + 6 per word of input + memory expansion.
- **`EXP`:** 10 + 50 per byte of the exponent.
- **Copy ops** (`CALLDATACOPY/CODECOPY/RETURNDATACOPY/EXTCODECOPY`): 3 per
  word copied + memory expansion.
- **`LOGn`:** 375 + 375·n topics + 8 per data byte + memory expansion.
- **`SLOAD`:** 800 cold / 100 warm.
- **`SSTORE` (EIP-2200):** 20000 for zero→nonzero, 5000 (or 100) for reset /
  no-op, plus cold-slot surcharge; clearing a slot queues a 4800 refund that
  is capped at half the total gas used (EIP-3529).
- **Calls:** 700 cold / 100 warm account access, 9000 value-transfer
  premium, 25000 new-account premium (plain `CALL` only), 2300 stipend added
  to value calls, and the forwarded gas is capped at
  `remaining − remaining/64`. Memory expansion is charged for argument and
  return-data regions.
- **Creation:** `CREATE` 32000; `CREATE2` 32000 + 6 per init-code word. The
  constructor forwards the 63/64-capped allowance; runtime code deposit costs
  200 per byte and failing to afford it aborts creation (`ErrCodeStoreOOG`).
- **`SELFDESTRUCT`:** 5000 (+25000 new-account premium when balance is sent
  to a fresh account); the account is removed at commit.

Out-of-gas consumes all remaining gas of the failing frame. Exceptional
failures inside a child call burn the gas forwarded to that child; `REVERT`
also returns unspent gas to the caller (the caller observes success flag 0
and may continue).

## Errors

All failures are distinguishable sentinel values in the `evm` package:

| Error | Cause |
| --- | --- |
| `ErrInvalidOpCode` | Unknown opcode or explicit `INVALID` |
| `ErrInvalidJump` | Jump target is not a JUMPDEST (or out of range / non-push byte) |
| `ErrStackOverflow` | Push/DUP beyond 1024 elements |
| `ErrStackUnderflow` | Pop/peek/DUP/SWAP with too few elements |
| `ErrOutOfGas` | Static, dynamic, memory or forwarded gas exceeded |
| `ErrCallDepth` | Nested call/creation depth reached 1024 |
| `ErrInsufficientBalance` | Value transfer with insufficient sender balance |
| `ErrWriteInReadOnly` | State mutation attempted in a `STATICCALL`/static frame |
| `ErrExecutionReverted` | Explicit `REVERT`; `Result.RevertData` holds the payload |
| `ErrReturnDataOOB` | `RETURNDATACOPY` reads beyond the last return data |
| `ErrCodeStoreOOG` | Not enough gas left to pay the 200/byte code deposit |
| `ErrInvalidMemAccess` | Invalid memory indexing (overflow) |
| `ErrUnknownRevision` | `RevertTo` of an unknown snapshot id |

Errors never cause partial commits: the whole working state is discarded.

## Calls, depth and value

- `CALL` transfers value and runs the target's code in the target's account
  context; `CALLCODE` runs target code in the caller's account context;
  `DELEGATECALL` additionally inherits caller and value; `STATICCALL` is
  read-only and cannot carry value.
- A failed child call reverts only that child's journaled changes and pushes
  0; successful calls push 1 and copy return data (a short copy is allowed).
- Depth is capped at 1024; reaching it yields `ErrCallDepth`.
- Unused forwarded gas is always restored to the parent frame.

## Creation

- Legacy `CREATE` derives `keccak256(rlp([sender, nonce]))[12:]` and increments
  the sender nonce first; `CREATE2` derives
  `keccak256(0xff ‖ sender ‖ salt ‖ keccak256(initcode))[12:]`.
- Init code and the deployed runtime code are separate: the constructor's
  returned bytes become the runtime code; init code is never callable.
- Failed creation leaves no code and no balance at the new address and rolls
  back every constructor change; its gas accounting is independent.

## Persistence and isolation

- Committed state is written to a JSON file atomically via temp-file rename;
  opening the file again fully restores balances, nonces, code, storage and
  event logs.
- A `State` is a deep copy: uncommitted changes are invisible to any other
  execution and to committed readers until `Commit` succeeds.
- `Snapshot()/Revert(id)` on a working state restores account creation,
  balance, nonce, code and storage changes in reverse journal order and can be
  taken at any execution point; re-running identical inputs after a revert
  reproduces the first run exactly.

## Determinism

- Fixed block environment (number/time/chain id/base fee) and no wall-clock or
  random source in execution.
- Map iteration never affects results; ordering-sensitive outputs (logs,
  journal entries) use slices.
- Addresses and hashes derive solely from inputs via RLP + Keccak-256.
- The runtime is single-process and uses no network services.
- Concurrent executions operate on disjoint working states and converge to
  the same committed result for the same initial state and messages,
  irrespective of interleaving.
