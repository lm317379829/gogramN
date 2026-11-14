# TGFileBot Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use subagent-driven-development (recommended) or executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 将旧 gogram 中 TGFileBot 需要的定制安全移入 gogramN，再将 TGFileBot 全面切换到新库。

**Architecture:** 保留新上游 API，新增类型明确的兼容能力。使用共享 DC 下载 sender、调用方持有的池和可取消 RPC，避免把旧库整文件或历史退化带入。主代理顺序实现并执行一次独立最终审查。

**Tech Stack:** Go 1.25+、标准库、原生 testing、Git；本机 Go 1.27.4。

**Spec:** `../specs/2026-11-14-tgfilebot-migration-design.md`

## Global Constraints

- 保留 github.com/xAmarnath/gogram、layer 233、v1.8.0-beta.15；不改旧 gogram。
- 不自动提交、推送、reset 或 clean；保护用户现有和并发修改。
- 保留上游 Ping()/FloodHandler/DownloadChunkContext；兼容能力采用新增 API。
- 不读取真实配置和会话密钥，不启动机器人或登录 Telegram。
- 最终修改回到用户原目录；同步前验证原目录内容未变。

## Review Focus

- HTTP 客户端取消不能触发共享连接重拨或终止别人的下载；Task 2 测取消优先级。
- 同一 DC 并发首次建池不能重复导出授权；Task 2 测单次初始化和等待者取消。
- 池重复关闭/客户端关闭/仍有持有者的池不能泄漏或误关连接；Task 2 测生命周期。
- QR 扫描但未批准、跨 DC 要求 2FA、已有授权必须区分；Task 3 测结果分类。
- 缓存 owner 变更不能把其他账号 peer 带入；Task 3 测 owner 隔离。

---

### Task 1: 建立隔离环境与定制迁移台账

**Files:** Create `docs/superpowers/tgfilebot-migration-audit.md` in gogramN. No old-library modifications.

**Interfaces:** Consumes shared ancestor f29af37 and old fork HEAD; produces per-file/custom-feature dispositions.

- [x] 使用 using-git-worktrees，检查三个仓库 HEAD/status 和 origin；为两个目标仓库建立隔离工作区，不做提交。
- [x] 对旧库的 20 个差异文件逐项归类，优先真实业务调用；注明已经由新上游覆盖和不相关改动。
- [x] 保存初始原目录 blob/HEAD 清单用于交付前冲突检测；记录 LF/CRLF 假脏状态。

### Task 2: 可取消探活、FloodWait 和共享 DC 分片下载

**Files:** Modify `mtproto.go`, `telegram/client.go`, `telegram/media.go`; Create focused download-sender implementation and `*_test.go` files in gogramN.

**Interfaces:** Produces `(*Client).PingContext(context.Context) (time.Duration,error)`; config `FloodHandlerContext func(context.Context,error) bool` in MTProto and ClientConfig; `(*Client).NewDownloadPool(int32,...context.Context) (*WorkerPool,error)`; `ErrWorkerTCPDead`; `DownloadOptions.WorkerPool *WorkerPool`, `DownloadOptions.RequestTimeout time.Duration`. Existing upstream signatures remain.

- [x] 先写失败测试：`TestPingContextCanceled`、`TestFloodHandlerContextCancellation`；断言取消返回 context.Canceled、旧回调仍可用、新回调优先。
- [x] 运行对应测试，确认新增接口缺失导致失败；实现请求 context 透传与新探活，保留上游 Ping 行为。
- [x] 先写失败测试：`TestDownloadSenderConcurrentAcquire`（同一 DC factory 仅调用一次）、`TestDownloadSenderWaitCanceled`、`TestDownloadPoolCloseIdempotent`、`TestDownloadSenderShutdown`；使用注入 fake factory 和离线 MTProto 对象，不联网。
- [x] 实现独立共享 sender 缓存、引用计数、在途创建等待与客户端关闭。任何网络 IO 不持有全局缓存锁；dead sender 替换不终止在途持有者。关闭通知唤醒等待者。
- [x] 先写失败测试：`TestChunkRequestTimeoutOverride`（8*time.Second）、`TestChunkCanceledBeforeSetup`、`TestChunkRangeBoundaries`、`TestWorkerCancellationNotTCPDead`。原有 end-exclusive、空范围和精确模式保留。
- [x] 扩展 DownloadOptions 和 DownloadChunkContext；外部池生命周期归调用方；取消先于断连分类；外部池断连立即向调用方传播，其他普通下载仍用上游重试。
- [x] 运行新测试与库全量测试，确认不需真实 Telegram。

