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

任一层执行超出预算都会整体原子中止，回滚该执行的全部状态修改，
并通过 `Result.GasUsed/GasLeft/Err` 报告实际消耗与失败原因。

### 结束方式与 Gas 报告规则

- **显式终止**：`STOP` / `RETURN` 正常结束，返回未消耗的 Gas。
- **隐式终止**：程序计数器越过最后一条指令（代码未以终止指令结尾）
  属于合法执行，语义等同于 `STOP`：成功结束、无返回数据，
  资源消耗与状态变更和显式终止完全一致，不会抛出运行时异常。
  操作数被截断的尾部 `PUSH` 以零填充后同样自然结束。
- **`REVERT`**：合约主动回滚，报告**实际已消耗**的 Gas
  （`GasUsed = GasLimit - GasLeft`，剩余 Gas 返还），并携带 revert payload。
- **异常终止**（`ErrOutOfGas`、`ErrInvalidOpcode`、`ErrInvalidJump`、
  栈上溢/下溢、`ErrWriteInReadOnly`、`ErrReturnDataOutOfBound` 等）：
  按 EVM 规则耗尽当前帧全部剩余 Gas，顶层表现为 `GasUsed == GasLimit`。
- **执行前失败**（余额不足、深度超限等尚未运行任何指令的失败）：
  `GasUsed == 0`，`GasLeft == GasLimit`。

任何失败都按上述规则报告**实际消耗**，不会统一记为预算上限；
相同初始状态与相同输入下报告的消耗稳定可复现，且
`GasUsed + GasLeft == GasLimit` 恒成立。

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
`Revert(id)` 完整恢复此后的全部变更，保证范围覆盖：

- 账户余额、nonce、代码与已写入的存储项；
- 事件日志与 EIP-2929 访问列表（热/冷状态决定后续 Gas 计费）；
- 提交标记：被回滚的写入不会留下任何待提交痕迹，
  因此回滚后的提交不会用旧值覆盖并发提交的新值；
- Gas 退款计数器：子调用/创建被回滚时，其内部累积的
  SSTORE 清零退款一并撤销，不会泄漏到上层帧。

执行栈与内存按帧隔离：子帧回滚时其父帧的栈与内存保持调用前状态
（仅按语义压入调用结果并写入返回数据区）。
在相同输入下，回滚后重新执行与首次执行的每一步资源消耗
及最终状态完全一致。

## 持久化与隔离

- 世界状态以单个 JSON 文档落盘，提交采用 `fsync + 原子 rename`，
  跨重启可完整读取账户、余额、nonce、代码、存储与事件日志。
- 每次执行基于提交状态构建独立的工作副本；未提交的修改对其他执行不可见。
- 提交按**增量（delta）**合并到最新已提交状态：余额与 nonce 以
  有符号差值应用，存储槽与代码按最后提交者生效，提交本身由互斥锁串行化。
  因此多个并发执行写**同一账户**也不会丢失更新：
  余额/nonce 差值可交换，合并结果与这些提交的任意串行执行一致；
  若合并会使余额或 nonce 变负，该次提交被拒绝并返回错误。
  自定义 `state.Store` 实现若不支持增量提交，则回退为整账户替换语义。

## 并发与确定性

- 各执行拥有独立的 `StateDB`/栈/内存，互不共享可变状态。
- 相同初始状态与相同输入，无论并发执行顺序如何，提交后最终状态一致；
  `TestReplayDeterminism` 通过逐字节比对两次独立运行的状态文件验证。
- 一致性保证：对同一账户的高频并发写入（余额累加、不同存储槽写入）
  在提交后完整保留，与串行执行的理论结果一致；同一存储槽的并发写
  以提交顺序决定最终值（等价于按提交顺序串行执行）。
- 验证方法：`go test -race ./...` 全量通过，无数据竞争报告；
  `TestConcurrentSameAccountTransfers` 与 `TestConcurrentSameContractCalls`
  以 300 路并发提交校验最终余额与存储的精确理论值。
- 环境数据（区块号、时间戳、chainID 等）必须由调用方在 `BlockContext`
  中显式提供；未知历史区块的 `BLOCKHASH` 确定性返回零，不依赖任何
  外部随机源或网络。

## 测试

```sh
go test ./...        # 全量测试
go test -race ./...  # 带竞态检测的全量测试（并发提交的验收方式）
```

覆盖：算术/位运算、非法操作码、非法跳转、栈上/下溢、Gas 耗尽与
精确计量、内存扩展、SSTORE 冷/热费用、调用深度超限、嵌套调用回滚、
余额不足、REVERT 数据、只读写入违规、CREATE/CREATE2 地址（含独立
Keccak 向量交叉验证）、ecrecover 往返、快照/回滚、重启恢复、
并发交错确定性以及 200 组随机字节码（要求不 panic、返回可判定错误）。
另含一个 Go fuzz 目标 `FuzzBytecodeNeverPanics`，已用数千个随机输入验证
不 panic；以及存储清零退款、内存偏移溢出（`ErrGasUintOverflow`）等边界。

针对边界与并发语义的专项测试：

- 隐式终止：未以终止指令结尾的字节码（含被截断的尾部 PUSH、
  跳转到末尾 `JUMPDEST` 后自然结束、空 init code）按成功处理，
  Gas 与状态变更和显式 `STOP` 完全一致。
- 失败资源计量：`REVERT` 报告实际消耗（与等价的 `RETURN` 程序一致），
  异常终止耗尽帧内 Gas，执行前失败消耗为零，且同输入重复执行结果稳定。
- 快照回滚一致性：回滚完整恢复余额/nonce/代码/存储/日志/访问列表与
  提交标记，子帧回滚撤销其退款；回滚后重放与首次执行逐步一致。
- 高频并发：300 路并发对同一账户转账与对同一合约的带值调用，
  提交后余额与全部存储槽与串行理论值精确一致（`-race` 下验证）。
