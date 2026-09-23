# 时差年代账（NTP era ledger）

离线 NTP 年代账：导入客户端与多个服务器之间的 NTP 报文与本地接收时间，
跨越 2036 年 32 位秒字段回绕、闰秒预告与服务器失同步阶段，全部以纯整数
fixed-point 计算，不依赖本机时钟、账号、云服务或随机网络时序。

## 构建与运行

```sh
go mod download && go build ./...
go test ./... -count=1
go run ./cmd/server --addr 127.0.0.1:5995
# 打开 http://127.0.0.1:5995 ，首页标题为「时差年代账」
```

首次启动会在空库中自动执行 SQLite 迁移并载入内置 fixture
（`--db` 可改路径，默认 `ledger.db`）。

## 数据模型

- 每个交换保留四时间戳（T1–T4）的原始 64 位字段、48 字节原始报文、
  采集时钟来源（`unix` / `tap32` 等）与本地接收时间。
- 每个交换计算 `delay`、`offset`、`dispersion`、`root distance`
  （λ = (delay + rootDelay)/2 + ε，ε 含 15 ppm 老化增长），全部为
  32.32 整数 fixed-point，无浮点误差。
- 每个 peer 维护 reach 移位寄存器（八进制显示）、最近 8 个滤波样本、
  stratum、reference id 与 era 候选。

## era 解析

- 32 位秒字段的 era **不凭当前系统时间猜测**。
- 采集记录含完整客户端时钟时，客户端 era 固定，服务器 era 由
  「与 T4 相差小于半个 era（2^31 秒）」唯一确定。
- 采集记录只有 32 位字段时（如硬件 tap），era 0..MaxEra 全部保留为候选。
- 页面可指定可信时间锚点：选择距锚点最近的候选，平局取较小 era，
  结果确定；没有锚点且存在多解时，候选全部保留、不做选择。
- delay/offset 与 era 无关（同一交换内用 ±2^31 窗口做回绕校正），
  era 只影响绝对时间定位与锚点比较。

## 闰秒策略

LI=1/2 是「预告」，不是把所有时间戳直接加一秒。三种策略只影响样本取舍：

- `strict`：剔除预告窗口内的样本；
- `permissive`：保留并在样本上打闰秒标记；
- `ignore`：完全忽略（LI=3 失同步仍然拒绝）。

## 筛选管线（固定次序，证据全部可展开）

`kod`（kiss-o'-death，记录 kiss code）→ `li-unsync`（LI=3）→
`origin-mismatch`（originate 不回声请求 T1）→ `duplicate-transmit`
（重复服务器 transmit 时间戳）→ `late-response`（迟到响应）→
`era-unresolvable`（无候选 era）→ `precision-bounds`
（精度指数越出 [-32, 24]）→ `negative-delay`（负往返时延）→
`leap-announced`（按闰秒策略）。每个过滤器对每个交换都记录判定与
说明，首个拒绝即主因；fixed-point 小数边界（0xFFFFFFFF 借位/进位）
在 `internal/ntp` 中显式处理并有测试。

## 内置 fixture

- `gps-stratum1`：回绕两侧各 3 个交换（era 0 末尾与 era 1 开头），
  客户端时钟完整，era 唯一确定。
- `tap32-only`：只有 32 位采集时钟，无锚点时保留 {era0, era1} 候选，
  给定 2037 锚点后确定选择 era 1。
- `flaky-campus`：KoD(RATE)、重复 transmit、迟到响应、LI 0→1→3 变化、
  origin 不匹配、负 delay、非法精度指数，以及 GPS→PPS→IPv4 的
  不连贯参考时钟。

## 页面

- `/` 总览：peer 状态表、锚点管理、锚点/闰秒策略切换。
- `/peer?id=N`：交换明细（筛选证据、原始字段、raw 报文可展开）、
  滤波样本表、offset/delay SVG 图（接受为点、拒绝为叉）。
- `/compare`：同一批数据在 无锚点/锚点 × strict/permissive/ignore
  下的选择结果对比。

## 测试

`go test ./... -count=1` 覆盖：整数 fixed-point 计算与小数边界、
era 候选保留与锚点下的确定性、滤波次序与拒绝证据、闰秒策略差异、
SQLite 迁移幂等与载入重算一致性、页面冒烟（含「时差年代账」标题）。

## 依赖

仅 `modernc.org/sqlite`（纯 Go SQLite，无 cgo），版本由
`go.mod` / `go.sum` 锁定。
