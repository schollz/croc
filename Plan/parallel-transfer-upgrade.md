# Croc 并行传输与自适应优化升级方案

| 项目 | 内容 |
| --- | --- |
| 基线版本 | v11.5.5 |
| 版本策略 | 保持 v11.5.5，不升级 major、不迁移 module path |
| 基线提交 | ebcdc31b |
| 文档日期 | 2026-10-28 |
| 文档版本 | 0.5 |
| 状态 | 设计稿，未实现 |
| 适用范围 | croc 点对点传输（relay / Tailcat），不包含 store 服务协议 |

### 变更记录

| 版本 | 变更 |
| --- | --- |
| 0.1 | 初始分析、目标、v2 协议、并发/续传/自适应方案 |
| 0.2 | 补充并发所有权与锁模型、协议状态机、错误恢复矩阵、边界场景、实施任务拆解、设计取舍、性能实验与灰度发布；细化 journal/bitmap 语义和关闭顺序 |
| 0.3 | 增加 batchID 帧校验、parallel-batch-reset、checkpoint 临界区、hash 失败重置、动态连接/活跃 worker 修正、FilePreparedBatch、单控制写者、精确哈希瓶颈处理等 |
| 0.4 | 增加协议状态机边界审计（批次/文件两级状态、非法事件、过期帧、重试/重连/取消/降级） |
| 0.5 | 按用户决定保持 v11.5.5：撤销 v12.0.0 major bump 与 module path /v12 迁移；保留 v2 feature negotiation 与发布门禁 |

> 阅读顺序建议：0-2 节看结论与现状；3-8 节看设计；9-15 节看落地；16-22 节看实现级细化、任务拆解与实验/灰度；23-24 节看协议边界审计与版本/发布策略。

## 0. 结论摘要

1. **多进程：不支持，也不建议引入。** croc 是单进程 Go 程序，网络 I/O 由 goroutine 并发完成，Go runtime 会把 goroutine 调度到多个 OS 线程（<code>src/cli/cli.go:44</code>、<code>src/webcli/webcli.go:31</code> 设置 <code>GOMAXPROCS(runtime.NumCPU())</code>）。多进程会重复 PAKE 密钥、控制通道、房间和进度状态，吞吐没有收益，复杂度和故障面显著增加。操作系统中同时运行多个 croc 进程传输不同 code 不属于“单次传输内部并行”。
2. **多线程/多连接：已支持。** 发送端每个数据连接一个 goroutine worker；接收端每个数据连接一个 goroutine；大文件通过 <code>requestedChunkQueue</code> 在多个连接间分块领取。
3. **大文件分块并行：部分支持。** 固定 32 KiB chunk、应用层每连接一次只处理一个 chunk（无预取/无显式在途窗口）、连接数固定（中继默认 4 条数据连接，Tailcat 8 条流）。高 RTT / 高 BDP 链路容易欠载。
4. **多个小文件并行：不支持。** <code>FilesToTransferCurrentNum</code>、单个 <code>CurrentFile</code>、<code>close-sender</code>/<code>close-recipient</code> 构成严格单文件锁步，每个文件都要一次控制 RTT。
5. **分块级断点续传：很弱。** 仅当目标文件大小完全一致时，用 <code>MissingChunks</code> 把“全零 32 KiB 块”视为缺失。没有持久化位图、没有块校验，部分写入或非零脏块会被误判。
6. **资源/网络自适应：没有。** 连接数、chunk 大小、压缩、并发均为固定值；只有静态 <code>--throttleUpload</code> 限速。
7. **建议方案：** 新增 v2 数据面（batchID/fileID + flags + 可变 chunk + 每连接在途窗口）、批量文件调度、<code>resume</code> 日志、<code>transferperf</code> 探测/控制器。所有新能力通过 feature negotiation 启用，旧路径完全保留，旧客户端和 Web 客户端自动降级。

### 0.1 预期收益（设计目标）

| 场景 | 现状 | 目标 |
| --- | --- | --- |
| 1 GiB 大文件，100 Mbps，80 ms RTT，4 条数据连接 | 受 32 KiB chunk、串行处理/缺少预取影响，高 RTT 下可能明显欠载；数量级示意约 13 Mbps（取决于内核缓冲） | 达到链路 70% 以上 |
| 1 GiB 大文件，1 Gbps LAN | 通常可打满 | 不低于现状 |
| 10,000 个 64 KiB 小文件，30 ms RTT | 每文件一次控制 RTT，约 300 s 量级 | 提升 3 倍以上 |
| 1 GiB 传输中 kill -9 后重启 | 依赖零块启发式，可能重传全部或误判 | 最多重传一个 checkpoint 窗口（约 1 s / 32 MiB） |
| 内存占用 | 无显式预算 | 默认不超过 256 MiB，且不超过内存上限的 10% |

## 1. 现状分析

### 1.1 进程与线程模型

- **单进程**：<code>main.go</code> -> <code>src/cli</code> -> <code>croc.Client.Send</code> / <code>Receive</code>。数据面没有 fork/exec 子进程；<code>exec.Command</code> 仅用于剪贴板、SSH shell、更新校验等辅助功能。
- **多 goroutine**：发送端、接收端、中继监听、本地中继、节点发现、重连、Tailcat 协商都使用 goroutine。
- **数据面并发单元**：
  - 发送：<code>croc.go:3772</code> 的 <code>sendData</code>，由 <code>chunk_queue.go:78</code> 的 <code>startSenderChunkQueue</code> 和 <code>chunk_queue.go:110</code> 的 <code>startLateSenderWorker</code> 启动。
  - 接收：<code>croc.go:3595</code> 的 <code>receiveData</code>，调用点包括 <code>croc.go:2845</code>、<code>staged_transport.go:363</code>、<code>tailcat_negotiation.go:368</code>。
- **结论**：支持“多线程”（Go goroutine 由 runtime 调度到 OS 线程），不支持“多进程数据面”。

#### 1.1.1 为什么不做多进程

| 维度 | 多进程方案 | 进程内 goroutine 方案 |
| --- | --- | --- |
| 密钥/PAKE | 需要 IPC 共享或重复握手 | 直接共享内存 |
| 分块所有权 | 需要跨进程锁/队列 | 共享队列 + mutex/atomic |
| 断点续传 | 多进程日志合并复杂 | 单一日志，状态一致 |
| 故障恢复 | 进程间协调复杂 | 单进程 context 取消 |
| 吞吐收益 | 网络 I/O 无收益；CPU 密集可多用核 | Go runtime 已可利用多核 |
| 平台兼容 | Windows/macOS/Linux 差异大 | 纯 Go，跨平台 |

如果未来压缩/哈希成为 CPU 瓶颈，优先使用有界 goroutine worker pool（或可选的子进程 worker pool），而不是把数据面拆成多进程。

### 1.2 当前单文件传输主流程

1. 发送端 <code>Send</code>（<code>croc.go:1759</code>）调用 <code>sendCollectFiles</code> 计算文件哈希，随后通过 <code>SenderInfo</code> 发送文件清单（大清单使用 <code>file_manifest.go</code> 的分块清单）。
2. 接收端 <code>processSenderInfo</code>（<code>croc.go:2445</code>）校验并展示清单，用户确认。
3. 接收端 <code>updateIfRecipientHasFileInfo</code>（<code>croc.go:3377</code>）逐文件检查目标路径：同大小则计算目标文件哈希；哈希相同则跳过；不同则询问覆盖/续传；选定一个文件后调用 <code>recipientInitializeFile</code>（<code>croc.go:3118</code>）。
4. <code>recipientGetFileReady</code>（<code>croc.go:3187</code>）发送 <code>TypeRecipientReady</code>，携带 <code>CurrentFileChunkRanges</code>。
5. 发送端 <code>processMessage</code> 的 <code>TypeRecipientReady</code> 分支（<code>croc.go:2993</code>）设置 <code>FilesToTransferCurrentNum</code> 和 <code>CurrentFileChunkRanges</code>；<code>updateState</code>（<code>croc.go:3508</code>）打开源文件并调用 <code>startSenderChunkQueue</code>。
6. 数据连接在 <code>activateRelayDataChannels</code>（<code>croc.go:2787</code>）/<code>openRelayDataChannels</code>（<code>croc.go:2801</code>）中建立；发送 worker 和接收 goroutine 启动。
7. 接收端收齐当前文件后发送 <code>TypeCloseSender</code>；发送端 <code>croc.go:3037</code> 重置全局生命周期并回 <code>TypeCloseRecipient</code>；接收端 <code>croc.go:3048</code> 重置；控制循环再选出下一个文件。
8. 全部文件完成后接收端发送 <code>TypeFinished</code>，发送端回显，双方标记成功。

### 1.3 并发能力矩阵

| 维度 | 现状 | 证据 | 主要限制 |
| --- | --- | --- | --- |
| 进程模型 | 单进程 | <code>main.go</code>、<code>src/cli</code> | 无多进程数据面 |
| 大文件分块并行 | 有 | <code>requestedChunkQueue</code>、<code>sendData</code> | 固定 32 KiB，应用层每连接串行处理一块 |
| 数据连接数 | 固定上限 | <code>croc.go:2787</code>：<code>min(len(RelayPorts), 8)</code>；Tailcat 8 条流 | 默认中继 4 条数据连接；不能按网络动态增减 |
| 多小文件并行 | 无 | 单个 <code>CurrentFile</code>；<code>FilesToTransferCurrentNum</code> | 严格锁步，每文件一个控制 RTT |
| 接收端并发写 | 有，但仅按连接 | <code>croc.go:3688</code> 的 <code>WriteAt</code> | 没有独立写池；慢盘会反压单连接 |
| 断点续传 | 零块启发式 | <code>croc.go:3149-3160</code>、<code>utils.go:437</code> | 只有同大小文件；无位图/块校验 |
| 资源/网络探测 | 无 | 无相关代码 | 固定参数 |
| 自适应并发 | 无 | 固定 <code>limit := min(len(...), 8)</code> | 高 RTT 低利用，低 RTT 可能过度并发 |
| 哈希准备 | 基本串行 | <code>file_preparation.go:84</code>；progressive 只启动一个后台 goroutine | 大量小文件启动慢 |
| 压缩 | 按文件固定 | <code>currentFileUsesCompression</code> | 不能按 CPU/链路动态关闭 |
| 进度统计 | 全局字段 | <code>TotalSent</code>、<code>TotalChunksTransferred</code>、<code>finishedNum</code> | 并发更新存在竞态风险 |

### 1.4 大文件并行细节与瓶颈

发送端队列 <code>requestedChunkQueue</code>（<code>chunk_queue.go:14</code>）把 <code>[chunkSize, start, count, ...]</code> 形式的范围展开为 offset 领取；每个 worker 循环执行：

~~~text
claim offset
-> ReadAt(32 KiB)
-> 可选压缩
-> AEAD 加密
-> dataConn.Send（同步阻塞）
-> complete
~~~

关键问题：

1. **应用层每连接一次只处理一个 chunk。** <code>sendData</code> 在 <code>dataConn.Send</code> 返回前不会读取/加密下一个 chunk（<code>croc.go:3772-3846</code>），没有预取或显式在途窗口。虽然内核发送缓冲可以吸收若干帧，但应用层无法根据 BDP 主动维持足够在途数据，磁盘/加密/压缩间隙会造成欠载。数量级示意：
   - 100 Mbps × 80 ms RTT => BDP 约 1 MB。
   - 4 条连接 × 32 KiB = 128 KiB 应用层工作集。
   - 若有效窗口仅约一个 chunk，128 KiB / 80 ms 约 1.6 MB/s，约 13 Mbps；实际值取决于内核缓冲和链路，但高 BDP 下仍容易欠载。
2. **chunk 固定 32 KiB。** <code>models.TCP_BUFFER_SIZE/2</code> 在 <code>croc.go:3780</code>、<code>chunk_queue.go:82</code>、<code>croc.go:3009</code> 等多处硬编码。每块都有 <code>Comm</code> 8 字节帧头、AEAD nonce/tag、write 系统调用和压缩/加密开销。
3. **连接数固定。** <code>activateRelayDataChannels</code> 一次性打开 <code>min(len(RelayPorts), 8)</code> 条；默认中继端口 9009-9013，banner 只包含 4 个数据端口，因此默认 4 条数据连接。Tailcat 固定 8 条流（<code>tailcat_stream_config_default.go:8</code>）。
4. **没有拥塞反馈。** 所有 worker 尽力发送，没有 RTT、重传、发送阻塞时间、队列深度等反馈；慢链路或中继限速时容易排队。
5. **接收端完成判定依赖全局字节数。** <code>receiveData</code> 用 <code>TotalChunksTransferred == CurrentFileChunkCount || TotalSent == Size</code> 判断完成；重复块或并发更新可能误判，v2 应改为位图计数。
6. **共享状态竞争。** <code>TotalSent</code> 在发送端由 <code>c.mutex</code> 保护，但接收端在 <code>receiveMutex</code> 中更新；<code>finishedNum</code> 在进度回调中自增，存在并发更新风险。

### 1.5 多小文件并行缺口

- **单文件状态**：<code>Client</code> 只有 <code>CurrentFile</code>、<code>CurrentFileChunkRanges</code>、<code>FilesToTransferCurrentNum</code>。数据 worker 全部围绕“当前文件”工作。
- **单请求锁步**：接收端一次只发一个 <code>TypeRecipientReady</code>；发送端一次只启动一个队列；文件完成通过 <code>close-sender</code>/<code>close-recipient</code> 全局重置生命周期。
- **每文件 RTT**：即使文件只有 1 KiB，也要经历“ready -> 数据 -> close -> 下一个”的往返。10,000 个小文件在 30 ms RTT 下仅控制面就约 300 s。
- **目标检查阻塞**：<code>updateIfRecipientHasFileInfo</code> 在控制循环中串行 <code>Lstat</code>/哈希/提示；大量文件会阻塞后续消息处理。
- **发送端准备串行**：<code>prepareAllFiles</code> 顺序哈希所有文件；progressive hash 只启动一个后台 goroutine（<code>file_preparation.go:117</code>）。
- **进度条按文件**：批量场景下无法表达聚合进度，且 <code>fmtPrintUpdate</code> 的 <code>finishedNum++</code> 没有原子保护。

### 1.6 断点续传现状

<code>recipientInitializeFile</code>（<code>croc.go:3118</code>）的逻辑：

1. 打开目标文件；如果不存在则创建。
2. 如果目标大小不等于源大小，直接 <code>Truncate</code> 到源大小，然后从头传输。
3. 如果大小相同，调用 <code>utils.MissingChunks</code>（<code>utils.go:437</code>）：逐 32 KiB 读取目标文件，把“全零块”视为缺失，其余块视为已存在。
4. 把缺失范围发给发送端；发送端只发送这些范围。

问题：

- **部分写入的块会被误判。** 一个块前半部分已写入、后半部分仍为零时，整块非全零，会被当作“已完成”，导致文件损坏。
- **合法零块被重复下载。** 全零数据块会被当作缺失，浪费带宽。
- **非零脏块被当作有效数据。** 预分配、旧文件残留或其他文件内容只要非零就会被跳过；最终没有整文件哈希复核（当前接收完成路径只关闭文件并发送 <code>close-sender</code>）。
- **进程重启后没有可靠状态。** 只能依赖稀疏文件中的零洞；如果文件系统不支持稀疏文件、或数据被部分覆盖，状态不可信。
- **<code>MissingChunks</code> 的空返回值有歧义。** 没有零块时返回 nil，而协议中空 ranges 表示“全部块”，语义上安全但效率低；无法表达“没有缺失块”。
- **续传百分比基于零字节估算。** <code>askReceiveOverwrite</code>（<code>croc.go:3327</code>）显示的百分比可能不准确。
- **重连只能文件级恢复。** <code>transferWithReconnect</code>（<code>croc.go:804</code>）重置状态后重新走文件检查，仍然依赖上述零块启发式；没有持久化位图。

### 1.7 资源/网络自适应现状

- 没有 CPU、内存、磁盘、网卡、RTT、吞吐、重传等探测。
- 连接数由中继 banner/Tailcat 配置决定；只有 staged transport 的固定“先 2 条，再 ramp 其余”逻辑（<code>staged_transport.go</code>、<code>tailcat_negotiation.go</code>）。
- chunk 大小固定 32 KiB；压缩按文件固定；接收端无写池；发送端无预读。
- <code>--throttleUpload</code> 使用全局 <code>rate.Limiter</code>（<code>croc.go:456-483</code>），是静态上限，不参与并发调节。
- <code>GOMAXPROCS</code> 设置为 CPU 数，但没有 cgroup quota、内存上限、文件描述符上限等容器环境感知。

### 1.8 已有可复用基础

| 现有能力 | 位置 | 复用方式 |
| --- | --- | --- |
| 分块范围队列 | <code>chunk_queue.go</code> | v2 队列沿用“范围展开 + claim/complete”思路，增加 fileID 和公平调度 |
| 范围工具 | <code>utils.go:512-600</code> | 复用 <code>ChunkRangesCount/Bytes/Contain</code>，新增 bitmap 与 ranges 互转 |
| 数据块边界校验 | <code>croc.go:3730</code> 的 <code>chunkFits</code> | 扩展为 v2 的 fileID/offset/length 校验 |
| 分块文件清单 | <code>file_manifest.go</code> | 大量文件清单继续使用 <code>TypeFileInfoStart/Batch/End</code> |
| progressive hash | <code>progressive_hash.go</code>、<code>file_preparation.go</code> | 按文件 <code>Prepared</code> 标记调度；后续可并行准备 |
| staged transport | <code>staged_transport.go</code> | 复用“延迟打开剩余连接”的机制，由控制器触发 ramp |
| Tailcat 统计 | <code>tailcat_data_transport.go</code>、<code>tailcattransport.BundleStats</code> | 作为网络探测输入（path、streams、bytes） |
| store 断点状态 | <code>storeclient/client.go:31-32</code>、<code>:104-118</code>、<code>:900-931</code> | 借鉴 4 worker + checkpoint 状态文件；P2P 需要位图和块校验，不能直接照搬 |

### 1.9 瓶颈优先级

1. 应用层每连接串行处理一块、缺少预取/在途窗口（高 RTT 首要瓶颈）。
2. 固定 32 KiB chunk（系统调用/加密开销；但小块有利于低延迟，需要自适应）。
3. 单文件锁步（小文件场景决定性瓶颈）。
4. 零块续传启发式（正确性 + 重复下载）。
5. 串行哈希/目标检查（大量文件启动慢）。
6. 无拥塞反馈（中继排队、重传、尾延迟）。
7. 全局进度/统计字段的并发竞争。

