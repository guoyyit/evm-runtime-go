# evm-runtime-go

一个纯本地、进程内的以太坊虚拟机（EVM）核心语义执行环境。它可以加载并执行
合约字节码、按操作码与内存扩展精确计量 Gas、维护可持久化的账户/余额/存储状态，
并保证在相同初始状态与输入下得到完全一致的结果。仅依赖 Go 标准库与
`golang.org/x/crypto`（Keccak-256 / RIPEMD-160），不访问任何外部网络服务。

## 包结构

- `common` — `Address`、`Hash`、256 位整数封装、Keccak-256。
- `state` — 可变世界状态、事务性快照/回滚、JSON 文件持久化（原子落盘）。
- `evm` — 操作码、解释器、栈/内存、Gas 计量、调用与合约创建、预编译合约。
- `runtime` — 进程内入口：执行/查询、原子提交、并发隔离、事件日志读取。

## 快速使用

```go
rt, err := runtime.New("state.json")
if err != nil { /* ... */ }
defer rt.Close()

// 合约创建（cfg.To == nil 时执行 init code 并部署返回的 runtime code）
res, err := rt.Execute(runtime.Config{
    From:     deployer,
    Data:     initBytecode,
    GasLimit: 10_000_000,
    Block:    evm.BlockContext{Number: big.NewInt(1), Timestamp: 1700000000},
})

// 普通消息调用
res, err = rt.Execute(runtime.Config{
    From: caller, To: &contract, Value: big.NewInt(1000),
    Data: calldata, GasLimit: 1_000_000,
})

// 只读查询（永不提交，且禁止任何写操作）
q, err := rt.Query(runtime.Config{From: caller, To: &contract, GasLimit: 1_000_000})
```

执行结果 `runtime.Result` 字段：

- `ReturnData` 返回数据（`REVERT` 时为 revert payload）
- `GasUsed` / `GasLeft` 实际消耗与剩余
- `Failed` / `Err` 可区分的失败原因（见下）
- `Logs` 本次提交产生的事件
- `Address` 创建出的合约地址

失败的执行不会提交任何状态；调用方通过 `errors.Is(res.Err, evm.ErrXxx)`
判断具体原因。

## 支持的操作码

算术与位运算：

- `STOP ADD MUL SUB DIV SDIV MOD SMOD ADDMOD MULMOD EXP SIGNEXTEND`
- `LT GT SLT SGT EQ ISZERO AND OR XOR NOT BYTE SHL SHR SAR`
- `KECCAK256`

环境与数据：

- `ADDRESS BALANCE ORIGIN CALLER CALLVALUE GASPRICE`
- `CALLDATALOAD CALLDATASIZE CALLDATACOPY`
- `CODESIZE CODECOPY EXTCODESIZE EXTCODECOPY EXTCODEHASH`
- `RETURNDATASIZE RETURNDATACOPY`
- `BLOCKHASH COINBASE TIMESTAMP NUMBER DIFFICULTY GASLIMIT CHAINID`
  `SELFBALANCE BASEFEE`

栈、内存、存储、控制流：

- `POP MLOAD MSTORE MSTORE8 SLOAD SSTORE`
- `JUMP JUMPI PC MSIZE GAS JUMPDEST`
- `PUSH1..PUSH32 DUP1..DUP16 SWAP1..SWAP16`

事件、调用、创建、系统：

- `LOG0..LOG4`
- `CALL CALLCODE DELEGATECALL STATICCALL`
- `CREATE CREATE2 RETURN REVERT INVALID SELFDESTRUCT`

预编译合约（地址 0x01–0x04）：

- `ecrecover`（手写 secp256k1 公钥恢复）
- `sha256`、`ripemd160`、`identity`

未列出的字节（如 `0x0c..0x0f`、`0x5c..0x5f`、`0xa5..0xef` 等）一律视为
`INVALID` 并产生 `ErrInvalidOpcode`，不会被静默跳过。

## 资源计量（Gas）

- 每个操作码先扣除静态基础费，再扣除依赖操作数的动态费用。
- 内存按 32 字节字对齐分配，扩展费用采用
  `3 * words + words^2 / 512`，且只对新增部分计费。
- 拷贝类（`*COPY`、`CALLDATACOPY`、`CODECOPY`、`RETURNDATACOPY`）
  按 `3 * ceil(size/32)` 计费；`KECCAK256` 额外按 `6/字`；
  `EXP` 按指数字节 `50/字节` 计费。