### Task 3: 登录、消息回复、命令和缓存定制

**Files:** Modify `telegram/auth.go`, `telegram/newmessage.go`, `telegram/cache.go` and only necessary client lifecycle code; Create focused regression tests.

**Interfaces:** Preserve `QRLogin(...QrOptions)`, `ExportAsPng() ([]byte,error)`, upstream typed bot scopes; provide cache rebind helper only if the audit proves needed. Existing owner isolation remains.

- [x] 先写失败测试：QR 结果分类未批准/成功/未知；迁入跨 DC import 后 SESSION_PASSWORD_NEEDED 处理，检查 SwitchDc 错误；PNG 固定256像素。
- [x] 先写失败测试：`TestReplyNilOptions` 验证 nil/无参数/现有参数的 ReplyID 与非 panic 行为；提炼纯 options helper 以免测试联网。
- [x] 先写失败测试：初始未知 owner 后真实 owner、切换账号、重复绑定；仅实现必要加载/重绑，不移除上游写锁、LRU 或合并写。
- [x] 对旧 mtproto/transport/channels/stories 改动逐项复核：不移入降低锁/清理/context/协议安全的变更。确需修改时补对应离线失败测试。
- [x] 运行本任务测试及 go test ./...。

### Task 4: TGFileBot 切换与构建部署一致性

**Files:** TGFileBot `go.mod`, 8 Go source import paths, `client.go`, `stream.go`, `.github/workflows/main.yml`, README files; new focused `migration_test.go`; gogramN migration audit.

**Interfaces:** Consumes Task 2/3 APIs; local `require github.com/xAmarnath/gogram v1.8.0-beta.15`, `replace github.com/xAmarnath/gogram => ../gogramN`.

- [x] 先添加编译契约测试：`var _ connectClient = (*telegram.Client)(nil)` 使用 PingContext；调用 typed DownloadChunkContext options；验证 bot scope 为默认或 peer。
- [x] 确认切换前测试失败；替换旧 imports 和 go.mod，connectClient 改用 PingContext，FloodHandlerContext 保留原有可取消等待。
- [x] 下载调用改为 DownloadChunkContext(ctx,...,&DownloadOptions{WorkerPool:pool,RequestTimeout:8*time.Second})，保留精确范围和原有重试/引用刷新；pool 创建绑定流 context。
- [x] SetBotCommands 调用构造显式默认/peer scope，避免 nil pointer；PackMessages、QR 双返回值不做无谓改写。
- [x] 根据新库 origin 实际 fork 设置 CI 远程替换；使用明确版本或 commit，避免旧模块/旧fork @latest。README 说明两个仓库本地与 CI 构建。
- [x] 运行 TGFileBot tests/build/vet，搜索旧 import、../gogram 依赖和 CI 路径，更新迁移台账。

### Task 5: 全量验证、独立审查与原目录交付

**Files:** Both repositories' changed files and migration audit. No commit or push.

- [x] 使用 verification-before-completion；两个仓库依次 go test ./...、go build ./...、go vet ./...；定向 -race 可行则执行，记录不支持条件。
- [x] 使用 requesting-code-review 做独立最终 diff 审查；按 receiving-code-review 验证反馈并增加针对性测试。
- [x] 检查 git diff --check、全部变更和迁移台账，确保旧库未变且上游协议层未回退。
- [x] 比对原目录初始内容清单，确认无并发修改后同步目标修改到原目录，保留工作区交付且不自动 commit。
- [x] 原目录再跑 tests/build/vet，确认 go list -m 实际解析到 ../gogramN。写明离线通过、真实登录/下载未验证。

## Self-review

设计中的模块路径、上游能力保护、下载/探活/FloodWait/QR/命令/缓存与旧库不动分别由 Tasks 1–4 覆盖；验证与交付由 Task 5 覆盖。公开接口使用新增能力而非改变原签名。额外修复必须对应迁移台账或本次测试/审查证据，不做无关重构。用户未授权提交/推送，替代每任务 commit 为 diff 审查与验证记录。
执行时若审计证明某项旧补丁不再需要，记录理由和覆盖测试，不强行搬入。