## 2. 目标、范围与非目标

### 2.1 目标

1. **大文件：** 在现有数据连接上增加每连接在途窗口和可变 chunk；按 BDP/磁盘/CPU 动态选择参数；高 RTT 链路达到链路容量 70% 以上，LAN 不回归。
2. **多小文件：** 支持一个批次内并行传输多个文件；默认可配置 2-16 个文件并行；大量小文件吞吐提升 3 倍以上。
3. **分块级续传：** 持久化 chunk 位图和块校验；进程崩溃/重连后只重传缺失或未校验块；最终整文件哈希校验。
4. **自适应：** 启动时探测 CPU/内存/磁盘/文件描述符/网络，运行中采样吞吐/RTT/重传/队列；动态调整活跃连接/worker 数、在途块数、chunk 大小、文件并行数，并受硬上限和 <code>--throttleUpload</code> 约束。
5. **兼容：** 旧 croc、Web 客户端、旧中继全部可用；新能力仅在双方声明 feature 时启用；默认 auto，可显式关闭。
6. **可观测/可测试：** 提供 debug 统计、<code>--stats</code> 摘要、单元/集成/故障注入/性能测试。

### 2.2 非目标

- 不引入多进程数据面。
- 不修改 relay 协议、不要求中继增加端口；连接数上限仍受 banner 和 Tailcat 配置约束。
- 不替换现有 PAKE、AEAD、压缩算法；不改变加密语义。
- 不在本方案中重写 store 服务/存储传输协议；仅复用其断点思路。
- 不保证对共享中继上其他租户的绝对公平；只做本地限流、退避和连接上限。
- 不在第一阶段实现 Web 客户端 v2；Web 继续走旧协议（其自身已有多 socket 大文件并行）。

### 2.3 设计原则

1. **Feature-gated：** 新数据面只在双方声明 <code>parallel-data-v2</code> 时启用；旧路径完全保留。
2. **最小侵入：** 新增 <code>parallel_*.go</code>，尽量不重写现有 <code>sendData</code>/<code>receiveData</code>；通过入口分派。
3. **有界资源：** 所有队列、缓冲、批次、文件句柄都有上限；对端输入必须校验。
4. **崩溃安全：** 先 <code>fdatasync</code> 数据，再原子写日志；日志损坏时安全回退。
5. **测量优先：** 探测失败使用保守默认；控制器有滞回和冷却，避免振荡。
6. **不泄露：** 日志/统计不包含文件名、路径、密钥；journal 权限 0600。

## 3. 总体架构

~~~text
+--------------------------------------------------------------+
|                        Orchestrator                          |
|  文件选择 / 批次选择 / 生命周期 / 进度聚合 / 错误传播         |
+---------------------------+----------------------------------+
                            |
      +---------------------+---------------------+
      |                                           |
+-----v-------------+                       +-----v-------------+
|  Sender engine    |                       |  Receiver engine  |
|  per-file queue   |                       |  per-file state   |
|  fair scheduler   |                       |  write dispatch   |
|  worker pipeline  |                       |  completion/hash  |
+-----+-------------+                       +-----+-------------+
      |                                           |
+-----v-------------+                       +-----v-------------+
|  Data plane v2    |<---- relay/Tailcat --->|  Data plane v2    |
|  fileID + offset  |                       |  fileID + offset  |
|  flags + payload  |                       |  flags + payload  |
+-----+-------------+                       +-----+-------------+
      |                                           |
+-----v-------------+                       +-----v-------------+
|  transferperf     |                       |  resume           |
|  probe/controller |                       |  bitmap/journal   |
+-------------------+                       +-------------------+
~~~

### 3.1 分层职责

| 层 | 职责 | 主要输入/输出 |
| --- | --- | --- |
| 探测层 <code>transferperf</code> | CPU/内存/磁盘/FD/网卡/网络采样 | <code>SystemProfile</code>、<code>NetworkSample</code> |
| 策略层 <code>transferperf.Controller</code> | 计算并发/块大小/窗口 | <code>Limits</code> |
| 调度层 <code>parallel_*</code> | 批次选择、公平队列、文件完成 | chunk 任务、<code>FileDone</code> |
| 传输层 <code>parallel_protocol</code> | v2 帧编解码、消息、校验 | 字节流 |
| 持久化层 <code>resume</code> | 位图、journal、校验、恢复 | 缺失 ranges/bitmap |

### 3.2 新增包与职责

- **<code>src/transferperf</code>**
  - <code>probe.go</code>：<code>SystemProfile</code>、<code>Probe</code> 接口、通用探测。
  - <code>probe_linux.go</code>、<code>probe_darwin.go</code>、<code>probe_windows.go</code>、<code>probe_other.go</code>：平台实现（build tags）。
  - <code>network.go</code>：RTT/吞吐/重传采样；Linux <code>TCP_INFO</code> 可选。
  - <code>controller.go</code>：AIMD/BDP 控制器。
- **<code>src/resume</code>**
  - <code>bitmap.go</code>：chunk 位图、ranges 互转、计数。
  - <code>journal.go</code>：元数据、快照、append log、原子写入、恢复。
  - <code>journal_test.go</code>、<code>bitmap_test.go</code>。
依赖方向：<code>src/transferperf</code> 和 <code>src/resume</code> 只依赖标准库/现有工具包，不得导入 <code>src/croc</code>；<code>src/croc</code> 导入它们，避免循环依赖。<code>transferperf</code> 的平台文件用 build tags 隔离。

- **<code>src/croc</code> 新文件**
  - <code>parallel_protocol.go</code>：feature 常量、控制消息结构、v2 数据帧编解码。
  - <code>parallel_send.go</code>：批次发送状态、per-file 队列、worker pipeline。
  - <code>parallel_receive.go</code>：批次接收状态、文件分发、完成/校验。
  - <code>parallel_queue.go</code>：公平 chunk 队列。
  - <code>parallel_progress.go</code>：聚合进度/统计（atomic）。
  - <code>parallel_state.go</code>（可选）：<code>Client</code> 上的 batch 字段和生命周期辅助。

### 3.3 状态机

**发送端**：

~~~text
idle
  -> batchPreparing (收到 batch-ready，打开文件、建队列)
  -> streaming (worker 发送)
  -> awaitingFileDone (队列耗尽，等待 file-done)
  -> batchDone (所有 file-done，发送 batch-done)
  -> idle
~~~

**接收端**：

~~~text
idle
  -> batchSelecting (检查目标、加载 journal、生成请求)
  -> batchReadySent
  -> receiving (数据帧分发、写盘、校验)
  -> fileCompleting (每文件 hash/rename/journal 清理)
  -> batchDoneReceived (发送下一批或 finished)
  -> idle
~~~

**错误/重连**：任一状态 -> cancel -> checkpoint journals -> close files/conns -> reconnect -> 重新 PAKE/清单 -> 重新选择批次 -> 恢复。

## 4. 协议 v2 设计

### 4.1 Feature 协商

在 <code>pakeFeatures()</code>（<code>tailcat_negotiation.go:211</code>）和 <code>SenderInfo.Features</code>/<code>RemoteFileRequest.Features</code> 中加入：

| Feature | 方向 | 作用 | 依赖 |
| --- | --- | --- | --- |
| <code>parallel-data-v2</code> | 双向 | 启用 v2 数据帧、批次控制消息、fileID | 无 |
| <code>chunk-resume-v2</code> | 双向 | 启用 chunk 位图/journal/块校验续传 | <code>parallel-data-v2</code> |
| <code>parallel-small-files-v1</code> | 双向 | 启用多文件批次 | <code>parallel-data-v2</code> |
| <code>transfer-autotune-v1</code> | 双向 | 启用 perf probe/动态参数提示 | <code>parallel-data-v2</code> |

- 双方在 PAKE/PAKEConfirm 中交换 feature；<code>processMessagePake</code>/<code>processMessagePakeConfirm</code> 设置 <code>peerParallelData</code>、<code>peerChunkResume</code>、<code>peerSmallFiles</code>、<code>peerAutotune</code>。
- 只有本地支持且对端支持时才启用对应能力。
- <code>parallel-data-v2</code> 单独即可支持 batch of 1（大文件）；<code>parallel-small-files-v1</code> 只控制 N&gt;1 的多文件批次。缺少 small-files 特性时，接收端仍可逐文件使用 v2 批次（每批 1 个文件），而不是整体回退 v1。
- 缺少 <code>chunk-resume-v2</code> 时，v2 仍可做并行传输，但不创建 journal；重连时只能按 legacy 的文件级/零块策略恢复，或整文件重传。
- <code>--no-parallel-data</code> 强制关闭；<code>--no-multiplexing</code> 在中继路径强制 1 条数据连接并禁用多文件批次；Tailcat 的流数仍由 stream 配置决定。
- 旧对端不认识新 feature，会忽略；新对端看不到旧 feature，走旧路径。

### 4.2 控制消息

新增 <code>message.Type</code>（只在 feature 启用时发送）：

| 类型 | 方向 | 载荷 |
| --- | --- | --- |
| <code>parallel-batch-ready</code> | 接收端 -> 发送端 | BatchID、文件请求列表 |
| <code>parallel-file-done</code> | 接收端 -> 发送端 | BatchID、文件索引、状态、字节数、哈希 |
| <code>parallel-batch-done</code> | 发送端 -> 接收端 | BatchID、状态、文件结果 |
| <code>parallel-batch-reset</code> | 发送端 -> 接收端 | BatchID、原因、可接受上限（chunkSize/batchFiles） |
| <code>perf-probe</code> | 双向 | 时间戳/序号（加密） |
| <code>perf-probe-reply</code> | 双向 | 原样回显时间戳/序号 |

建议结构（JSON，字段名可调整）：

~~~go
// 每个字段的 JSON tag 省略了反引号，实际实现时补上。
type ParallelFileRequest struct {
    Index     int     // json:"i"
    ChunkSize int64   // json:"cs"
    Ranges    []int64 // json:"r,omitempty"  [chunkSize, start, count, ...]
    Bitmap    []byte  // json:"bm,omitempty" base64，与 Ranges 二选一
    Resume    bool    // json:"res,omitempty"
}

type ParallelBatchReady struct {
    BatchID uint64                // json:"b"
    Files   []ParallelFileRequest // json:"f"
}

type ParallelFileDone struct {
    BatchID uint64 // json:"b"
    Index   int    // json:"i"
    Status  string // json:"s" ok | skipped | hash-mismatch | error
    Bytes   int64  // json:"n,omitempty"
    Hash    []byte // json:"h,omitempty"
}

type ParallelBatchDone struct {
    BatchID uint64             // json:"b"
    Status  string             // json:"s"
    Files   []ParallelFileDone // json:"f,omitempty"
}

type ParallelBatchReset struct {
    BatchID      uint64 // json:"b"
    Reason       string // json:"r"
    MaxChunkSize int64  // json:"mc,omitempty"
    MaxBatchFiles int   // json:"mf,omitempty"
}
~~~

约束：

- 单批 <code>Files</code> 不超过 <code>maxBatchFiles</code>（建议 256）；单批总 chunk 数不超过 <code>maxBatchChunks</code>（建议 262144）；单批请求 JSON 不超过 8 MiB。
- 缺失范围超过阈值（建议 100k 段）时使用 <code>Bitmap</code>，避免 ranges 膨胀。
- 所有索引必须在 <code>FilesToTransfer</code> 范围内；<code>ChunkSize</code> 必须在 64 KiB..4 MiB 且为 2 的幂；<code>Ranges</code> 必须单调、不重叠、在文件范围内。
- 旧对端不会收到这些消息；若在未协商时收到，返回协议错误并终止，避免静默误处理。v2 批次活跃时收到 legacy <code>TypeRecipientReady</code>/<code>TypeCloseSender</code>/<code>TypeCloseRecipient</code>，或 legacy 活跃时收到 batch 消息，均按协议错误处理。

### 4.3 数据帧 v2

v2 明文（在 AEAD 之前）：

| 偏移 | 长度 | 字段 | 说明 |
| --- | --- | --- | --- |
| 0 | 1 | version | 固定 2 |
| 1 | 1 | flags | bit0=压缩，bit1=文件最后一块，bit2-7 保留 |
| 2 | 2 | reserved | 0 |
| 4 | 8 | batchID | 当前批次 ID，LE uint64 |
| 12 | 4 | fileID | 文件在清单中的索引，LE uint32 |
| 16 | 8 | offset | 文件内偏移，LE uint64 |
| 24 | N | payload | 原始或压缩数据 |

- 线上格式：<code>nonce(12) || AEAD.Seal(plaintext)</code>，与现有 <code>crypt.EncryptAEADTo</code> 一致。
- 接收端在 <code>peerParallelData</code> 为真时只接受 v2 帧；否则走 v1（8 字节 offset + data）。
- 校验：
  - <code>version == 2</code>，<code>flags</code> 仅允许已知位。
  - <code>batchID</code> 等于当前批次 ID；<code>fileID</code> 在当前批次中。
  - <code>offset % chunkSize == 0</code>。
  - <code>len(payload) > 0</code>，<code>offset + len(payload) &lt;= fileSize</code>；非最后一块长度必须等于 <code>chunkSize</code>，最后一块长度必须等于 <code>fileSize - offset</code>。
  - 解压后长度不超过 <code>24 + chunkSize</code>；压缩帧必须解压成功。
  - 重复块允许写入（幂等），但位图只计一次；若块已完成且内容不同，记录错误、清除该块 completed，并在下一批请求重传（或直接按 file-done hash-mismatch 处理）。
- 与 v1 的差异：v1 的 offset 在 AEAD 明文之外（8 字节 + ciphertext），v2 把 header 放入 AEAD 明文，认证 batchID/fileID/offset/flags，避免跨批次/跨文件/偏移篡改。

### 4.4 批次时序

~~~text
接收端                                      发送端
  |--- parallel-batch-ready (N files) ------->|
  |                                           | 打开文件/建 per-file 队列
  |<------ data frames (batchID,fileID) ------|
  |--- parallel-file-done (file 0) ---------->|
  |--- parallel-file-done (file 1) ---------->|
  |                    ...                    |
  |<---------- parallel-batch-done -----------|
  |--- next batch-ready / finished ---------->|
~~~

- 一批只允许一个批次在途；接收端收到 <code>parallel-batch-done</code> 后才发下一批，限制内存和状态。
- 单文件（包括超大文件）也走同一路径，批次大小为 1；这样只需要维护一套 v2 逻辑。
- 发送端在收到所有 <code>parallel-file-done</code> 后发送 <code>parallel-batch-done</code>；如果某文件失败，立即发 <code>parallel-batch-done</code> 带错误状态，接收端保留 journal 供重试。
- 接收端在批次完成后调用现有 <code>updateIfRecipientHasFileInfo</code> 的 v2 分支选择下一批；全部完成时发送 <code>TypeFinished</code>。
- 控制通道仍是 connection 0，数据通道独立；批次消息不会阻塞数据。

### 4.5 兼容与降级

| 组合 | 行为 |
| --- | --- |
| 新发送端 + 旧接收端 | 旧端不声明 <code>parallel-data-v2</code>，走 v1 单文件路径 |
| 旧发送端 + 新接收端 | 新端看不到 feature，走 v1 |
| 新发送端 + 新接收端 | v2 批次/数据帧 |
| 新发送端 + Web 接收端 | Web 不声明 feature，走 v1；Web 自身多 socket 并行不受影响 |
| relay-only / Tailcat | 协议不变；v2 只改变数据帧和控制消息 |
| <code>--no-multiplexing</code> | 中继路径强制 1 条数据连接，禁用多文件批次；大文件仍可用 v2 帧/窗口；Tailcat 仍使用其 stream 配置 |
| <code>--throttleUpload</code> | 仍为硬上限；控制器只降低并发，不突破限速 |

## 5. 大文件并行管道

### 5.1 可变 chunk 与 BDP

- chunk 大小在每个文件开始时由接收端确定，写入 batch-ready 的 <code>ChunkSize</code>；同一文件传输期间不变，保证 offset 对齐和续传位图稳定。发送端必须原样使用；若超出自身内存/实现上限，发送端返回 <code>parallel-batch-reset</code> 并给出可接受的 <code>MaxChunkSize</code>/<code>MaxBatchFiles</code>，接收端以更小值重试一次。禁止静默改变 chunk 大小，否则 offset 和位图会错位。
- 接收端收到 batch-reset 后必须丢弃该批次按旧 chunkSize 建立的 journal/临时状态；chunkSize 变化后旧位图不可复用，只能重新创建。为减少 reset，接收端初始 chunk 建议不超过 1 MiB，只有探测确认高 BDP 时才升到 4 MiB。
- 选择公式（首版）：

~~~text
BDP = measured_bw * measured_rtt
target_inflight = clamp(BDP, 256 KiB, 64 MiB)
chunk = clamp(next_pow2(target_inflight / (conns * inflight)), 64 KiB, 4 MiB)
chunk = min(chunk, file_size)
~~~

- 初始保守值：<code>chunk=256 KiB</code>、<code>inflight=2</code>、<code>conns=min(2, available)</code>；探测完成后按控制器调整。
- 小文件：<code>chunk = min(256 KiB, file_size)</code>，避免为 1 KiB 文件发 4 MiB 缓冲。
- 大文件最后一块允许短于 chunk，帧头 flags 标记 final。
- v2 需要新增 <code>maxChunkSizeV2</code>/<code>maxDecompressedChunkSizeV2</code>（例如 chunk+24）；v1 的 32 KiB 常量保持不变，避免影响旧路径。

### 5.2 每连接在途窗口

当前 <code>sendData</code> 是“读一块 -> 加密 -> Send -> 下一块”。v2 为每个数据连接增加一个 writer goroutine 和一个有界输出队列：

~~~text
pump goroutine:
  acquire slot (in-flight 配额)
  task = queue.claim()
  buf = bufferPool.Get(task.length + header)
  n = file.ReadAt(buf[header:], task.offset)
  wire = encodeAndEncrypt(buf)
  out <- frame            // 有界 channel，满则阻塞
writer goroutine:
  for frame := range out:
    err = conn.Send(frame.wire)
    release buffer
    release slot
    onSent(frame)         // 更新 per-file 计数
    if err: cancel batch
~~~