- 存储采用 EIP-2929 冷/热访问与 EIP-2200 动态费用：
  冷账户访问 2600、热访问 100；`SSTORE` 新建槽 20000、改写 5000，
  无变化写 100（净 Gas 规则）。将非零槽清零按 EIP-3529 给予 4800
  退款；写回原值补偿此前差价。退款在成功执行结束时按实际消耗的 1/5
  封顶返还，失败执行不退款。
- 内存偏移 + 长度以 uint64 计算，任何溢出立即产生
  `ErrGasUintOverflow`，不会发生静默环绕或越界访问。
- 调用：基础 700，带值附加 9000 并向被调用方发放 2300 stipend，
  新建账户附加 25000；转发给子调用的 Gas 受 63/64 规则限制，
  未用完的 Gas 无论子调用成败都会归还父帧。
- `CREATE` 32000，`CREATE2` 额外 `6/字`；部署代码按 200/字节收取。
- 最大调用深度 1024。

### 隐式结束

程序计数器越过最后一条指令（包括末尾 `PUSH` 的数据字节被截断的
情况）时，执行按**隐式 `STOP`** 处理：正常成功结束，不产生异常，
不额外扣 Gas（与显式 `STOP` 的 0 费用一致），已产生的状态变更与
资源消耗和显式终止的程序完全相同。空字节码同样立即成功返回。

### 失败与回退时的计量

`Result.GasUsed` / `Result.GasLeft` 永远报告**实际消耗**，不会统一
记为预算上限，且相同输入下结果稳定可复现：

- `REVERT`：消耗到回退点为止的 Gas，剩余 Gas 照常返还
  （`GasLeft > 0`），revert payload 随 `Result.ReturnData` 返回。
- Gas 耗尽（含内存扩展失败、Gas uint64 溢出）：该帧预算全部耗尽，
  `GasLeft == 0`、`GasUsed == GasLimit`。
- 非法指令、非法跳转、栈上/下溢、只读写违规等其他故障：报告执行到
  故障指令为止的实际消耗，剩余 Gas 返还。
- 余额不足、深度超限等在执行任何指令前发生的失败：消耗为 0。

任一层执行失败都会整体原子中止，回滚该执行的全部状态修改，
失败原因通过 `Result.Err` 以可区分的哨兵错误报告。

## 错误语义

所有失败都返回可区分的哨兵错误，可用 `errors.Is` 判定：

- `ErrInvalidOpcode` 非法操作码
- `ErrInvalidJump` 跳转到非 `JUMPDEST` 目标
- `ErrStackOverflow` / `ErrStackUnderflow` 栈上溢/下溢
- `ErrOutOfGas` Gas 耗尽（含内存扩展、Gas uint64 溢出）
- `ErrDepthLimitExceeded` 调用深度超过 1024
- `ErrInsufficientBalance` 带值调用时余额不足
- `ErrWriteInReadOnly` 只读上下文中执行写操作
- `ErrReturnDataOutOfBound` `RETURNDATACOPY` 越界
- `ErrExecutionReverted` 合约 `REVERT`（payload 见 `evm.RevertData(err)`）
- `ErrCreateCollision` 创建地址已存在非空合约

注意区分：`REVERT` 是合约主动回滚（携带数据、消耗已用 Gas），
其余为 EVM 级故障。子调用失败以 0 压栈供父合约处理，
但子调用内的状态修改会被回滚；只有父帧自身中止才导致整笔交易失败。

## 事务性快照与回滚

`state.StateDB.Snapshot()` 可在执行的任意时点建立回滚点，
`Revert(id)` 完整恢复此后的全部可变状态，保证范围：

- 账户余额、nonce、合约代码与代码哈希；
- 已写入的存储槽（包括新增槽的移除）；
- 账户的存在性——无论是 `CreateAccount` 显式创建，还是转账/写存储
  时隐式创建的账户，回滚后都恢复到快照时点的（不）存在状态；
- 访问列表（冷/热标记）、事件日志、自毁标记，以及提交用的
  touched 账户/槽集合（回滚后不会向存储层提交幻影写入）。

执行栈与内存是每个调用帧私有的临时状态：子调用失败时其栈与内存随
帧一起销毁，父帧的栈与内存自始不受子帧影响，因此无需也无法回滚；
在相同输入下重新执行时，每个帧都会重建完全一致的栈与内存。

回滚后用相同输入重新执行，每一步的资源消耗（含依赖账户存在性的
新建账户费、依赖访问列表的冷/热费用）与最终状态都与首次执行完全
一致。