- <code>out</code> 容量 = <code>inFlight</code>（建议 2-4）；<code>slot</code> 可用带缓冲 channel 或 <code>out</code> 本身作为配额。
- 内存上限：<code>conns * inFlight * (chunk + 24 + AEAD overhead) * 2</code>（读/加密与待发）；默认总预算 256 MiB，且不超过内存上限的 10%。
- 每连接仍只有一个 writer 调用 <code>Comm.Send</code>，与现有 <code>writeMu</code> 兼容；不会出现同连接多 writer 竞争。
- 队列耗尽后 pump 关闭 <code>out</code>；writer 必须排空所有已入队帧后才算该连接完成。所有连接 writer 退出后，发送端才进入 <code>awaitingFileDone</code>，不能提前发 batch-done。
- 失败时 <code>attempt.report(transferDisconnectError)</code>，与现有重连机制一致；已发送未确认块由接收端 journal 在重连后重新请求。

### 5.3 读预取与缓冲池

- 首版每个 worker 直接 <code>ReadAt</code>；队列按 offset 递增领取，多个 worker 并发读可能造成 HDD 随机读。
- Phase 4b 增加 per-file 预读 ring：一个 reader goroutine 顺序读 <code>4 * chunk</code>，worker 从 ring 取 slice；引用计数归零后归还。
- 缓冲池使用 <code>sync.Pool</code>，按 chunk 大小分档（64 KiB/256 KiB/1 MiB/4 MiB），避免频繁分配。
- 预读深度受内存预算和磁盘队列深度限制；SSD/NVMe 可关闭预读以减少延迟。

### 5.4 公平调度

- <code>parallelChunkQueue</code> 维护每个文件的剩余 ranges、当前 offset、剩余块数。
- <code>claim()</code> 采用轮转（round-robin）或 deficit round-robin：每次从当前文件取一块，切换到下一个未完成文件；小文件优先完成，避免尾延迟。
- 大文件与小文件混合批次中，小文件获得与文件数成比例的份额；不做严格权重，避免大文件饥饿。
- 每文件 <code>remaining</code> 用 atomic；归零时关闭文件（读取已完成）；整批归零时通知 orchestrator。
- 队列本身有 <code>maxQueuedChunks</code> 上限；worker 先取配额再 claim，避免预取过多。
- v2 的 chunk 数/offset/剩余字节使用 <code>int64/uint64</code>，不要复用 legacy 的 <code>int</code> 总数字段；32 位平台和超大文件必须测试。

### 5.5 压缩与加密并发

- v2 帧 flags 的 bit0 表示本块是否压缩；发送端可按 per-file <code>IsCompressed</code> 决定，也可在 CPU 饱和时对后续块关闭压缩（接收端按帧解压）。v2 worker 不得调用依赖全局 <code>FilesToTransferCurrentNum</code> 的 <code>currentFileUsesCompression</code>，否则多文件批次会读错文件。
- 压缩使用现有 <code>compress.CompressTo</code>（内部 <code>sync.Pool</code>，并发安全）；每 worker 复用 <code>compressedBuffer</code>。
- AEAD：建议每 worker 通过 <code>crypt.NewAESGCM(key)</code> 创建独立实例，或确认共享 <code>cipher.AEAD</code> 的并发安全；无论哪种，race 测试必须覆盖。随机 nonce 每块生成，1M 块碰撞概率可忽略。
- <code>--throttleUpload</code> 的 <code>rate.Limiter</code> 是并发安全的；在 writer 前 reserve，确保所有 worker 共享总速率。

### 5.6 动态连接/活跃 worker

- <code>availableDataChannels()</code>：中继为 <code>min(len(Options.RelayPorts), 8)</code>，Tailcat 为 bundle 的 stream 数；<code>--no-multiplexing</code> 在中继路径为 1，Tailcat 仍为 bundle stream 数。
- **数据连接的建立保持现有对称协议，不在传输中由单侧打开新索引。** relay 在 activation 时打开 banner 提供的全部（或 staged transport 的 2 条 + ramp 其余）；Tailcat 打开 bundle 的全部 stream。双方建立相同的索引集合。
- 不在运行中单方面调用 <code>openRelayDataChannels</code> 打开新索引：对端没有对应的 relay room/接收 goroutine，sender worker 会阻塞在握手或发送上。若未来需要真正增加 TCP 连接，必须增加协调消息（例如 <code>parallel-conn-open</code>/<code>parallel-conn-ready</code>，双方确认索引 j 后再打开），放到 Phase 4b。
- v2 控制器的 <code>Connections</code> 限制表示**活跃 worker 数**，不是实际 TCP 连接数。增加时从已建立但空闲的连接激活 worker；减少时让多余 worker 完成当前块后退出。实际 TCP 连接保留到会话结束，避免接收 goroutine 把关闭误判为断线。接收端可以为所有已建立连接保留 receive goroutine，空闲连接不耗 CPU；实际发送速率主要由发送端 worker 数和接收端反压共同决定。
- Tailcat 的 stream 数在 bundle 创建时固定；控制器只能在 1..streams 内调整实际使用 worker 数，不能动态增加流。未来可把 <code>Config.StreamCount</code> 暴露为可配置上限。
- 中继端口不足时，活跃 worker 上限自动降到 available；不会尝试超出 banner 的连接。
- staged transport 的 ramp 可以在批次进行中完成：新连接按现有 install 路径启动 receive goroutine 或注册 sender worker，不需要重发 batch-ready；新 worker 从当前队列继续 claim。<code>startLateSenderWorker</code> 在 v2 活跃时必须注册到 batch，而不是 legacy <code>requestedChunkQueue</code>。

## 6. 多小文件并行

### 6.1 文件分类与批次选择

接收端在 <code>updateIfRecipientHasFileInfo</code> 的 v2 分支中分类。目标检查使用有界 worker pool（建议 <code>min(4, NumCPU)</code>）：只对“目标存在且大小相同”的文件计算哈希；结果按索引汇总，控制循环只消费结果，不在 worker 中弹交互提示。交互提示统一回到控制循环，避免并发读写标准输入。发送端文件准备同样使用有界 pool：每个索引只由一个 worker 准备，完成后按索引发送 <code>TypeFilePrepared</code>；接收端批次选择只挑已 <code>Prepared</code> 的文件，未完成的留到后续批次，不阻塞已准备好的文件传输。大量小文件时逐条 <code>TypeFilePrepared</code> 会造成控制面开销，v2 增加 <code>TypeFilePreparedBatch</code>（最多 256 项，索引/哈希/压缩标志数组）批量通知；旧 peer 仍使用逐条消息。批量通知只设置 <code>Hash/IsCompressed/Prepared</code>，<code>imohash-v2</code> 的 exact-hash 协商仍按文件进行，不能跳过。<code>TypeFilePreparedBatch</code> 也由单一 control writer 发送，准备 worker 只提交结果，避免与控制消息逻辑顺序竞争。但当前 exact-hash 是单 pending，逐个小文件往返会成为批次瓶颈：建议对 <code>Size &lt;= smallFileThreshold</code> 的文件直接使用全文件 xxhash（或跳过采样精确校验），大文件才走 exact-hash；或者后续把 exact-hash 扩展为按 index 的并发请求表。若按文件混用算法，需要在 <code>FileInfo</code> 或 Prepared 消息中携带每文件算法；首版可限定一个批次使用统一算法，并推荐默认 xxhash。发送端 <code>--ask</code> 在 v2 的首个批次询问一次并记住结果（显示本批文件数/总大小或整个传输概况），后续批次不重复询问；拒绝则终止整次传输，而不是只拒绝当前批次。并行哈希/目标检查/校验不得各自绘制进度条；统一到聚合进度或 debug 日志，避免多行输出互相覆盖。接收端需要覆盖确认的文件默认不进入并行批次；若用户明确选择批量覆盖，则只询问一次（显示文件数/总大小），否则这些文件逐文件走 legacy 提示，避免 256 次交互。

| 类别 | 条件 | 处理 |
| --- | --- | --- |
| 已完成 | 目标存在、大小/哈希与清单一致 | 跳过，计入 unchanged |
| 空文件/符号链接 | Size==0 或 Symlink!="" | 走现有元数据创建，不占数据批次 |
| 小文件 | Size &lt;= <code>smallFileThreshold</code>（默认 1 MiB） | 进入并行批次 |
| 大文件 | Size &gt; 阈值 | 单独成批（或与少量小文件混合，Phase 3b） |
| 需要交互 | 目标存在、哈希不同、未指定 <code>--overwrite/--rename</code> | 先在控制循环按现有逻辑询问；批准后进入批次 |

批次上限：

- <code>maxBatchFiles=256</code>：限制控制消息和内存。
- <code>maxBatchBytes=64 MiB</code>：限制**小文件批次**总大小；大于 <code>smallFileThreshold</code> 的单个大文件单独成批，不受此上限限制（只受 chunk/inflight/内存约束）。
- <code>maxBatchChunks=262144</code>：限制**小文件批次**的队列/请求规模；大文件用 ranges/bitmap 惰性展开队列，不一次性分配全部 chunk 任务。
- <code>maxOpenFiles=min(rlimit*3/4, 256)</code>：限制同时打开的目标/源文件句柄；超出的文件留到下一批。

选择顺序建议按文件大小升序，优先完成小文件，降低尾延迟；同时保证每个文件至少一个 chunk，避免被大文件挤占。所有进入批次的文件必须已经完成 <code>Prepared</code>/哈希（progressive hash 可能仍在后台进行）；未准备好的文件等待或留到下一批，不能在没有可信 <code>FileInfo.Hash</code> 的情况下启用续传。选择循环遇到未 <code>Prepared</code> 文件时应 <code>continue</code> 扫描后续文件，而不是直接返回 nil；只有当前完全没有可传文件时才等待下一个 <code>FilePrepared</code>，避免队首未准备文件阻塞整个批次。等待由 <code>FilePrepared</code>/<code>FilePreparedBatch</code> 消息或控制循环事件驱动，不要 sleep 轮询。

### 6.2 批次状态与完成协议

- 接收端为每个文件建立 <code>receiveFileState</code>：<code>index</code>、<code>file</code>、<code>chunkSize</code>、<code>bitmap</code>、<code>journal</code>、<code>received</code>、<code>done</code>。
- 发送端为每个文件建立 <code>fileSendState</code>：<code>index</code>、<code>file</code>、<code>chunkSize</code>、<code>ranges</code>、<code>remaining</code>、<code>compressed</code>。
- 数据帧的 <code>fileID</code> 直接使用清单索引；接收端用 map 分发。
- 文件完成：
  1. 接收端位图计数达到 <code>chunkCount</code>；
  2. <code>file.Sync()</code>（大文件）或临时文件写完；
  3. 计算整文件哈希，与 <code>FileInfo.Hash</code> 比较；
  4. 成功：删除 journal、必要时 rename、发送 <code>parallel-file-done{ok}</code>；
  5. 失败：将该文件所有块标记为 missing（清空 completed 位图/重建 journal），发送 <code>hash-mismatch</code>；发送端重试该文件（首版整文件重试，Phase 4b 支持按块重传）。如果不清空位图，重复块会被幂等忽略，重试永远不会修复数据。
- 文件完成后按 <code>FileInfo.ModTime</code> 调用 <code>Root.Chtimes</code>（与 unchanged 文件行为一致），失败只告警；新文件按清单设置权限，已存在的续传文件保留原权限。
- 整文件哈希校验在独立的有界 verify pool 中执行（建议 1-2 个并发），避免与接收写盘争用磁盘；验证期间其他文件可以继续接收。
- 为减少 10k 小文件的 file-done 控制消息，可在 Phase 4b 增加 <code>parallel-files-done</code> 批量结果（每 16-64 个或 100 ms 发送一次）；首版先逐文件发送，保证状态机简单，性能测试确认控制面成为瓶颈后再批量化。
- 发送端收到所有 file-done 后发送 <code>parallel-batch-done</code>；接收端清理批次状态并选择下一批。
- 错误：任一文件致命错误（磁盘满、源文件变化、hash 连续失败）-> 取消批次，保留 journal，走现有 <code>attempt.report</code> 重连/报错。

### 6.3 文件句柄、内存与进度

- 接收端打开文件数不超过 <code>maxOpenFiles</code>；每完成一个文件立即关闭并释放状态。
- 发送端同样限制打开源文件数；<code>ReadAt</code> 并发安全；文件关闭前等待该文件所有在途块发送完成。
- 批次进度使用聚合 <code>atomic.Int64</code>：已接收字节/总字节、已完成文件数/总文件数；单独 ticker 更新进度条，data worker 不直接调用 UI。
- <code>numberOfTransferredFiles</code>/<code>finishedNum</code> 在 <code>file-done(ok)</code> 时更新，而不是选中/重试时更新，避免 hash 重试导致重复计数；回退/失败的文件不计入成功数。
- 单文件批次（大文件）沿用现有按文件进度条；多文件批次显示聚合进度 + 当前文件数，避免 10k 行输出。
- <code>--stdout</code>、<code>--sending-text</code> 禁用多文件批次，走旧路径（输出顺序敏感）。

### 6.4 与 legacy 的边界

- v2 批次启用时，不使用 <code>FilesToTransferCurrentNum</code>、<code>CurrentFile</code>、<code>CurrentFileChunkRanges</code>、<code>TypeRecipientReady</code>、<code>TypeCloseSender/CloseRecipient</code> 的全局语义；这些字段留给 legacy 路径。
- <code>updateState</code> 在 v2 活跃时只处理批次消息，不启动 legacy 队列。
- <code>resetForReconnectAttempt</code> 清理 v2 状态；<code>closeAttempt</code> 先 checkpoint/close journals，再关闭文件/连接。
- 若 v2 中途失效（例如对端重连后不再声明 feature），不得静默回退 legacy 继续同一批次；应 abort 当前批次、保留 journal，然后按新协商结果重新开始（新的 v1 或 v2 批次）。已写入数据通过 hash/journal 重新评估。

### 6.5 批次选择伪代码

~~~go
func (c *Client) selectParallelBatch() ([]ParallelFileRequest, bool, error) {
    if !c.parallelEnabled() || c.Options.Stdout || c.Options.SendingText {
        return nil, false, nil
    }
    var reqs []ParallelFileRequest
    var bytes, chunks int64
    for i, info := range c.FilesToTransfer {
        if c.fileFinished(i) || info.declined { continue }
        if info.Size == 0 || info.Symlink != "" { continue }
        dest := join(info.FolderRemote, info.Name)
        state, err := c.inspectDestination(i, dest, info) // 并发池，可缓存
        if err != nil { return nil, false, err }
        if state.unchanged { c.markUnchanged(i); continue }
        if state.needsPrompt {
            if !c.promptOverwrite(i, state) { c.markDeclined(i); continue }
        }
        ranges, err := state.missingRanges(info.Size)
        if err != nil { return nil, false, err }
        if len(ranges) == 0 { c.markFinished(i); continue }
        if info.Size > smallFileThreshold {
            if len(reqs) == 0 {
                reqs = append(reqs, ParallelFileRequest{...})
                bytes += info.Size
                chunks += count(ranges)
            }
            break // 大文件单独成批
        }
        if bytes+info.Size > maxBatchBytes || chunks+count(ranges) > maxBatchChunks ||
           len(reqs) >= maxBatchFiles { break }
        reqs = append(reqs, ParallelFileRequest{...})
        bytes += info.Size; chunks += count(ranges)
    }
    return reqs, len(reqs) > 0, nil
}
~~~

## 7. 分块级断点续传

### 7.1 目标

- 以固定 <code>resume chunk</code> 为粒度记录“已完成/在途/缺失”。
- 进程崩溃、断网重连、用户中断后，只重传缺失或未校验块。
- 块级校验能发现部分写入/磁盘损坏；最终整文件哈希兜底。
- 旧文件（没有 journal）可安全回退，不误信零块启发式。

### 7.2 Journal 格式

建议二进制格式（大文件位图/哈希更紧凑；小文件可用 JSON 元数据）：

~~~text
offset  size  field
0       8     magic "CROCRSV2"
8       2     version
10      2     header length
12      4     header CRC32C
16      H     header (binary or JSON)
...           completed bitmap (ceil(chunkCount/8) bytes)
...           inflight bitmap
...           chunk hashes (chunkCount * 8 bytes, xxhash64)
...           8 bytes footer CRC64/xxhash
~~~

所有整数使用小端；解析前先校验 header/位图/哈希长度上限，避免恶意 journal 触发大分配。

Header 字段：

| 字段 | 说明 |
| --- | --- |
| FileSize | 源文件大小 |
| ModTimeUnixNano | 源文件 mtime（辅助校验） |
| HashAlgorithm | 清单协商的算法 |
| SourceHash | 发送端 FileInfo.Hash |
| ChunkSize | 续传粒度，必须与请求一致 |
| ChunkCount | ceil(FileSize/ChunkSize) |
| Destination | 目标相对路径（用于校验，不记录绝对敏感路径） |
| StateVersion | journal 结构版本 |

- 恢复匹配以 <code>FileSize + SourceHash + ChunkSize</code> 为主；<code>ModTime</code> 只记录用于诊断，不作为拒绝条件，避免 rsync/chmod 后 mtime 变化导致无谓丢弃。
- <code>SourceHash</code> 不能为空；为空说明文件未 <code>Prepared</code>，不应创建/加载 journal。
- 位图：<code>completed</code> 表示“已写入且已通过块校验/已 fsync”；<code>inflight</code> 表示“已开始写但未 checkpoint”。
- 块哈希：完成时计算 <code>xxhash64(payload)</code>（解密/解压后的纯文件数据，不覆盖 header），用于恢复时校验；不需要发送端提供块哈希。恢复时按 offset 读取相同长度的 payload 重新计算。
- 大文件可选 append log：每完成 N 块（建议 64-256）追加一条 <code>{chunkID, hash, CRC}</code> 记录并 <code>fsync</code>；每 30 s 或 4096 块做一次完整快照，日志超过快照 2 倍时压缩。加载时先读快照再重放日志，避免每秒写完整位图/哈希造成写放大。
- 对大于 10 GiB 的文件，建议 chunk 不小于 256 KiB，避免 <code>chunkCount * 8</code> 的块哈希表过大；64 KiB 主要留给小文件/低延迟场景。
- 小文件（&lt;= 1 MiB）可以只记录“完整/缺失”，不建块哈希；大文件才启用完整 journal。
- Journal 文件权限 0600；路径通过 <code>receivefs.Root</code> 创建，禁止跟随符号链接逃逸。建议使用 <code>Root.CreateTemp</code> 写临时 journal、<code>Root.Rename</code> 原子替换；<code>Root.WriteFileAtomic</code> 已包含临时文件 + Sync + Rename，但目录 fsync 仍需单独处理。临时数据文件同样使用 <code>Root.CreateTemp</code>，避免随机文件名冲突和路径逃逸。
- Sidecar 名称冲突：若 <code>dest.crocstate</code> 已存在但不是合法 journal（magic/CRC/版本不匹配），不要覆盖用户文件；回退到集中的 <code>.croc-resume/</code> 目录，或对该文件禁用续传并改用临时文件 + rename。

### 7.3 状态转换与崩溃一致性

~~~text
missing --WriteAt开始--> inflight --WriteAt完成+hash--> completed
inflight --checkpoint前崩溃--> missing（恢复时）
completed --checkpoint--> durable completed
completed --恢复校验失败--> missing
~~~

Checkpoint 顺序（必须严格遵守）：

1. 进入 checkpoint 临界区：暂停新的 <code>completed</code> 标记；数据 worker 仍可继续 <code>WriteAt</code>，但 <code>MarkComplete</code> 等待。
2. 等待当前已经进入完成标记的块结束；未完成的块保持 <code>inflight</code>。
3. 对数据文件执行 <code>fdatasync</code>（Linux <code>unix.Fdatasync</code>；Windows <code>FlushFileBuffers</code>；macOS <code>F_FULLFSYNC</code> 可选）。
4. 在临界区内复制 <code>completed</code> 位图和块哈希快照；此时新完成的块尚未标记，保持 <code>inflight</code>。
5. 写 journal 临时文件并 <code>fsync</code>；<code>rename</code> 为正式 journal；对目录 <code>fsync</code>（Unix）。
6. 释放临界区；等待中的 <code>MarkComplete</code> 继续。journal 中的 <code>inflight</code> 块在恢复时转为 <code>missing</code>，最多重传一个 checkpoint 窗口。

- 每 1 s 或 32 MiB 触发一次 checkpoint，以先到者为准；传输结束、SIGINT、重连、<code>closeAttempt</code> 时强制 checkpoint。启用 append log 时，这个频率对应“追加日志 + fsync”；完整快照按 30 s / 4096 块执行。
- 临界区只阻塞“完成标记”，不阻塞网络接收和 <code>WriteAt</code>；<code>fsync</code> 通常为毫秒级。若底层文件系统 <code>fsync</code> 很慢（NFS/SMB），降低 checkpoint 频率或使用 <code>--no-resume</code>。
- 崩溃发生在 2-3 之间（fdatasync 之前）：数据可能尚未 durable，journal 仍是旧快照；最近窗口重传；安全。
- 崩溃发生在 3-4 之间（fdatasync 之后、快照写入之前）：数据已 durable，但 journal 未记录新快照；最近窗口重传；安全。
- 崩溃发生在 4-5 之间（快照内存中、未落盘）：旧 journal 仍有效；最近窗口重传；安全。
- Journal 损坏/截断/CRC 失败：丢弃并整文件重传；不崩溃。
- 恢复时 <code>inflight</code> 一律转为 <code>missing</code>；<code>completed</code> 可选校验（见 7.5）。
- checkpoint 失败（fsync/rename/目录 fsync）：不要发布新的 completed 快照；停止批次并报告错误，旧 journal 保留，后续可重试。

### 7.4 恢复流程

1. 接收端收到文件清单后，打开/创建目标文件。
2. 尝试加载 <code>dest + ".crocstate"</code>：
   - 不存在：全新传输；创建 journal。
   - 存在但 header 与当前文件大小/源哈希/算法/chunkSize 不匹配：删除 journal，整文件重传。
   - 匹配：加载位图；把 inflight 清为 missing。
3. 校验 completed 块（默认全量，见 7.5）：逐块 <code>ReadAt</code> + hash，与 journal 中的哈希比较；不匹配的块清为 missing。
4. 生成缺失 ranges/bitmap，放入 batch-ready 请求；发送端只发送这些块。
5. 传输过程中每完成一块：写数据 -> 计算块哈希 -> 标记 completed（内存） -> 更新进度；按 checkpoint 策略持久化。
6. 全部块完成：<code>Sync</code> -> 整文件哈希 -> 成功则删除 journal，失败则保留并报错/重试。

### 7.5 校验策略

- **默认全量校验 completed 块**：恢复时读取整个已接收文件，按块哈希比较；对 1 GiB 文件约 1-2 s（SSD），换取正确性。
- **<code>--resume-verify=sample</code>**：只校验最近 checkpoint 窗口和随机抽样块；更快但可能漏掉旧损坏。
- **<code>--resume-verify=none</code>**：信任 journal；仅在用户明确接受风险时使用；最终整文件哈希仍然执行。
- **整文件哈希**：使用清单中的算法（默认 xxhash 全文件）；若用户选择 <code>imohash</code> 等采样算法，文档说明其续传身份校验为采样强度，建议续传场景使用 xxhash/md5。
- **hash 不匹配处理**：首版整文件重传一次；若仍失败，报错并保留 journal。Phase 4b 可让发送端提供块哈希清单，实现按块修复。

### 7.6 清理与安全

- 大文件采用“就地写 dest + sidecar journal”：成功后删除 journal，不做 rename（避免 Windows/跨设备 rename 限制）；失败时保留 partial 文件 + journal 供续传。
- 小文件采用“临时文件 + rename”：<code>Root.CreateTemp</code> 写入，校验通过后 <code>Root.Rename</code> 到目标；失败时删除临时文件，不暴露 partial 目标。若目标已存在且用户批准覆盖，先 <code>Root.Remove</code> 再 <code>Root.Rename</code>（小文件可接受短暂非原子窗口；Windows 上尤其需要）。
- 成功：删除 journal；如果使用临时数据文件，<code>rename</code> 到目标；目录 fsync。
- 用户拒绝/取消：保留 journal（可续传），或按 <code>--no-resume</code> 删除临时状态；不删除用户已有目标文件。
- 源文件变化：sender 的 <code>validateSourceUnchanged</code> 失败；receiver 的 journal 因 SourceHash 不匹配而丢弃。
- 路径安全：journal 与临时文件都通过 <code>receivefs.Root</code> 打开，拒绝绝对路径/父目录逃逸；权限 0600。
- 不记录明文、密钥、完整绝对路径；只记录目标相对路径的哈希或 basename。Journal 的 CRC 只用于检测意外损坏，不作为防篡改边界；传输完整性由 AEAD 和最终文件 hash 保证。

### 7.7 与 store 传输的关系

- <code>storeclient</code> 已有 <code>storedTransferWorkers=4</code>、<code>checkpointChunks=8</code>、<code>.croc-store-&lt;id&gt;.json</code> 的 <code>Completed</code> map（<code>storeclient/client.go:31-32</code>、<code>104-118</code>、<code>900-931</code>、<code>1013-1070</code>）。
- 该模式证明“并发块 + 持久化完成状态”可行；但 P2P 传输文件可能远大于 store chunk（4 MiB），且需要位图/块哈希，不能直接复用 JSON map（大文件会膨胀）。
- 本方案不修改 store 协议；未来可将 <code>resume</code> 的 bitmap/journal 泛化，供 store 和 P2P 共用。

### 7.8 恢复伪代码

~~~go
func (c *Client) openReceiveFile(index int, info FileInfo) (*receiveFileState, error) {
    dest := join(info.FolderRemote, info.Name)
    file, created, err := c.receiveRoot.OpenFileForResume(dest, info.Size)
    if err != nil { return nil, err }
    journal, err := resume.Open(dest+".crocstate", resume.Metadata{
        FileSize: info.Size, SourceHash: info.Hash, HashAlgorithm: c.Options.HashAlgorithm,
        ChunkSize: c.parallelChunkSize(info), ModTime: info.ModTime,
    })
    if err != nil { // corrupt/mismatch -> restart
        journal = resume.New(...)
        file.Truncate(info.Size)
    } else if created {
        // new file: all missing
    }
    if err := journal.VerifyCompleted(file, c.resumeVerifyMode); err != nil {
        return nil, err
    }
    return &receiveFileState{file: file, journal: journal, ...}, nil
}
~~~

## 8. 资源与网络自适应

### 8.1 探测项