## 持久化与隔离

- 世界状态以单个 JSON 文档落盘，提交采用 `fsync + 原子 rename`，
  跨重启可完整读取账户、余额、nonce、代码、存储与事件日志。
- 每次执行基于提交状态构建独立的工作副本；未提交的修改对其他执行不可见。
- 提交在存储锁内原子地合并到最新已提交状态（合并与落盘是一个临界区），
  因此多个并发执行可以安全地交错提交，且合并语义与提交顺序无关（见下）。

## 并发与确定性

- 各执行拥有独立的 `StateDB`/栈/内存，互不共享可变状态。
- 提交按**增量**合并，保证同一账户上的并发提交不丢失更新：
  - 余额与 nonce 以相对执行基线的**有符号增量**合并，加法可交换，
    因此并发转账/扣款的最终结果与串行执行的理论值一致，
    与交错方式、提交顺序无关；
  - 存储槽按键逐槽合并（同一槽并发写时后提交者覆盖，等价于某种
    串行顺序）；合约代码仅在本执行真正部署过时才参与合并，
    避免无关提交覆盖他人部署的代码；
  - 若增量合并会使账户余额或 nonce 变为负数（并发透支），该次提交
    被拒绝并返回 `state.ErrBalanceUnderflow` /
    `state.ErrNonceUnderflow`，已提交状态不受影响——等价于串行
    执行中该笔交易因余额不足而失败。
- 相同初始状态与输入，无论并发执行顺序如何，提交后最终状态一致；
  `TestReplayDeterminism` 通过逐字节比对两次独立运行的状态文件验证，
  `TestConcurrentSameAccountCommits` /
  `TestConcurrentSameAccountStorage` 以数百次并发提交校验同一账户的
  余额与存储等于串行理论值。
- 环境数据（区块号、时间戳、chainID 等）必须由调用方在 `BlockContext`
  中显式提供；未知历史区块的 `BLOCKHASH` 确定性返回零，不依赖任何
  外部随机源或网络。

### 验证方法

```sh
go test -race ./...
```

带竞态检测的全量测试必须无数据竞争报告；并发一致性用例不依赖
偶然时序，重复执行（如 `-count=5`）结果稳定。

## 测试

```sh
go test ./...
go test -race ./...   # 并发场景须带竞态检测
```

覆盖：算术/位运算、非法操作码、非法跳转、栈上/下溢、Gas 耗尽与
精确计量、内存扩展、SSTORE 冷/热费用、调用深度超限、嵌套调用回滚、
余额不足、REVERT 数据、只读写入违规、CREATE/CREATE2 地址（含独立
Keccak 向量交叉验证）、ecrecover 往返、快照/回滚、重启恢复、
并发交错确定性以及 200 组随机字节码（要求不 panic、返回可判定错误）。
另含一个 Go fuzz 目标 `FuzzBytecodeNeverPanics`，已用数千个随机输入验证
不 panic；以及存储清零退款、内存偏移溢出（`ErrGasUintOverflow`）等边界。

针对边界与并发语义的专项验收：

- 隐式结束：无 `STOP`/`RETURN`/`REVERT` 结尾的字节码（含末尾 `PUSH`
  截断、无终止指令的 init code）按成功处理，Gas 与状态变更和显式
  终止的孪生程序逐项一致（`TestImplicitHaltSuccess` /
  `TestImplicitHaltCreate`）。
- 失败计量：`REVERT` 报告实际消耗而非预算上限，OOG 耗尽全部预算，
  非法指令/栈异常按实际消耗报告且彼此可区分，相同输入重复执行计量
  稳定（`TestRevertGasMetering` / `TestFailureGasMetering`）。
- 回滚完整性：异构变更（余额/nonce/代码/存储/访问列表/日志/隐式
  创建账户/touched 集合）回滚后完整复原，重放结果与首次一致；
  嵌套回滚后的提交不泄漏幻影账户（`TestRollbackRestoresFullState` /
  `TestRollbackRestoresImplicitAccounts` /
  `TestNestedRevertRollbackConsistency` / `TestRetouchAfterRollback`）。
- 并发提交：同一账户 400 次并发转账提交与 300 次并发存储写入，
  最终余额/存储等于串行理论值（`TestConcurrentSameAccountCommits` /
  `TestConcurrentSameAccountStorage`），并由 `go test -race ./...`
  保证无数据竞争。