| 类别 | 指标 | 采集方式 | 用途 |
| --- | --- | --- | --- |
| CPU | 逻辑核数、GOMAXPROCS、cgroup quota | <code>runtime.NumCPU</code>、<code>/sys/fs/cgroup/cpu.max</code>（v2）、<code>cpu.cfs_quota_us</code>（v1） | 压缩/哈希并发上限 |
| 内存 | 可用内存、cgroup limit、当前 Go heap | <code>/proc/meminfo</code>、<code>memory.max</code>、<code>runtime.ReadMemStats</code> | 缓冲预算 |
| 磁盘 | 顺序读/写 MB/s、延迟 | 临时文件 8-32 MiB 读写基准；超时 150 ms | chunk/连接上限 |
| FD | RLIMIT_NOFILE | <code>unix.Getrlimit</code>；Windows 固定 8192 | 批次文件数 |
| 网卡 | 链路速率（可选） | Linux <code>/sys/class/net/*/speed</code>；Wi-Fi/虚拟网卡不可靠 | 仅作提示 |
| RTT | 控制通道往返 | <code>perf-probe</code> 3 次取中位数 | BDP/窗口 |
| 吞吐 | 实际 goodput | 发送/接收字节计数器 EWMA | 控制器主输入 |
| 丢包/重传 | TCP_INFO（Linux）或发送阻塞时间/超时 | <code>unix.GetsockoptTCPInfo</code>；跨平台回退 | 退避 |
| 压缩比 | 发送前/后字节 | 现有压缩路径统计 | 是否关闭压缩 |
| Tailcat | path/streams/bytes | <code>BundleStats</code> | 直连/DERP 提示 |

- CPU 利用率：Unix 用 <code>getrusage</code> 的 utime+stime 差分；Windows 用 <code>GetProcessTimes</code>；失败时退化为“发送阻塞时间 + 吞吐下降”判断，不阻塞传输。

探测必须满足：

- 失败/超时使用默认值，不阻塞传输启动。
- 磁盘基准写入临时文件后删除；不写用户文件；磁盘满/只读时跳过。
- 探测结果缓存（同一设备/路径），避免每次传输重复基准。
- 不与 <code>--throttleUpload</code> 冲突；限速时以限速值为带宽上限。

### 8.2 控制器

~~~go
type Limits struct {
    Connections    int
    ChunkSize      int64
    InFlight       int
    ParallelFiles  int
    ReadAheadBytes int64
    Compression    bool
}

type Sample struct {
    At              time.Time
    Bytes           int64
    Interval        time.Duration
    RTT             time.Duration
    Retransmits     uint64
    SendBlocked     time.Duration
    CPUUtilization  float64
    MemUtilization  float64
    DiskReadBps     int64
    DiskWriteBps    int64
    QueueDepth      int
    Errors          int
}

type Controller interface {
    Initial(profile SystemProfile, availableConnections int) Limits
    Observe(Sample) Limits
    Current() Limits
}
~~~

### 8.3 调节策略

- **采样周期**：250-500 ms；吞吐/RTT 使用 EWMA（alpha 0.2-0.3）。
- **目标在途字节**：<code>target = clamp(bw * rtt, 256 KiB, 64 MiB)</code>。
- **chunk 大小**：<code>chunk = nextPow2(target / (connections * inFlight))</code>，限制 64 KiB..4 MiB，且不超过文件大小。
- **增加顺序**：连接数 -> 每连接在途块数 -> chunk 大小。每次只增加一个维度，观察 1-2 个采样周期。
- **减少顺序**：chunk 大小 -> 在途块数 -> 连接数；乘法减少（×0.5 或 ×0.75）。
- **触发减少**：
  - RTT 超过基线 2 倍；
  - 重传增量超过阈值（Linux TCP_INFO）；
  - 发送阻塞时间占比 &gt; 30%；
  - 错误/超时增加；
  - relay 返回限流/断开。
- **触发不增加**：
  - CPU &gt; 85% 或内存 &gt; 90%；
  - 磁盘实测吞吐接近基准的 80%；
  - 队列深度持续 &gt; 50%；
  - 正在冷却期（减少后 2 s）。
- **滞回**：连续 2 个采样周期才有资格增加；增加后至少 1 s 不再增加；避免振荡。
- **硬上限**：
  - 活跃 worker/连接数 &lt;= min(availableConnections, 8, <code>--parallel-connections</code>)；
  - 在途 &lt;= 4；
  - chunk &lt;= 4 MiB；
  - 并行文件数 &lt;= min(16, max(2, CPU/2), <code>--parallel-files</code>)；
  - 总缓冲 &lt;= min(256 MiB, 内存上限×10%)；
  - 任何参数不得超过用户显式值或 <code>--throttleUpload</code>。
- **降级**：探测不可用/容器限制未知时，使用保守默认（2 连接、2 在途、256 KiB、并行文件 4），仅根据实际吞吐缓慢增加。

### 8.4 集成点

- <code>newClient</code>：创建 <code>transferperf.Controller</code>；传输路径确定后异步启动探针。
- <code>activateRelayDataChannels</code>：实际 TCP 连接建立保持现有对称协议（全部或 staged 2+ramp）；<code>controller.Initial(...).Connections</code> 只决定初始**活跃 worker 数**。不单方面增量打开新索引，也不在降速时关闭连接。
- <code>openRelayDataChannels</code>：保持现有激活/ramp 调用；v2 不用于单侧增量打开。未来若增加 <code>parallel-conn-open/ready</code> 协调消息，再由双方按索引打开并记录，避免重复。
- <code>startParallelBatch</code>：使用 <code>controller.Current()</code> 的 chunk/inFlight/parallelFiles。
- <code>receiveDataV2</code>：不需要调参；帧大小由发送端决定；接收端用本地接收字节/磁盘/内存采样调整自己的 chunk 请求和并行文件数，不需要新增 <code>perf-sample</code> 控制消息。发送端用自身发送统计、发送阻塞时间和 TCP_INFO 调整 worker/在途。
- <code>perf-probe</code>：PAKE 后、首批前各 3 次；传输中每 1 s 一次；只在 feature 启用时发送。
- Tailcat：<code>BundleStats</code> 提供 path/bytes，作为初始 RTT/吞吐提示；stream 数固定，控制器只调 worker 数。
- <code>--throttleUpload</code>：控制器把 <code>limiter.Limit()</code> 作为带宽上限；不把限速等待当作网络拥塞。

### 8.5 控制器伪代码

~~~go
func (c *Controller) Observe(s Sample) Limits {
    c.bw = ewma(c.bw, float64(s.Bytes)/s.Interval.Seconds(), 0.3)
    c.rtt = ewmaDuration(c.rtt, s.RTT, 0.2)
    bad := s.RTT > 2*c.baseRTT || s.Retransmits > c.lastRetrans+threshold ||
           s.SendBlocked > s.Interval/3 || s.Errors > 0
    if bad {
        c.limits.ChunkSize = max(minChunk, c.limits.ChunkSize/2)
        c.limits.InFlight = max(1, c.limits.InFlight*3/4)
        c.limits.Connections = max(1, c.limits.Connections*3/4)
        c.cooldownUntil = s.At.Add(2*time.Second)
        return c.clamp()
    }
    if s.CPUUtilization > 0.85 || s.MemUtilization > 0.90 ||
       s.QueueDepth > c.maxQueue/2 || s.At.Before(c.cooldownUntil) {
        return c.clamp()
    }
    if c.goodSamples >= 2 {
        target := clamp(int64(c.bw*float64(c.rtt)/float64(time.Second)), minWindow, maxWindow)
        if c.limits.Connections < c.maxConnections {
            c.limits.Connections++
        } else if c.limits.InFlight < maxInFlight {
            c.limits.InFlight++
        } else {
            want := nextPow2(target / int64(c.limits.Connections*c.limits.InFlight))
            if want > c.limits.ChunkSize {
                c.limits.ChunkSize = min(want, maxChunk)
            }
        }
        c.goodSamples = 0
        c.cooldownUntil = s.At.Add(time.Second)
    }
    return c.clamp()
}
~~~

## 9. 代码改动清单

### 9.1 新增文件

| 文件 | 内容 | 风险 |
| --- | --- | --- |
| <code>src/croc/parallel_protocol.go</code> | feature 常量、消息结构、v2 帧编解码/校验 | 协议兼容 |
| <code>src/croc/parallel_send.go</code> | batch sender、per-file 状态、worker pipeline | 并发/内存 |
| <code>src/croc/parallel_receive.go</code> | batch receiver、文件分发、完成/校验 | 并发/磁盘 |
| <code>src/croc/parallel_queue.go</code> | 公平 chunk 队列、bitmap/ranges 消费 | 调度正确性 |
| <code>src/croc/parallel_progress.go</code> | 聚合进度/统计、atomic | UI/竞态 |
| <code>src/resume/bitmap.go</code> | 位图、ranges 互转、计数 | 边界 |
| <code>src/resume/journal.go</code> | journal 读写、checkpoint、恢复 | 崩溃一致性 |
| <code>src/transferperf/probe.go</code> | profile/probe 接口、通用逻辑 | 平台 |
| <code>src/transferperf/probe_linux.go</code> 等 | 平台探测 | build tags |
| <code>src/transferperf/controller.go</code> | AIMD/BDP 控制器 | 振荡 |
| 测试文件 | 单元/集成/fuzz/bench | CI 时间 |

### 9.2 修改文件

| 文件/位置 | 修改 |
| --- | --- |
| <code>src/croc/croc.go:214-270</code> | <code>Client</code> 增加 <code>performance</code>、<code>parallel</code>、<code>resume</code> 相关字段 |
| <code>src/croc/croc.go:2787-2869</code> | <code>activateRelayDataChannels</code> 保持现有对称连接建立；v2 只调整活跃 worker，不单方面打开新索引 |
| <code>src/croc/croc.go:2993</code> | 新增 <code>parallel-batch-ready</code> 分支；v2 不设置 legacy 单文件字段 |
| <code>src/croc/croc.go:3037-3055</code> | v2 批次不使用全局 close-sender/close-recipient；保留 legacy 分支 |
| <code>src/croc/croc.go:3066-3117</code> | v2 发送端 feature 列表加入新能力 |
| <code>src/croc/croc.go:3118-3186</code> | <code>recipientInitializeFile</code> 增加 journal/bitmap 分支 |
| <code>src/croc/croc.go:3187-3254</code> | <code>recipientGetFileReady</code> v2 改为发送 batch-ready |
| <code>src/croc/croc.go:3377-3507</code> | 拆出目标检查 helper；增加 v2 批次选择；并行检查 |
| <code>src/croc/croc.go:3508-3565</code> | <code>updateState</code> 增加 v2 分支，避免启动 legacy 队列 |
| <code>src/croc/croc.go:3595-3770</code> | <code>receiveData</code> 入口检查 v2 并转 <code>receiveDataV2</code>；完成判定用位图 |
| <code>src/croc/croc.go:3772-3860</code> | <code>sendData</code> 保留 legacy；v2 使用 <code>parallel_send.go</code> |
| <code>src/croc/croc.go:753-803</code> | <code>resetForReconnectAttempt</code> 清理 v2 状态 |
| <code>src/croc/croc.go:730-751</code> | <code>closeAttempt</code> checkpoint/关闭 batch 文件与 worker |
| <code>src/croc/chunk_queue.go</code> | 保留 <code>requestedChunkQueue</code> 作为 legacy；新增 <code>parallelChunkQueue</code> |
| <code>src/croc/file_preparation.go:84-116</code> | <code>prepareAllFiles</code> 增加有界并行；保持顺序/错误传播 |
| <code>src/croc/progressive_hash.go</code> | 新增 <code>TypeFilePreparedBatch</code> 处理；评估 exact-hash 单 pending 对批次的瓶颈 |
| <code>src/croc/tailcat_negotiation.go:211-219</code> | <code>pakeFeatures</code> 加入新 feature；可选 stream 上限 |
| <code>src/croc/tailcat_stream_config_default.go:8</code> | 保持默认 8；未来暴露可配置上限 |
| <code>src/message/message.go</code> | 新增消息类型常量 |
| <code>src/cli/cli.go</code> | 新增 flags、校验、帮助文本 |
| <code>src/utils/utils.go:512-600</code> | 新增 bitmap/ranges 互转、预分配辅助 |
| <code>src/croc/client_state.go:151-173</code> | 进度统计 atomic 化；单 ticker 更新 |
| <code>src/croc/terminal_display.go</code> | 批量进度显示 |
| <code>README.md</code>、<code>src/docs</code> | 文档/CLI 说明 |
| <code>web/src/protocol/*</code> | Phase 5 可选：Web v2 支持；否则保持 legacy |

### 9.3 关键接口签名（建议）

~~~go
// src/croc/parallel_protocol.go
func encodeDataFrameV2(dst []byte, batchID uint64, fileID uint32, offset int64, flags byte, payload []byte, aead cipher.AEAD) ([]byte, error)
func decodeDataFrameV2(src []byte, aead cipher.AEAD, maxChunk int) (dataFrameV2, error)

// src/croc/parallel_queue.go
type chunkTask struct { file *fileSendState; offset int64; length int }
type parallelChunkQueue struct { ... }
func (q *parallelChunkQueue) claim() (chunkTask, bool)
func (q *parallelChunkQueue) complete(task chunkTask)

// src/croc/parallel_send.go
func (c *Client) startParallelBatch(attempt *transferAttemptState, ready ParallelBatchReady) error
func (c *Client) startParallelWorker(conn *comm.Comm, batch *batchSendState)

// src/croc/parallel_receive.go
func (c *Client) receiveDataV2(i int, conn *comm.Comm, attempt *transferAttemptState)
func (c *Client) startBatchReceive(ready ParallelBatchReady) error
func (c *Client) completeReceiveFile(state *receiveFileState) error

// src/resume/journal.go
type Metadata struct { FileSize, ModTime int64; SourceHash []byte; HashAlgorithm string; ChunkSize int64; ChunkCount int }
type Journal struct { ... }
func Open(path string, meta Metadata) (*Journal, error)
func (j *Journal) MissingRanges() []int64
func (j *Journal) MarkComplete(chunk int, hash uint64)
func (j *Journal) Checkpoint(data *os.File) error
func (j *Journal) Close() error

// src/transferperf/controller.go
type Controller interface { Initial(SystemProfile, int) Limits; Observe(Sample) Limits; Current() Limits }
~~~

## 10. 分阶段实施

### Phase 0：基线与可观测性（不改变线上协议）

- 新增统计结构：发送/接收字节、块数、重试、每个连接吞吐、发送阻塞时间、峰值内存；debug 日志输出。
- 修复/规避现有并发统计风险：<code>finishedNum</code>、<code>TotalSent</code>、进度回调统一到单 ticker 或 atomic。
- 增加 loopback 集成基准（1 GiB、10k 小文件、不同延迟/带宽模拟）。
- 加入 feature 常量但默认不发送；确保旧测试全绿。
- 退出标准：<code>go test -race</code> 通过；基线数据记录在测试日志/基准文件。

### Phase 1：v2 数据面 + 大文件管道

- 实现 feature 协商、v2 帧、batch-ready/file-done/batch-done、batch of 1。
- 实现公平队列、每连接在途窗口、可变 chunk（首版固定 256 KiB，控制器未接入）。
- 入口分派：<code>receiveData</code>/<code>startSenderChunkQueue</code> 根据 <code>peerParallelData</code> 选择 v2。
- 保留 legacy 路径不变；<code>--no-parallel-data</code> 可强制旧路径。
- 退出标准：1 GiB 文件哈希一致；旧/新互传通过；80 ms RTT 模拟吞吐提升 3 倍以上；race 通过；LAN 无回归。

### Phase 2：分块级续传

- 实现 <code>resume</code> 包、journal、bitmap、checkpoint、恢复校验。
- 接收端 batch-ready 携带 ranges/bitmap；发送端 v2 队列按请求 chunkSize/ranges 发送。
- 完成时整文件哈希；失败保留 journal；重连/进程重启恢复。
- 退出标准：10%/50%/90% kill -9 测试通过；重传不超过一个 checkpoint 窗口；损坏单块可被识别；源文件变化丢弃 journal；旧路径不受影响。

### Phase 3：多小文件并行

- 实现批次选择、并行目标检查、并行准备、per-file 状态、聚合进度。
- 限制 FD/批次大小/总 chunk；处理空文件/符号链接/覆盖提示/rename。
- 与 legacy 边界：<code>--stdout</code>/<code>--sending-text</code>/<code>--no-multiplexing</code> 禁用批次。
- 退出标准：10k × 64 KiB 文件吞吐 3 倍以上；无 FD 耗尽；哈希全部正确；与旧客户端互传正常。

### Phase 4：自适应

- 实现 <code>transferperf</code> 探测、控制器、perf-probe 消息。
- 接入连接数、chunk、in-flight、并行文件数；尊重 throttle/端口/Tailcat/内存上限。
- 退出标准：模拟 10/50/200 ms RTT、10/100/1000 Mbps 下控制器收敛且不振荡；LAN 不回归；内存不超过 256 MiB；探测失败安全降级。

### Phase 4b：可选优化

- per-file 预读 ring、压缩按 CPU 自适应、可变长度帧/中途调 chunk（需新协议版本）、发送端块哈希清单按块修复、TCP_INFO 全平台增强、Web v2。
- 仅在 Phase 1-4 基准显示瓶颈时实施，避免过度设计。

### Phase 5：硬化与发布

- 完整兼容矩阵（Linux/macOS/Windows；relay/Tailcat/local；v11.5.5/新构建/Web）。
- 保持 module path <code>github.com/schollz/croc/v11</code>；不进行 v12 迁移。
- fuzz、race、故障注入、性能回归门禁。
- 文档、CLI 帮助、release notes；按现有版本策略发布，不手动打 tag。

## 11. 配置与 CLI

| Flag | 默认 | 说明 |
| --- | --- | --- |
| <code>--no-parallel-data</code> | false | 强制旧协议，用于排障/兼容（等价于 <code>--parallel-data=off</code>） |
| <code>--parallel-data</code> | auto | auto/off/force；force 仅用于测试/诊断，仍要求对端支持 |
| <code>--parallel-connections</code> | 0（auto） | 1..8；0 表示由控制器决定 |
| <code>--chunk-size</code> | 0（auto） | 64 KiB..4 MiB，2 的幂；0 表示自适应 |
| <code>--parallel-files</code> | 0（auto） | 1..16；0 表示由控制器决定 |
| <code>--no-autotune</code> | false | 使用固定保守参数，不探测 |
| <code>--no-resume</code> | false | 禁用 v2 journal/续传 |
| <code>--resume-verify</code> | full | <code>full</code>/<code>sample</code>/<code>none</code> |
| <code>--stats</code> | false | 结束时输出吞吐/连接/续传摘要 |
| <code>--no-multiplexing</code> | 现有 | 中继路径强制 1 条数据连接，禁用多文件批次；Tailcat 仍使用 stream 配置 |
| <code>--throttleUpload</code> | 现有 | 仍为硬上限；控制器不得突破 |

- <code>--chunk-size</code> 在接收端表示请求值，在发送端表示允许的上限；发送端不得静默改变接收端请求，只能通过 <code>parallel-batch-reset</code> 返回上限。
- <code>--parallel-connections</code> 在发送端表示活跃 worker 上限；接收端为兼容接受，但不因此停止服务已建立连接的 receive goroutine（否则发送端 worker 会阻塞）。
- <code>--parallel-files</code> 在接收端表示批次文件上限；发送端可用 <code>parallel-batch-reset</code> 给出更小的可接受值。
- <code>--no-resume</code> 时 v2 不创建/不加载 journal，已有 partial 目标按全新传输处理（大文件 truncate/覆盖，小文件 temp+rename），不采用零块启发式。
- Flags 需要校验并写入帮助；环境变量可选（沿用 <code>CROC_*</code> 风格）。
- 默认行为：双方支持时启用 v2；<code>--no-parallel-data</code> 可回退；旧端自动降级。
- 文档说明：默认中继只提供 4 条数据连接；更多连接需要自定义 relay <code>--ports</code> 或本地 relay；不要通过重复进程绕过限制。

## 12. 测试与验收

### 12.1 单元测试

- <code>parallel_queue_test.go</code>：多文件轮转、ranges/bitmap、无重复/无遗漏、并发 claim/complete。
- <code>parallel_protocol_test.go</code>：v2 帧编解码、越界/错位/未知 flags/压缩/最后一块、fuzz。
- <code>resume/bitmap_test.go</code>：位图边界、ranges 互转、计数。
- <code>resume/journal_test.go</code>：正常/截断/CRC 错误/元数据不匹配/inflight 恢复。
- <code>transferperf/controller_test.go</code>：合成样本收敛、上下限、滞回、CPU/内存/错误触发退避。
- <code>parallel_send_test.go</code>：worker pipeline 失败传播、在途配额、缓冲归还。
- <code>parallel_receive_test.go</code>：重复块、越界块、完成判定、哈希失败。

### 12.2 集成测试

- 使用现有 loopback relay 测试工具（如 <code>tcp_test.go</code> 的多端口 relay、<code>local_relay_test.go</code>）启动 v2 传输。
- 场景：
  - 1 个大文件（1 GiB/10 GiB 稀疏文件）哈希一致。
  - 10k 个小文件（含空文件/符号链接/子目录）哈希一致。
  - 大小混合批次、<code>--rename</code>、<code>--overwrite</code>、拒绝文件。
  - 旧发送端/新接收端、新发送端/旧接收端、Web 接收端。
  - relay-only、Tailcat、local relay、<code>--no-multiplexing</code>。
- 使用 <code>comm.New</code> + 自定义 <code>net.Conn</code> 包装器模拟延迟/带宽/中断；Linux CI 可选 <code>tc netem</code>。
- 增加 v2 帧/控制消息的 golden fixture（字节级），Go 测试直接比对；Web 端仍使用 v1 fixture，确保自动降级不依赖新协议。

### 12.3 故障注入

| 故障 | 期望 |
| --- | --- |
| 中途关闭一条数据连接 | 重连后只重传未确认块；最终哈希正确 |
| 接收端 kill -9（10/50/90%） | journal 恢复；重传不超过 checkpoint 窗口 |
| journal 截断/CRC 错误 | 丢弃并整文件重传，不崩溃 |
| 磁盘写入返回 ENOSPC | 报错，保留 journal；释放空间后可续传 |
| 源文件传输中被修改 | <code>validateSourceUnchanged</code> 失败；明确报错 |
| 单块内容损坏（测试注入） | 恢复校验发现并重传该块；最终哈希正确 |
| FD 上限很低 | 批次自动缩小，不崩溃 |
| 中继限流/断开 | 控制器退避；连接数下降；重连 |
| 对端发送非法 fileID/offset/长度 | 拒绝并报协议错误，不越界写 |

额外：在 checkpoint 的每个步骤（临界区、fdatasync、快照、写 journal、rename、目录 fsync）后注入强制退出，验证恢复只重传未 durable 的窗口，且 journal 不损坏。

### 12.4 性能矩阵与验收

| 场景 | 指标 | 验收 |
| --- | --- | --- |
| 1 GiB，100 Mbps，80 ms RTT，4 连接 | 吞吐 | 链路 70% 以上 |
| 1 GiB，1 Gbps，1 ms RTT | 吞吐 | 不低于基线 |
| 10k × 64 KiB，30 ms RTT | 总耗时 | 基线 1/3 以下 |
| 1 GiB，50% 处 kill -9 | 重传字节 | 不超过 5% |
| 10k 小文件 | FD 峰值 | 不超过 RLIMIT_NOFILE 的 80% |
| 任意 | 内存 | 不超过 256 MiB 或内存上限 10% |
| 任意 | race | <code>go test -race</code> 干净 |
| 控制器 | 振荡 | 参数不出现连续 5 次以上来回跳变 |

### 12.5 兼容性矩阵

- Linux/macOS/Windows 构建与传输。
- relay-only / Tailcat / local relay。
- v11.5.5 <-> 新构建双向互传（自动回退 v1）。
- 新构建 <-> 新构建使用 v2；Web 接收端使用 v1。
- Go module path 保持 <code>github.com/schollz/croc/v11</code>。
- <code>--no-multiplexing</code>、<code>--throttleUpload</code>、<code>--no-compress</code>、<code>--stdout</code>、<code>--sending-text</code>、<code>--overwrite</code>、<code>--rename</code>。

### 12.6 测试命令

~~~text
go test ./src/croc ./src/utils ./src/resume ./src/transferperf
go test -race ./src/croc ./src/resume ./src/transferperf
go test -run TestParallel -v ./src/croc
go test -bench BenchmarkParallel -benchmem ./src/croc
go test -fuzz FuzzDataFrameV2 ./src/croc
~~~

## 13. 观测与运维

- Debug 日志（默认关闭，<code>--debug</code> 开启）：

~~~text
perf: cpus=8 quota=4.0 mem=16GiB rlimit=1024 disk_r=1.2GB/s disk_w=800MB/s
perf: rtt=42ms bw=92Mbps retrans=0 limits={conns=4 chunk=1MiB inflight=3 files=8}
parallel: batch=1 files=64 bytes=48MiB chunks=12288
resume: file=index3 completed=8192/16384 checkpoint=32MiB
~~~

- <code>--stats</code> 结束时输出：总字节、耗时、平均/峰值吞吐、峰值连接数、chunk 范围、重试次数、续传字节、探测结果；不输出文件名/路径。
- 所有错误继续走 <code>redact.Error</code>；不把 journal 内容/哈希写入普通日志。
- 不在生产默认开启 perf-probe 详细日志，避免噪声。

## 14. 风险与缓解

| 风险 | 影响 | 缓解 |
| --- | --- | --- |
| 中继连接数/公平性限制 | 连接被拒/限速，性能不升反降 | 硬上限 8；relay 错误退避；默认连接数受 banner 约束；文档说明 |
| 内存膨胀 | OOM/GC 抖动 | 总缓冲预算、chunk 上限、in-flight 上限、sync.Pool；超限降级 |
| FD 耗尽 | 大量小文件失败 | 探测 RLIMIT_NOFILE；批次/打开文件上限；完成即关闭 |
| 磁盘满/碎片 | 写入失败、性能下降 | 传输前空间检查；预分配（fallocate/Truncate）；ENOSPC 保留 journal |
| journal 损坏/安全 | 续传错误/路径攻击 | CRC、原子 rename、0600、receivefs 路径校验、失败整文件重传 |
| 并发竞态 | 崩溃/数据错误 | 每文件状态锁/atomic；不共享 legacy 全局字段；race/fuzz 测试 |
| 压缩 CPU 饱和 | 吞吐下降 | 按帧 flags 关闭压缩；控制器 CPU 守卫；<code>--no-compress</code> |
| Tailcat stream 固定 | 高 RTT 下仍不足 | 在 8 条内调 worker；未来暴露 StreamCount；文档说明 |
| 最终哈希开销 | 大文件结束变慢 | 可配置 <code>--resume-verify</code>；后台校验；默认 xxhash 全文件 |
| 与 Web/旧端分叉 | 兼容问题 | feature gate + legacy 路径；Web 保持旧协议；矩阵测试 |
| 进度/提示行为变化 | 用户困惑 | 批次聚合进度；保留 <code>--stdout</code>/文本旧路径；文档 |
| 稀疏文件/NFS 语义 | 续传/预分配异常 | 检测文件系统（可选）；失败回退整文件；不做强假设 |
| 测试不稳定 | CI 噪声 | 固定种子/超时；loopback 模拟而非依赖外网；性能门禁宽松阈值 |
| 新构建默认行为变化 | 用户升级后吞吐/磁盘行为变化，旧脚本使用新 flag 失败 | 默认 auto 但可回退；release notes/文档；灰度与 v11.5.5 兼容矩阵 |
| 版本 tag 复用 | v11.5.5 已发布时复用同一 tag，包管理器/用户混淆 | 不复用已发布 tag；实际发布 tag 由维护者按现有流程选择 |

## 15. 开放决策

> 版本号保持 **v11.5.5**；不进行 major bump，也不迁移 Go module path（保持 <code>/v11</code>）。

1. v2 是否在首个版本默认启用，还是先以 <code>--parallel-data=auto</code> 灰度一个版本。
2. 默认 chunk 上限 4 MiB 是否足够；10 Gbps/低 RTT 场景是否需要 8 MiB。
3. 小文件阈值 1 MiB、批次 256 文件/64 MiB 是否合适；是否需要按 relay 端口数调整。
4. journal 使用 sidecar 还是集中到 <code>.croc-resume/</code> 目录；sidecar 可见但直观，集中目录更整洁。
5. 默认恢复校验 full 还是 sample；full 更安全但大文件恢复慢。
6. 是否在 v2 中要求全文件哈希算法（xxhash/md5），还是允许 imohash 采样算法并文档说明。
7. Web 客户端 v2 的时间点；是否先只做 Go 端。
8. 是否暴露 <code>--chunk-size</code>/<code>--parallel-connections</code> 给高级用户，还是仅 auto + 环境变量。
9. Tailcat StreamCount 是否改为可配置/可动态申请。
10. 大文件与小文件是否允许混合批次；首版建议分开，降低调度复杂度。

## 16. 并发所有权与锁模型

本节把 v2 的并发边界固定下来，避免在现有全局字段上继续叠加竞态。

### 16.1 状态所有权

| 状态/资源 | 唯一所有者 | 同步方式 | 生命周期 |
| --- | --- | --- | --- |
| <code>FilesToTransfer</code>、<code>FilesHasFinished</code>、<code>transferLifecycle</code> | 控制循环 <code>transfer()</code> | 无需锁；只在控制循环读写 | 整个会话 |
| <code>FilesToTransferCurrentNum</code>、<code>CurrentFile</code>、<code>CurrentFileChunkRanges</code>、<code>CurrentFileIsClosed</code> | legacy 路径专用 | <code>receiveMutex</code> / 控制循环 | legacy 单文件 |
| <code>batchSendState</code>（files/queue/workers） | 控制循环创建和销毁 | <code>batch.mu</code> 保护 map/worker 注册；worker 只读不可变字段 | 一个批次 |
| <code>parallelChunkQueue</code> | 发送端 worker 共享 | 内部 <code>mu</code>；<code>claim/complete</code> | 一个批次 |
| <code>fileSendState.file</code> | 发送端 worker | <code>os.File.ReadAt</code> 并发安全；<code>closeOnce</code> 关闭 | 文件发送期间 |
| <code>fileSendState.remaining</code> | 发送端 worker | <code>atomic.Int64</code> | 文件发送期间 |
| <code>batchReceiveState</code>（files map） | 控制循环创建；数据 worker 查找 | <code>batch.mu</code> / <code>RWMutex</code> | 一个批次 |
| <code>receiveFileState</code>（bitmap/count/journal） | 接收端数据 worker | 每文件 <code>mu</code>；<code>WriteAt</code> 在锁外 | 文件接收期间 |
| <code>resume.Journal</code> | 接收端 | 内部 <code>mu</code>；<code>MarkComplete</code> 并发安全；<code>Checkpoint</code> 单写者 | 文件接收期间 |
| <code>parallelProgress</code> | 数据 worker + UI ticker | <code>atomic.Int64</code>；UI 只读 | 整个会话 |
| <code>c.bar</code> | UI ticker | <code>barMu</code>；数据 worker 不直接调用 | 整个会话 |
| <code>c.dataAEAD</code> | 控制循环设置后只读 | 每 worker 独立 AEAD 实例，或确认实现并发安全 | 会话 |
| <code>c.limiter</code> | 所有 worker | <code>rate.Limiter</code> 并发安全 | 会话 |
| <code>attempt.errc</code> / <code>attempt.once</code> | 任一数据/控制 goroutine | <code>sync.Once</code>；只报告第一个错误 | 一个 attempt |

### 16.2 硬性规则

1. **v2 数据 worker 不得读写 legacy 字段**：<code>FilesToTransferCurrentNum</code>、<code>CurrentFile</code>、<code>CurrentFileChunkRanges</code>、<code>CurrentFileIsClosed</code>、<code>TotalSent</code>、<code>TotalChunksTransferred</code>、<code>fread</code>。这些字段由 legacy 单文件路径独占。
2. **先校验后分配**：对端给的 <code>fileID</code>、offset、length、chunkSize、ranges/bitmap、batchID 必须先验证，再查 map 或分配缓冲；禁止用对端长度直接 <code>make([]byte, n)</code>。
3. **锁顺序**：<code>batch.mu -> file.mu -> journal.mu</code>；不得反向。持有 <code>file.mu</code> 时不做 <code>ReadAt/WriteAt/Send/Sync</code> 等 I/O。
4. **文件关闭所有权**：发送端文件由最后一个 <code>remaining==0</code> 的 worker 触发 <code>closeOnce</code>；接收端文件由完成文件状态机触发 <code>closeOnce</code>；其他 goroutine 只读，不关闭。
5. **进度与 UI 解耦**：数据 worker 只更新 atomic 计数；进度条由一个 ticker/控制循环更新，避免 <code>progressbar</code> 并发调用和 <code>finishedNum</code> 竞态。
6. **取消与关闭顺序**：先停止新任务，再等待在途任务，最后 checkpoint/关闭文件与连接；不得先关文件再让 worker 写入。
7. **错误只报告一次**：<code>attempt.report</code> 使用 <code>sync.Once</code>；worker 失败后不再继续 claim，避免错误风暴。
8. **不共享可变 AEAD 状态**：优先每 worker 一个 <code>cipher.AEAD</code>；如果共享，必须用 race 测试证明 Seal/Open 并发安全。
9. **worker panic 必须转为错误**：每个 sender/receiver worker 使用 <code>defer recover()</code>，调用 <code>attempt.report</code>，不能只让 goroutine 退出而留下未完成队列。
10. **控制消息单写者**：v2 的 batch-ready/file-done/batch-done/reset/perf-probe 由控制循环或单一 control writer 发送；数据/校验 worker 通过 event channel 提交完成/错误，不直接调用 <code>message.Send</code>。<code>Comm.writeMu</code> 只保证帧不交错，不保证不同 goroutine 的逻辑顺序。

### 16.3 取消/关闭顺序

1. 触发取消：控制循环收到错误、用户中断、hash 连续失败、磁盘致命错误。
2. 调用 <code>batch.cancel()</code>：关闭 <code>ctx</code>，停止 sender pump 继续 claim；writer 不再接受新帧。
3. 关闭数据连接以唤醒阻塞的 <code>Comm.Send/Receive</code>；<code>closeAttempt</code> 负责关闭 <code>connectionsSnapshot()</code> 中的全部连接。
4. 等待 sender/receiver worker 退出（<code>WaitGroup</code>，带超时）；未完成的块保持“未确认”，由 journal 在重连后重新请求。
5. 接收端对每个打开的文件执行 <code>fdatasync</code>（尽力）-> <code>journal.Checkpoint</code> -> <code>journal.Close</code> -> <code>file.Close</code>；失败时保留 journal，不删除数据。
6. 发送端关闭源文件句柄；不写 journal。
7. 清理 <code>batchSendState/batchReceiveState</code>，重置 <code>parallelActive</code>；<code>resetForReconnectAttempt</code> 还要清空 <code>peerParallelData/peerChunkResume/peerSmallFiles/peerAutotune</code> 等 peer 标志，但保留磁盘 journal；控制循环决定重连或返回错误。
8. 重连成功后重新 PAKE/清单，接收端从 journal 恢复缺失块；发送端按新的 batch-ready 重新建队列。

> 注意：<code>attempt.report</code> 目前只关闭控制连接。数据连接可能仍阻塞在 <code>Send</code>；取消路径必须显式关闭数据连接或设置 deadline，否则 worker 无法退出。

### 16.4 内存预算

发送端峰值（单侧）：

~~~text
send_buffers = conns * inFlight * (chunk + frame_header + aead_overhead) * 2
queue_meta   = files * ~200 B + chunks_meta
journal      = 0 (sender)
total_send   = send_buffers + queue_meta
~~~

接收端峰值：

~~~text
recv_buffers = conns * (chunk + frame_header + aead_overhead) * 2
bitmap       = chunkCount / 8 * 2 (completed + inflight)
chunk_hashes = chunkCount * 8
journal      = bitmap + hashes + header
total_recv   = recv_buffers + per_file_state + journal
~~~

约束：

- 每侧总预算默认 <code>min(256 MiB, 内存上限 * 10%)</code>；发送/接收各自计算，不合并。
- 超预算时按“chunk -> inFlight -> connections -> parallelFiles”的顺序降级。
- 1M chunk 的位图约 250 KiB（两份），块哈希约 8 MiB；可接受。
- 缓冲池按 64 KiB/256 KiB/1 MiB/4 MiB 分档；池内保留上限不超过总预算的 25%。

## 17. 协议与状态机补充

### 17.1 Feature 协商顺序与一致性

| 阶段 | 消息 | 携带方 | 作用 |
| --- | --- | --- | --- |
| PAKE | <code>TypePAKE</code> | 接收端 | 接收端能力 |
| PAKEConfirm | <code>TypePAKEConfirm</code> | 发送端 | 发送端能力 |
| 文件清单 | <code>SenderInfo.Features</code> | 发送端 | 发送端能力复核 |
| 文件请求 | <code>RemoteFileRequest.Features</code> | 接收端 | 接收端能力复核 |

规则：

- 有效能力 = 本地支持 AND PAKE 对端声明 AND 文件清单/请求未否定。任一阶段缺失即回退 v1。
- 如果 PAKE 与 <code>SenderInfo/RemoteFileRequest</code> 不一致，按“取交集”处理并记录 debug 日志；不因单条消息启用 v2。
- <code>pakeFeatures()</code> 输出顺序固定，便于测试对比；新增 feature 只追加，不删除。
- <code>--no-parallel-data</code> 时本地 feature 列表不包含 v2 能力，旧路径完全不受影响。
- 旧对端忽略未知 feature；新对端看到缺失即 false，不需要协议版本号。

### 17.2 批次状态转换

以下表格描述**批次聚合状态**。多文件并发时，以 23.2 的“每文件状态”为准；聚合状态只用于诊断/测试，不阻塞其他文件的正常推进（例如一个文件 retrying 时，其他文件仍可 streaming）。

**发送端**：

| 当前状态 | 事件 | 下一状态 | 动作 |
| --- | --- | --- | --- |
| idle | 收到合法 batch-ready | preparing | 校验、打开源文件、建 per-file 队列 |
| idle | batch-ready 超出本地上限 | idle | 发送 parallel-batch-reset，携带可接受上限；不建队列 |
| preparing | 队列/worker 就绪 | streaming | 启动 pump/writer |
| streaming | 所有文件 remaining==0 | awaitingFileDone | 关闭源文件；等待 file-done |
| streaming | 致命错误 | aborting | cancel、report、保留接收端 journal |
| awaitingFileDone | file-done(ok) 且未全部完成 | awaitingFileDone | 标记文件完成 |
| awaitingFileDone | 全部 file-done(ok) | done | 发送 batch-done |
| awaitingFileDone | file-done(hash-mismatch) | retryingFile | 重开文件、重建该文件队列（首版整文件重试） |
| retryingFile | 重试成功 | awaitingFileDone | 继续等待 |
| retryingFile | 重试失败/超限 | aborting | 发送 batch-done(error) |
| aborting | 清理完成 | idle | 控制循环重连或返回 |

**接收端**：

| 当前状态 | 事件 | 下一状态 | 动作 |
| --- | --- | --- | --- |
| idle | 选中批次 | selecting | 检查目标、加载 journal、校验 completed |
| selecting | 请求构建完成 | readySent | 发送 batch-ready |
| readySent | 首个 data frame | receiving | 建立/确认 per-file 状态 |
| readySent | 收到 parallel-batch-reset | selecting | 按 MaxChunkSize/MaxBatchFiles 重建请求；只重试一次 |
| receiving | 文件位图满 | fileCompleting | Sync、整文件 hash、rename/journal 清理 |
| fileCompleting | hash ok 且未全部完成 | receiving | 发送 file-done(ok)，继续接收其他文件 |
| fileCompleting | hash ok 且全部完成 | awaitingBatchDone | 发送 file-done(ok)，等待 batch-done |
| fileCompleting | hash 失败 | retryRequested | 清空该文件 completed 位图/重建 journal，发送 file-done(hash-mismatch) |
| retryRequested | file-done 已发送 | receiving | 保持批次 active，等待发送端重传该文件 |
| awaitingBatchDone | batch-done(ok) | idle | 清理批次，选择下一批或发送 finished |
| 任意 | 致命错误/取消 | aborting | checkpoint、关闭、report |

补充：接收端的 <code>receiveDataV2</code> goroutine 可以在批次建立前启动；它应阻塞在 <code>batchReady</code> channel（带 context 超时），不要 sleep 轮询。批次状态用锁或 <code>atomic.Pointer</code> 保护；晚加入的 receive goroutine 如果批次已存在，直接 attach 当前批次。正常情况下发送端收到 batch-ready 后才发数据；若 data frame 先于 batch-ready 到达，按协议错误关闭该连接，避免把帧写入错误状态。

### 17.3 时序：大文件（batch of 1）

~~~text
接收端                                        发送端
  |--- batch-ready(file 0, ranges) ----------->|
  |                                           | 校验/打开/建队列
  |<---------- data frame(fileID=0) -----------|
  |<---------- data frame(fileID=0) -----------|
  |                     ...                    |
  |--- file-done(file 0, ok) ----------------->|
  |<---------- batch-done(ok) -----------------|
  |--- finished ------------------------------>|
~~~

### 17.4 时序：多小文件

~~~text
接收端                                        发送端
  |--- batch-ready(files 3,4,5,6) ------------>|
  |                                           | 4 个 per-file 队列
  |<-- data(3) -- data(4) -- data(5) -- data(6) ...
  |--- file-done(3) -------------------------->|
  |--- file-done(4) -------------------------->|
  |--- file-done(5) -------------------------->|
  |--- file-done(6) -------------------------->|
  |<---------- batch-done(ok) -----------------|
  |--- next batch-ready / finished ----------->|
~~~

### 17.5 时序：崩溃后恢复

~~~text
接收端重启
  | 读取 batch 选择所需的文件清单
  | 加载 dest.crocstate，校验 completed，inflight->missing
  |--- batch-ready(file 0, bitmap/ranges) ---->|
  |                                           | 只入队缺失块
  |<---------- data frame(fileID=0) -----------|
  |--- file-done(0, ok) ---------------------->|
  |<---------- batch-done(ok) -----------------|
~~~

### 17.6 时序：重连

~~~text
数据连接错误
  -> attempt.report(transferDisconnectError)
  -> 控制连接关闭，transferWithReconnect 接管
  -> closeAttempt：停止 worker、checkpoint journal、关闭文件/连接
  -> resetForReconnectAttempt：清空 v2 内存状态，保留 journal
  -> 重新 PAKE、重新文件清单
  -> 接收端重新加载 journal，batch-ready 只请求缺失块
~~~

### 17.7 消息/帧校验

| 对象 | 校验 |
| --- | --- |
| file-prepared(-batch) | 安全通道后；发送端发送、接收端接收；index 在范围内且不重复；hash 非空；单批不超过 256 项 |
| batch-ready | 安全通道已建立且文件清单已完成；BatchID>0；files 非空且不超过上限；index 不重复且在范围内；size 与清单一致且>0；chunkSize 为 64 KiB..4 MiB 的 2 的幂；ranges/bitmap 二选一且合法；总 chunk 数不超限 |
| file-done | BatchID 匹配；index 在当前批次；status 为允许枚举；bytes 不超过文件大小；status=ok 时必须带非空 hash 且与清单 hash 一致（skipped 可空） |
| batch-done | BatchID 匹配；status 合法；files 结果与请求索引一致 |
| batch-reset | BatchID 匹配；reason 非空；MaxChunkSize 在协议范围内；MaxBatchFiles&gt;0 |
| data v2 | version=2；flags 未知位为 0；batchID 匹配；fileID 存在；offset 对齐；offset+len<=size；非最后一块 len==chunkSize；最后一块 len==size-offset；解压后不超过 header+chunkSize |
| perf-probe | 仅在安全通道后；长度/序号合法；限速；未协商则忽略 |
| perf-probe-reply | 仅在安全通道后；序号匹配未完成 probe；限速；未协商则忽略 |
| 未知消息 | 已知但未启用的 feature -> 协议错误；完全未知 -> debug 忽略（前向兼容） |

### 17.8 Ranges / Bitmap 语义

- <code>Ranges</code> 格式沿用 legacy：<code>[chunkSize, startChunk, count, startChunk, count, ...]</code>。v2 中 <code>ranges[0]</code> 必须等于请求的 <code>ChunkSize</code>，否则按协议错误处理。
- v2 的 batch-ready 只包含“仍有缺失块”的文件；完整文件不出现在批次里，避免“空 ranges = 全部”的歧义。
- 全缺失编码为 <code>[chunkSize, 0, chunkCount]</code>；部分缺失按升序、不重叠。校验 <code>start/count &gt;= 0</code>、<code>count &gt; 0</code>、<code>start+count &lt;= chunkCount</code>，并拒绝重叠/乱序。
- <code>Bitmap</code> 与 <code>Ranges</code> 互斥且必须恰好一个非空；v2 中空 ranges 不再表示“全部块”。bit i=1 表示缺失。长度必须为 <code>ceil(chunkCount/8)</code>，末尾多余位必须为 0。
- 缺失段超过 100k 时优先用 bitmap；发送端可以直接按 bitmap 消费，也可以转换为 ranges。全 0 bitmap 表示没有缺失，文件不应出现在请求中，收到时按协议错误处理。
- <code>ChunkSize</code> 必须与 journal 的 chunkSize 一致；不一致时丢弃 journal，整文件重传（不能把旧位图映射到新粒度）。

## 18. 错误分类与恢复矩阵

| 错误类别 | 例子 | 检测点 | 处理 | journal |
| --- | --- | --- | --- | --- |
| 瞬时网络 | EOF、reset、超时、relay 断开 | data/control Receive/Send | <code>transferDisconnectError</code>；按现有 10 次重连退避 | 保留，重连后恢复 |
| relay 准入/限流 | room full、handshake 拒绝、限速 | 连接建立/发送错误 | 降低连接目标；指数退避；不无限重试 | 保留 |
| Tailcat 建立失败 | DERP/direct 失败 | tailcat 协商 | 走现有 relay 回退；v2 状态清理 | 保留 |
| 协议违规 | 非法 fileID/offset/ranges、重复 batch、错误 hash 长度 | 解码/校验 | 立即 abort；不重连；日志脱敏 | 保留（供人工恢复） |
| chunk/batch 超限 | ChunkSize 超出本地上限、批次文件过多 | batch-ready 校验 | 发送 batch-reset 给出上限；接收端重试一次；再失败 abort | 保留 |
| 源文件变化 | 大小/mtime/inode 变化 | <code>validateSourceUnchanged</code> | abort；提示用户重新开始 | 接收端保留 |
| 目标磁盘 | ENOSPC、EIO、权限拒绝 | <code>WriteAt/Sync/Rename</code> | abort；不自动重试；保留 journal | 保留 |
| 哈希不匹配 | 整文件 hash 与清单不符 | 文件完成 | 清空 completed/重建 journal；整文件重试 1 次；再失败 abort | 重置后保留 |
| journal 损坏 | magic/CRC/长度错误 | 加载 | 丢弃 journal；整文件重传 | 删除重建 |
| FD 耗尽 | EMFILE | open | 缩小批次/延后文件；持续失败则 abort | 保留 |
| 用户取消 | context canceled | 控制循环 | checkpoint、关闭、返回取消错误 | 保留 |
| 重复/迟到块 | 已完成块再次到达（receiving/retrying） | data v2 | 内容相同则幂等忽略；内容不同则清除 completed 并按 file-done(hash-mismatch) 重传 | 清除对应块 |
| 未知文件块 | fileID 不在批次 | data v2 | 协议错误；abort | 保留 |
| 超大帧 | 超过 maxChunkSizeV2 | 解码 | 协议错误；abort | 保留 |
| 限速等待 | rate.Limiter delay | writer | 不是错误；控制器不视为拥塞 | 不变 |

重试预算：

- 网络重连沿用 <code>maxReconnectAttempts=10</code>。
- 单文件 hash 重试 1 次；同一批次最多 1 次整批重试。
- 协议违规、源文件变化、磁盘致命错误不自动重试。
- 重连后重新走 PAKE/清单；不重用旧 batchID。

## 19. 边界场景清单

| 场景 | 期望行为 | 实现要点 |
| --- | --- | --- |
| 单个空文件 | 不进入数据批次；创建后完成 | 现有 <code>createEmptyFileAndFinish</code> |
| 全部为空文件/空目录 | 不启动数据连接也可完成 | 清单/空目录流程 |
| 只有符号链接 | 元数据创建；不进入批次 | <code>Symlink</code> 字段校验 |
| 单文件超大 | batch of 1；可用 v2 窗口 | 不因批次大小为 1 回退 |
| 1 万个小文件 | 分批并行；FD 受限 | batch caps + FD probe |
| 大小混合 | 默认小文件成批、大文件单独 | 避免大文件挤占小文件 |
| 重名/rename | 接收端生成唯一目标名 | 在 batch-ready 前完成改名 |
| overwrite/ask | 先交互再入批；已有 partial 默认续传 | 批量中不阻塞等待输入；如需全新传输，用户先删除目标/journal |
| <code>--stdout</code>/文本 | 回退 legacy | 输出顺序敏感 |
| <code>--no-compress</code> | 帧 flags 不压缩 | 每帧标志 |
| <code>--throttleUpload</code> | 硬上限 | 控制器不突破 limiter |
| local/only-local/IP | 使用本地 relay 端口数 | 不覆盖 <code>localRelayPort</code> |
| relay/Tailcat/derp | 均支持 v2 帧 | 数据连接来源无关 |
| <code>--no-multiplexing</code> | 中继 1 条连接；Tailcat 按配置 | 禁用批次或仅单文件 |
| SOCKS/HTTP 代理 | 连接数受代理限制 | 保守初始连接；错误退避 |
| IPv6/local relay | 地址拼接正确 | 复用现有 normalize |
| Windows 路径/保留名 | 通过 receivefs 校验 | <code>os.Root</code> 容器 |
| 大小写不敏感 FS | 重名检测按平台语义 | 使用 Root.Stat/Lstat |
| 源文件传输中修改 | 失败并提示 | snapshot + validate |
| 目标被用户修改 | 以 journal/hash 为准 | 完成时复核 |
| 磁盘满/只读 | 明确错误；保留 journal | 预检查 + 错误映射 |
| 网络文件系统/稀疏文件 | 允许但性能降级 | 预分配可选；失败回退 |
| 进程崩溃 | journal 恢复 | checkpoint 顺序 |
| 同时多个 croc 进程 | 各自独立限流 | 无跨进程协调；文档说明 |
| 32 位平台/大文件 | int64 offset；uint64 chunk | 避免 int 溢出 |
| 时钟跳变 | 使用 monotonic 计时 | RTT/采样不用 wall clock |
| FD 上限低 | 自动缩小批次 | RLIMIT_NOFILE |
| 杀毒/文件锁 | 重试 rename/close | 有界重试 |
| 旧版零块部分文件 | 默认整文件重传 | 首版不信任零块启发式；后续可提供显式 opt-in 工具 |
| journal 与源不匹配 | 丢弃 journal | size/hash/chunkSize 校验 |
| Web 接收端 | 回退 v1 | feature 缺失 |
| 重连发生在 batch-ready 后 | 重新请求缺失 | journal + 新 batchID |
| 数据连接晚加入 | 注册为 worker | 不重复打开索引 |
| 乱序/重复块 | 位图幂等 | 重复内容不一致报错 |
| 最后一块短 | 允许 final 短块 | 长度精确校验 |
| 中途改 chunkSize | 禁止 | 每文件固定；下次传输再变 |
| batchID 回绕 | uint64 单调；会话内不复用 | 重连重置 |
| 恶意 peer | 校验/限长/限速 | 不越界写、不无限分配 |

## 20. 实施任务拆解

| ID | 任务 | 交付/验收 |
| --- | --- | --- |
| P0-1 | 统计与进度原子化；单 ticker 更新 UI | race 通过；基线数据 |
| P0-2 | feature 常量/协商脚手架（默认关闭） | 旧测试全绿；feature 矩阵单测 |
| P0-3 | 可模拟延迟/带宽/断线的 net.Conn 测试工具 | 可复现实验 |
| P0-4 | 现状基准 + chunk/在途窗口 spike | 验证高 RTT 欠载假设，确定默认 chunk/窗口范围 |
| P1-1 | v2 帧编解码 + 校验 + fuzz | 帧边界/恶意输入测试 |
| P1-2 | batch-ready/file-done/batch-done/batch-reset 消息与校验 | 状态机单测 |
| P1-3 | 公平 chunk 队列（多文件轮转/bitmap） | 无重复/无遗漏/并发测试 |
| P1-4 | 发送端 batch engine + per-conn pipeline | 1 GiB loopback 哈希一致 |
| P1-5 | 接收端 batch engine + 分发 + 完成 | 乱序/重复块测试 |
| P1-6 | 入口分派与生命周期（legacy 不变） | 新/旧互传通过 |
| P1-7 | 聚合进度/统计/日志 | UI 无竞态；debug 可读 |
| P1-8 | loopback 多端口集成测试 | 1 GiB、10k 小文件 |
| P1-9 | 兼容矩阵（旧/新/Web/relay/Tailcat） | 矩阵全绿 |
| P2-1 | resume bitmap/journal + checkpoint | 截断/CRC/恢复单测 |
| P2-2 | 接收端 journal 加载/校验/ranges | kill -9 恢复 |
| P2-3 | 发送端 ranges/bitmap + 请求 chunkSize | 任意缺失范围正确 |
| P2-4 | 关闭/重连 checkpoint 顺序 | 无丢状态/无死锁 |
| P2-5 | 故障注入（断线/ENOSPC/损坏块/FD） | 预期矩阵通过 |
| P2-6 | 整文件 hash 复核 + 重试策略 | 哈希一致/失败可控 |
| P3-1 | 批次选择/提示/rename/unchanged | 大目录正确 |
| P3-2 | 并行目标检查/并行准备 + PreparedBatch | 10k 文件启动加速、控制消息不膨胀 |
| P3-3 | per-file 状态/公平调度/FD 上限 | 无 FD 耗尽 |
| P3-4 | 多文件进度/统计 | 输出稳定 |
| P3-5 | 10k 小文件集成/兼容测试 | 吞吐目标 |
| P4-1 | CPU/内存/磁盘/FD/网卡探针 | 平台回退 |
| P4-2 | AIMD/BDP 控制器 + 单测 | 收敛/不振荡 |
| P4-3 | 活跃 worker 目标；未来协调式增量打开 | 动态增减不阻塞 |
| P4-4 | perf-probe 消息 + RTT/重传采样 | 采样准确 |
| P4-5 | 控制器接入 chunk/inFlight/files | 参数生效 |
| P4-6 | 性能矩阵/回归门禁 | 达标/无回归 |
| P5-0 | 版本/tag 检查：保持 <code>/v11</code>，不复用已发布 tag | <code>go build ./...</code>、module path 未变 |
| P5-1 | 如发布：Prepare Release + 资产/签名/版本校验 | 发布门禁全绿 |

任务依赖：P0 -> P1 -> P2 -> P3；P4 可在 P1 后并行开发，但默认只在 P2/P3 稳定后启用。每个任务必须包含单元测试、race 覆盖和文档更新；性能任务必须记录基线。

## 21. 设计取舍与备选方案

| 方案 | 取舍 | 决定 |
| --- | --- | --- |
| 多进程数据面 | 需共享密钥/队列/日志；无网络 I/O 收益 | 不做；用 goroutine |
| 每文件独立连接 | 实现简单但浪费连接、顺序受限 | 不做；共享批次队列 + fileID |
| 固定 32 KiB chunk | 低延迟但高开销/低 BDP | 每文件可变 chunk |
| 中途改变 chunk | 需要可变长度帧和位图重映射 | 首版不做；每文件固定 |
| 零块启发式续传 | 无额外状态但会误判 | 仅 legacy 回退；v2 用 journal |
| JSON journal | 易调试但大文件膨胀 | 二进制位图 + JSON/binary header |
| 一批在途 vs 多批流水 | 多批可隐藏 RTT，但状态/内存复杂 | 首版一批；后续可加窗口 |
| 小文件打包成单流 | 减少帧开销，但按文件完成/续传复杂 | 首版每文件帧；后续评估 |
| FEC/纠删码 | TCP 已可靠；实现复杂 | 不做 |
| AIMD vs 固定并发 | AIMD 需调参但适应网络 | 用 AIMD；保留手动覆盖 |
| 接收端决定 chunk | 与位图/续传一致；发送端可下调 | 接收端决定 |
| 降低连接数时关闭连接 | 省资源但易被误判断线 | 只让 worker 退出，保留连接 |
| 块哈希清单 | 精确定位坏块，但元数据大 | 首版 journal 本地哈希；后续可选 |
| Web v2 | 需同步 TS 实现 | 首版不做；自动回退 |

## 22. 性能实验与灰度发布

### 22.1 实验矩阵

| ID | 场景 | 变量 | 指标 |
| --- | --- | --- | --- |
| E1 | 1 GiB，1 Gbps LAN | 基线 / v2 / v2+autotune | 吞吐、CPU、RSS |
| E2 | 1 GiB，100 Mbps，80 ms RTT | 同上 | 吞吐、p95 完成时间 |
| E3 | 1 GiB，10 Mbps，200 ms RTT | 同上 | 吞吐、重传 |
| E4 | 10k × 64 KiB，30 ms RTT | 同上 | 总耗时、文件/秒 |
| E5 | 1 GiB，50% kill -9 | v2+resume | 重传字节、恢复时间 |
| E6 | 混合大/小文件 | 同上 | 尾延迟、FD 峰值 |
| E7 | 4 核/2 GiB 容器 | 同上 | CPU 节流、OOM |
| E8 | relay/Tailcat/local | 同上 | 路径差异、回退 |

工具：

- loopback 多端口 relay + 自定义 <code>slowConn</code>（延迟/带宽/断线）。
- Linux <code>tc netem</code> 或 CI 等价工具；macOS Network Link Conditioner 可选。
- <code>fio</code> 测磁盘基线；<code>pprof</code> 看 CPU/内存；<code>strace -c</code> 看 syscall/chunk。
- 统计：吞吐、p50/p95 完成时间、CPU%、RSS、syscall/chunk、重传、重传字节、FD 峰值。

### 22.2 灰度与回滚

1. **开发/CI**：默认关闭 v2，使用 <code>--parallel-data=force</code> 开启；跑完整兼容/故障测试。
2. **Beta**：默认 <code>auto</code>，但只在双方声明 feature 时启用；收集本地 <code>--stats</code> 日志（不上报）。
3. **默认**：达到验收指标后默认 auto，并按现有版本策略发布；保留 <code>--no-parallel-data</code>、<code>--no-autotune</code>、<code>--no-resume</code>。保持 module path <code>/v11</code> 不变。
4. **回滚**：用户可用 <code>--no-parallel-data</code> 回退；也可 <code>--chunk-size=32768 --parallel-files=1 --no-autotune</code> 近似旧行为。relay/Tailcat 协议未变，旧客户端不受影响。
5. **发布门禁**：性能矩阵达标、race/fuzz 干净、兼容矩阵全绿、无 P0/P1 缺陷、文档/帮助更新。

### 22.3 观测指标

- <code>croc_parallel_batch_files</code>、<code>croc_parallel_active_workers</code>、<code>croc_parallel_inflight_bytes</code>
- <code>croc_parallel_chunk_size</code>、<code>croc_parallel_connections</code>
- <code>croc_resume_completed_chunks</code>、<code>croc_resume_retransmit_bytes</code>
- <code>croc_perf_rtt_ms</code>、<code>croc_perf_goodput_bps</code>、<code>croc_perf_retransmits</code>
- 仅本地 debug/stats 输出，不默认上报；避免泄露路径/文件名。

## 23. 协议状态机边界审计

### 23.1 审计范围与结论

本节逐条检查 v2 批次状态机在以下维度的组合：批次生命周期、单文件生命周期、连接生命周期、feature 协商、控制/数据消息顺序、重连/取消/重试、资源错误。结论：

1. **原 17.2 的单一批次状态表不足以描述多文件并发。** 一个批次内可能同时存在“文件 A 在校验、文件 B 在发送、文件 C 在重试”的组合，必须拆成“批次聚合状态 + 每文件状态 + 每连接状态”，否则状态转移会出现歧义。
2. **必须定义幂等规则。** 重复的 batch-ready、file-done、batch-done、data frame 在可靠 TCP 或重连边界上都可能出现；相同内容幂等忽略，冲突内容必须报协议错误或触发明确重试。
3. **必须用 batchID 拒绝过期帧。** 仅靠 fileID 无法区分“上一批同索引文件的迟到帧”；数据帧已增加 batchID。
4. **batch-reset 只能在首批数据之前使用。** 一旦接收端开始按旧 chunkSize 写盘，发送端再改 chunkSize 会导致 offset/位图错位；此时只能 abort/重连。
5. **hash 失败重试必须重置 completed 位图。** 否则重复块被幂等忽略，重试永远无法修复数据。
6. **重连/取消必须按状态定义清理顺序。** 在 selecting、streaming、verifying、retrying 中取消，journal/文件/连接的关闭顺序不同，但都必须保留可恢复状态。

审计未发现“在单活跃批次 + 单 control writer + 严格校验”前提下无法实现的死锁；主要风险是状态遗漏，不是协议不可行。

### 23.2 状态分层

| 层 | 状态 | 所有者 | 说明 |
| --- | --- | --- | --- |
| Session | featureNegotiated、fileInfoDone、batchID、finished | 控制循环 | 整个连接会话 |
| Batch | idle、selecting、readySent、streaming、awaitingFileDone、retrying、awaitingBatchDone、aborting | 控制循环 | 聚合状态，由文件状态推导 |
| Sender file | pending、queued、sending、sent、retrying、done、failed | batch sender + worker | 每文件独立 |
| Receiver file | inspecting、ready、receiving、verifying、retrying、done、failed | batch receiver | 每文件独立 |
| Connection | connecting、active、idle、failed | transport | 实际 TCP/Tailcat 连接 |

#### 23.2.1 发送端每文件状态

| 当前 | 事件 | 下一状态 | 动作 |
| --- | --- | --- | --- |
| pending | 批次开始/文件打开 | queued | 建 per-file ranges/队列 |
| queued | 首个 chunk 被 claim | sending | 启动读取/加密/send |
| sending | remaining==0 且 writer 排空 | sent | 关闭源文件，等待 file-done |
| sent | file-done(ok) | done | 标记完成，更新计数 |
| sent | file-done(hash-mismatch) | retrying | 重开文件，重建该文件队列；其他文件不受影响 |
| retrying | 重试 chunk 开始 | sending | 重新发送 |
| retrying | 重试次数超限 | failed | abort 批次 |
| 任意 | 致命错误/取消 | failed | 停止该文件，abort 批次 |

#### 23.2.2 接收端每文件状态

| 当前 | 事件 | 下一状态 | 动作 |
| --- | --- | --- | --- |
| inspecting | 目标/journal 检查完成 | ready | 生成 ranges/bitmap |
| ready | 首个 data frame | receiving | 打开/确认目标文件 |
| receiving | 位图满 | verifying | Sync、整文件 hash |
| verifying | hash ok | done | 删除 journal、rename/chteimes、发 file-done(ok) |
| verifying | hash 失败 | retrying | 清空 completed/重建 journal、发 file-done(hash-mismatch) |
| retrying | 收到重传 data frame | receiving | 重新累计位图 |
| retrying | 重试超限 | failed | abort 批次 |
| 任意 | 致命错误/取消 | failed | checkpoint、关闭、report |

#### 23.2.3 批次聚合状态

批次状态不是单一线性状态，而是由文件状态推导：

~~~text
if any file failed            -> aborting
else if any file retrying     -> retrying
else if any file receiving/sending -> streaming
else if all files sent && any awaiting done -> awaitingFileDone
else if all files done && receiver -> awaitingBatchDone
else if receiver正在选文件    -> selecting / readySent
else                          -> idle
~~~

这样可以表达“文件 A 在 verifying、文件 B 在 streaming、文件 C 在 retrying”，而不会被迫把整个批次标成单一状态。

### 23.3 边界事件矩阵

| # | 场景 | 合法前置 | 期望处理 |
| --- | --- | --- | --- |
| 1 | batch-ready 在 idle 到达 | idle | 校验后进入 readySent |
| 2 | batch-ready 在批次活跃时到达 | selecting/readySent/streaming | 协议错误，不改变状态 |
| 3 | batch-ready 重复同一 BatchID | readySent/streaming | 协议错误；不重复建队列 |
| 4 | batch-ready 使用旧 BatchID | 新批次 idle | 协议错误；旧批次已结束 |
| 5 | batch-ready files 为空 | idle | 协议错误 |
| 6 | batch-ready index 重复 | idle | 协议错误 |
| 7 | batch-ready index 越界/已完成/已拒绝 | idle | 协议错误或跳过；不得重新传输已完成文件 |
| 8 | batch-ready size 与清单不一致 | idle | 协议错误 |
| 9 | batch-ready 包含空文件/符号链接 | idle | 协议错误；空文件走元数据流程 |
| 10 | batch-ready chunkSize 非法 | idle | 协议错误 |
| 11 | batch-ready chunkSize 超出本地上限 | idle | 发送 batch-reset，携带 MaxChunkSize |
| 12 | batch-ready ranges/bitmap 非法/为空/全 0 | idle | 协议错误 |
| 13 | batch-ready 总 chunk/总字节超限 | idle | 小文件批次 reset；大文件单文件放行 |
| 14 | batch-reset 在首批数据前 | readySent | 接收端丢弃旧状态，按上限重建 |
| 15 | batch-reset 在首批数据后 | streaming | 协议错误；只能 abort/重连 |
| 16 | batch-reset 重复 | readySent | 协议错误；同一批次只允许一次 reset 重试 |
| 17 | file-done 在 batch-ready 前 | idle/selecting | 协议错误 |
| 18 | file-done BatchID 不匹配 | 任意 | 协议错误 |
| 19 | file-done index 不在批次 | 任意 | 协议错误 |
| 20 | file-done 重复且内容一致 | sent/done | 幂等忽略 |
| 21 | file-done 重复但状态/hash 冲突 | sent/done | 协议错误 |
| 22 | file-done(ok) 缺少 hash | sent | 协议错误 |
| 23 | file-done(ok) hash 与清单不一致 | sent | 触发该文件重试/abort |
| 24 | file-done(error) | sent | abort 批次，保留 journal |
| 25 | file-done(skipped) | sent | 标记 skipped，不计入成功 |
| 26 | file-done 在 preparing/streaming 到达 | 未发送完 | 协议错误 |
| 27 | batch-done 在全部 file-done 前 | awaitingFileDone | 协议错误 |
| 28 | batch-done 缺少/多出 file 结果 | awaitingBatchDone | 协议错误 |
| 29 | batch-done 重复 | idle | 幂等忽略 |
| 30 | batch-done 在 verifying 中到达 | receiving/verifying | 协议错误；发送端不应提前发送 |
| 31 | data frame 在 batch-ready 前 | readySent 之前 | 协议错误/关闭连接 |
| 32 | data frame 在 batch-done 后 | idle | 过期帧，debug 忽略；不写入 |
| 33 | data frame batchID 不匹配 | 任意 | 协议错误；拒绝写入 |
| 34 | data frame fileID 不在批次 | 任意 | 协议错误 |
| 35 | data frame 指向 done 文件 | done | 一律忽略（debug），不写入/不回退；最终 hash 已保证完成状态 |
| 36 | data frame 指向 retrying 文件 | retrying | 接受，转 receiving |
| 37 | data frame 指向 failed 文件 | failed | 协议错误 |
| 38 | data frame offset 未对齐/越界/溢出 | receiving | 协议错误 |
| 39 | data frame 非最后一块 len!=chunkSize | receiving | 协议错误 |
| 40 | data frame 最后一块 len!=size-offset | receiving | 协议错误 |
| 41 | data frame final flag 与实际不符 | receiving | 协议错误 |
| 42 | data frame 解压后超过上限/解压失败 | receiving | 协议错误；abort 或重连 |
| 43 | data frame AEAD 失败 | receiving | abort/重连；不得写入 |
| 44 | data frame 重复且内容相同 | receiving | 幂等忽略 |
| 45 | data frame 重复且内容不同 | receiving | 清除该块 completed，按 hash-mismatch 重传 |
| 46 | 一个文件 verify 时其他文件仍在 streaming | streaming | 合法；按文件独立完成 |
| 47 | 一个文件 hash 失败时其他文件仍在 streaming | streaming/retrying | 合法；该文件重试，其他文件继续 |
| 48 | 重试成功 | retrying | 回到 done；批次继续 |
| 49 | 重试超限 | retrying | abort 批次；journal 保留 |
| 50 | 全部文件 done | awaitingBatchDone | 只等待 batch-done，不再接受 data |
| 51 | batch-done(ok) | awaitingBatchDone | 清理批次，选下一批或 finished |
| 52 | batch-done(error) | 任意活跃 | abort；checkpoint/保留 journal |
| 53 | 数据连接失败 | streaming/awaitingFileDone | abort attempt；重连；journal；新 BatchID |
| 54 | 控制连接失败 | 任意 | abort attempt；按现有 10 次重连 |
| 55 | 取消发生在 selecting | selecting | 关闭句柄；仅删除本次新建且未写数据的临时 journal，已有 resume journal 保留 |
| 56 | 取消发生在 streaming | streaming | 停止 claim，checkpoint，关闭 |
| 57 | 取消发生在 verifying | verifying | 等待/取消校验，checkpoint，关闭 |
| 58 | 重连发生在 retrying | retrying | 该文件 journal 已清空/重建；其他文件 journal 保留；新批次全量请求该文件 |
| 59 | 重连时部分文件已 done | 任意 | 已完成文件按 hash 跳过，未完成按 journal 恢复 |
| 60 | feature 不匹配发生在首批前 | idle | 回退 v1 |
| 61 | feature 不匹配发生在 v2 活跃后 | streaming | abort/重连；不得静默降级 |
| 62 | TypeFinished 在批次活跃时到达 | streaming/awaiting | 协议错误 |
| 63 | TypeFinished 在全部文件前到达 | 未完成 | 协议错误 |
| 64 | TypeFinished 重复 | finished | 幂等忽略 |
| 65 | v2 活跃时收到 legacy close/ready | 任意 | 协议错误 |
| 66 | legacy 活跃时收到 batch 消息 | 任意 | 协议错误 |
| 67 | 未知消息 | 任意 | 已知未启用 feature -> 错误；完全未知 -> debug 忽略 |
| 68 | perf-probe 在批次中到达 | 任意 | 合法；限速/回显 |
| 69 | FD 耗尽发生在批次中 | streaming | abort；缩小批次后重连 |
| 70 | 磁盘满/源文件变化/worker panic | 任意 | abort；保留 journal；不自动重试 |

### 23.4 状态不变量

实现和测试必须保证：

1. 同一会话最多一个活跃批次；batch-ready 不得在活跃批次上叠加。
2. BatchID 会话内单调且不复用；数据帧/控制消息的 BatchID 必须匹配当前批次。
3. 数据帧的 fileID 必须在当前批次；offset/length/final 必须与文件大小和 chunkSize 一致。
4. 单文件 completed 位图只能 missing -> inflight -> completed；completed -> missing 只允许在 hash-mismatch/冲突块明确重置时发生。
5. 没有完整位图 + 整文件 hash 验证，不得发送 file-done(ok)。
6. 没有全部 file-done(ok/skipped)，不得发送 batch-done(ok)。
7. batch-done 或 TypeFinished 之后不再接受任何 data frame。
8. 每文件状态只能前进；回退仅限显式 retrying 路径。
9. v2 控制消息由单 control writer 发送；数据 worker 只提交事件。
10. Journal 是 completed 状态的持久化来源；恢复时 inflight 一律转 missing。
11. 取消/关闭顺序：停止新任务 -> 等待在途 -> checkpoint journal -> 关闭文件 -> 关闭连接。
12. feature 回退只允许在首批 v2 数据之前；v2 活跃后只能 abort/重连，不能静默降级。
13. 单文件 hash 重试最多 1 次；批次重试最多 1 次；重连沿用 10 次上限。
14. 不允许静默改变 chunkSize；变化必须经过 batch-reset + 重新建 journal。
15. 不允许静默改变批次文件集合；sender/receiver 都以 batch-ready 的索引集合为准。

### 23.5 审计发现与计划修改

本次审计对计划做了以下补充：

- 增加每文件状态表和批次聚合状态推导，替代单一批次状态表。
- 增加 batch-ready/file-done/batch-done/reset/data 的幂等与冲突规则。
- 明确 batch-reset 只能在首批数据前；数据开始后只能 abort/重连。
- 明确 data frame 的 batchID/fileID/offset/length/final 校验和过期帧处理。
- 明确 file-done(ok) 必须带 hash 且与清单一致；hash 失败重试前必须清空 completed。
- 明确 batch-done 只能在全部 file-done 后；重复 batch-done 幂等。
- 明确 feature 不匹配在 v2 活跃后不得静默降级。
- 明确 FD/磁盘/源文件变化/worker panic 的 abort 与 journal 保留策略。
- 增加取消/重连在各状态下的关闭顺序和恢复路径。
- 增加状态机测试矩阵（23.6）。

### 23.6 状态机测试矩阵

| 测试 | 断言 |
| --- | --- |
| TestStateBatchReadyWhileActive | 拒绝第二个批次，队列/文件状态不变 |
| TestStateBatchReadyDuplicateID | 重复 BatchID 报协议错误，不重复建队列 |
| TestStateBatchReadyInvalidRanges | 非法 ranges/bitmap 不分配大缓冲、不打开文件 |
| TestStateBatchResetBeforeData | 接收端按新上限重建，旧 journal 丢弃 |
| TestStateBatchResetAfterData | 发送端拒绝，接收端 abort，不写错 offset |
| TestStateFileDoneBeforeReady | 协议错误，不改变文件状态 |
| TestStateFileDoneDuplicate | 相同状态幂等；冲突状态报错 |
| TestStateFileDoneHashMismatch | 清空 completed、重建 journal、只重试该文件 |
| TestStateBatchDoneEarly | 协议错误；不清理未完成文件 |
| TestStateStaleDataOldBatch | batchID 不匹配的数据被拒绝/忽略 |
| TestStateDataAfterDone | done 文件不被回退；迟到帧只 debug 忽略 |
| TestStateDuplicateContentMismatch | 清除该块 completed，触发 hash-mismatch |
| TestStateOutOfOrderChunks | 位图正确、完成计数正确 |
| TestStateOneFileRetryOthersStream | 其他文件不受影响，批次继续 |
| TestStateRetryLimit | 超过重试上限 abort，journal 保留 |
| TestStateReconnectDuringStreaming | 新批次只请求缺失块 |
| TestStateReconnectDuringVerifying | 已完成/校验中状态正确处理 |
| TestStateCancelDuringSelecting/Streaming/Verifying | 关闭顺序正确，journal 可恢复 |
| TestStateFDExhaustion | 缩小批次后重试，不崩溃 |
| TestStateFeatureMismatchMidBatch | abort/重连，不静默 v1 |

## 24. 版本与发布策略（保持 v11.5.5）

### 24.1 版本决策

按用户决定：**方案执行后保持当前版本号 v11.5.5**，不升级 major，不迁移 Go module path。

- <code>go.mod</code> 保持 <code>module github.com/schollz/croc/v11</code>；不执行 <code>/v12</code> 迁移。
- 所有 Go imports、tests、<code>web/sshwasm</code>、<code>web/testdata/oracle</code>、<code>.github/tests</code> 保持 <code>/v11</code> 不变。
- <code>src/cli/update.go</code> 的 <code>go install .../v11@latest</code>、<code>README.md</code>、packaging specs/patches 保持现状。
- v2 能力通过 feature negotiation 启用，不依赖版本字符串；保持 v11.5.5 不影响新旧端回退 v1。
- 如果 v11.5.5 已经是已发布 tag，**不要重复使用同一 tag 发布新代码**；正式发布 tag 由维护者按现有发布流程选择（例如下一个 patch/minor），但本方案的版本基线保持 v11.5.5。

### 24.2 兼容矩阵

| 组合 | 行为 |
| --- | --- |
| v11.5.5 旧端 -> 新构建 | v1 旧路径（旧端无 v2 feature） |
| 新构建 -> v11.5.5 旧端 | v1 旧路径（旧端无 v2 feature） |
| 新构建 -> 新构建 | v2 批次/数据面 |
| 新构建 <-> Web 接收端 | v1 旧路径；Web v2 后续版本 |
| 旧/新 relay | relay 协议未变，可任意顺序升级 |
| 旧/新 Tailcat | 传输配置未变；v2 只改变数据帧 |
| Go module path | 保持 <code>github.com/schollz/croc/v11</code> |

滚动升级仍然安全：v2 只在双方声明 feature 时启用；单侧升级自动回退 v1；不需要比较版本字符串，也不需要 module path 变更。

### 24.3 发布注意事项

- 不执行 module path 迁移，不产生 <code>/v12</code> import；下游 Go 消费者无需改 import。
- <code>src/version/version.go</code> 保持 v11.5.5 基线；不要手动改版本号来“制造”发布。真正发布时按仓库 Prepare Release workflow 和选定 tag 盖章。
- 若 v11.5.5 已发布：不得移动/复用该 tag；使用新的 patch/minor tag 由维护者决定。本文档不要求发布 v12。
- 发布（如需要）仍走 Prepare Release workflow：不手动打 tag；校验资产/签名/版本；发布 draft 触发 Docker。
- 由于 module path 未变，packaging、README、update.go 的安装路径无需调整；仅按正常发布流程更新版本/changelog。

### 24.4 升级与降级

**升级：**

1. 新旧客户端可独立升级；两端都是新构建时才启用 v2。
2. 若出现问题，可用 <code>--no-parallel-data</code>、<code>--no-autotune</code>、<code>--no-resume</code> 回退。
3. 旧 v11.5.5 客户端不需要为兼容 v2 做任何改动。

**降级：**

- 若从包含 v2 的新构建降级到不认识 <code>.crocstate</code> 的旧构建，先完成传输，或删除 partial 文件 + journal；否则旧版的零块启发式可能误判。
- 新 flag 在旧构建中不存在；脚本使用新 flag 时需移除。
- v2 journal 不会被旧构建读取；建议降级前清理，避免磁盘残留。

### 24.5 发布门禁

- v11.5.5 <-> 新构建双向互传：大文件、小文件、续传、Web。
- relay-only / Tailcat / local relay / proxy / IPv6。
- 1 GiB 高 RTT、10k 小文件、kill -9 恢复、ENOSPC、FD 限制、源文件变化。
- <code>go test -race</code>、fuzz、golden fixture。
- 若实际发布：确认 tag 未复用、Prepare Release 资产/签名/版本校验通过；module path 仍为 <code>/v11</code>。
- 性能矩阵达标、无 P0/P1 缺陷、文档/帮助/release notes 完成。

## 附录 A：v2 数据帧与消息示例

**数据帧（解密后）**：

~~~text
offset 0  version=2
offset 1  flags=0x01 (compressed)
offset 2  reserved=0
offset 4  batchID=7 (LE)
offset 12 fileID=3 (LE)
offset 16 offset=1048576 (LE)
offset 24 payload (compressed bytes)
~~~

**batch-ready（JSON 示意）**：

~~~json
{
  "b": 7,
  "f": [
    {"i": 3, "cs": 262144, "r": [262144, 0, 4096], "res": true},
    {"i": 4, "cs": 262144, "bm": "AAAA...", "res": true}
  ]
}
~~~

**file-done（JSON 示意）**：

~~~json
{"b":7,"i":3,"s":"ok","n":1073741824,"h":"base64..."}
~~~

## 附录 B：journal 状态图

~~~text
[missing] --WriteAt begin--> [inflight]
[inflight] --WriteAt done + chunk hash--> [completed]
[inflight] --crash before checkpoint--> [missing] on load
[completed] --checkpoint: fdatasync + atomic journal--> [durable completed]
[durable completed] --resume verify fail--> [missing]
[durable completed] --resume verify ok--> [durable completed]
~~~

## 附录 C：控制器参数建议

| 参数 | 建议值 | 说明 |
| --- | --- | --- |
| 采样周期 | 250-500 ms | 太短抖动，太长反应慢 |
| EWMA alpha | 0.2-0.3 | 吞吐/RTT |
| 最小 chunk | 64 KiB | 低延迟小文件 |
| 最大 chunk | 4 MiB | 10 Gbps 可选 8 MiB |
| 最小在途 | 1 | 每连接 |
| 最大在途 | 4 | 每连接 |
| 最大活跃 worker | min(8, available) | 中继/Tailcat 约束；实际 TCP 连接固定 |
| 最大并行文件 | min(16, max(2, CPU/2)) | 小文件 |
| 缓冲预算 | min(256 MiB, mem*10%) | 发送+接收各自 |
| 增加冷却 | 1 s | 增加后 |
| 减少冷却 | 2 s | 减少后 |
| 触发 RTT 退避 | 大于 2 倍 baseline | 网络拥塞 |
| 触发重传退避 | 增量大于阈值 | Linux TCP_INFO |
| CPU 上限 | 85% | 不再增加 |
| 内存上限 | 90% | 不再增加 |

## 附录 D：现有代码索引

| 主题 | 位置 |
| --- | --- |
| GOMAXPROCS | <code>src/cli/cli.go:44</code>、<code>src/webcli/webcli.go:31</code> |
| 发送入口 | <code>src/croc/croc.go:1759</code> |
| 接收入口 | <code>src/croc/croc.go:1989</code> |
| 控制消息循环 | <code>src/croc/croc.go:2228</code>、<code>:2926</code> |
| 数据通道激活 | <code>src/croc/croc.go:2787</code>、<code>:2801</code> |
| 单文件选择 | <code>src/croc/croc.go:3377</code>、<code>:3508</code> |
| 接收文件初始化 | <code>src/croc/croc.go:3118</code> |
| 接收数据 | <code>src/croc/croc.go:3595</code> |
| 发送数据 | <code>src/croc/croc.go:3772</code> |
| 分块队列 | <code>src/croc/chunk_queue.go:14</code>、<code>:78</code>、<code>:110</code> |
| 缺失块启发式 | <code>src/utils/utils.go:437</code> |
| 范围工具 | <code>src/utils/utils.go:512</code> |
| 文件准备 | <code>src/croc/file_preparation.go:45</code>、<code>:84</code>、<code>:117</code> |
| feature 列表 | <code>src/croc/tailcat_negotiation.go:211</code> |
| Tailcat 流数 | <code>src/croc/tailcat_stream_config_default.go:8</code> |
| 重连 | <code>src/croc/croc.go:753</code>、<code>:804</code> |
| 进度条 | <code>src/croc/terminal_display.go:57</code>、<code>src/croc/client_state.go:151</code> |
| store 并发/断点参考 | <code>src/storeclient/client.go:31-32</code>、<code>:104-118</code>、<code>:900-931</code>、<code>:1013-1070</code> |
| 中继端口 banner | <code>src/cli/cli.go:1247</code>、<code>src/tcp/tcp.go:527</code> |
| 默认端口 | <code>src/cli/cli.go:166</code> |
| Go module path | <code>go.mod:1</code>（保持 /v11） |
| 升级提示字符串 | <code>src/cli/update.go:278</code> |
| 发行版打包 | <code>packaging/distributions/*</code>（goipath/import path/patch） |
| Comm 帧 | <code>src/comm/comm.go:230-320</code> |
| AEAD | <code>src/crypt/crypt.go:55-109</code> |

---

**实施建议：** 先完成 Phase 0/1，用真实高 RTT 链路验证“每连接在途窗口 + 可变 chunk”的收益；再落地 Phase 2 的 journal，确保续传正确性；Phase 3 的多文件调度在 Phase 2 稳定后接入；Phase 4 的控制器必须以实测数据驱动，避免为了自适应而自适应。所有阶段保持 <code>--no-parallel-data</code> 可回退，并以旧/新互传和 Web 兼容作为发布门禁。
