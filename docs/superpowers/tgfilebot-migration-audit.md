# TGFileBot 定制迁移台账

## 范围与基础版本

- 共同祖先：`f29af37`；旧 fork：`d363691`；新库起点：`2c21b1e`；应用起点：`a9ab447`。
- 新库保留模块 `github.com/xAmarnath/gogram`、layer 233、版本 v1.8.0-beta.15。
- 原库未修改；不自动 commit/push。TGFileBot 原来出现的 9 个 M 标记经 blob 哈希核对，没有内容差异。
- 实现在两个 detached worktree 内完成，交付前检查原目录没有并发内容修改，再同步变更。

## 旧 fork 的 20 个差异文件

| 旧文件 | 处置与理由 |
|---|---|
| [.gitignore](../../../gogram/.gitignore) | 不搬忽略所有测试、个人 IDE 文件等本地规则。应用新增测试须精确解除忽略。 |
| [README](../../../gogram/README.md) | 不搬 Go 1.18、unknown layer、旧 layer 等退化描述。 |
| [importcontacts 示例](../../../gogram/examples/importcontacts/import_contacts.go) | 原 FloodHandler 保持兼容，示例无需改签名。 |
| [PFS 示例](../../../gogram/examples/pfs/main.go) | 仅格式变化，不搬。 |
| [signup 示例](../../../gogram/examples/signup/signup.go) | HTTP/3 依赖不进入主库；新上游已引用但未声明该依赖，使用独立示例模块保留功能。 |
| [go.mod](../../../gogram/go.mod) / [go.sum](../../../gogram/go.sum) | 不搬旧 module、Go 版本和全局 quic-go 依赖；HTTP/3 示例在自己的模块内声明并锁定依赖。 |
| [TCP 连接](../../../gogram/internal/transport/connection.go) | 保留上游 TCP 类型检查、KeepAlive 和 context。旧版 30 秒间隔并非应用契约，不搬会删除类型保护的整段补丁。 |
| [transport](../../../gogram/internal/transport/transport.go) | 不搬 mode 初始化失败时删除 Close 的泄漏性变更。 |
| [MTProto](../../../gogram/mtproto.go) | 新增可取消 FloodHandlerContext 和带 error 的 PingContext，保留原公开接口。保留上游 lifecycle 锁、建连失败清理、异步独立恢复、shutdown 提前标记；不搬同步 reader 重连和取消等待等风险补丁。Pong 路由由新上游保留。 |
| [QR 认证](../../../gogram/telegram/auth.go) | 移入256像素 PNG、跨 DC import 后2FA、未批准扫描分类、即时授权成功。上游非阻塞更新通知已覆盖旧 helper。 |
| [Bot 命令](../../../gogram/telegram/bots.go) | 不把强类型接口改回 any；应用构造默认/peer scope，显式传空 language code，保留旧菜单语义。 |
| [缓存](../../../gogram/telegram/cache.go) | 保留 owner 隔离、LRU/min peer 和合并写。旧 RebindToUser 从未被应用/库调用；旧 BindToUser(0) 会阻止实际用户绑定，且旧改动删除部分 LRU/min peer 更新，不照搬。对真实用户缓存加载、未知→已知 owner 和错 owner 拒绝写特征测试。 |
| [channels](../../../gogram/telegram/channels.go) | 不搬删除已加入频道容错/缓存的退化；IterChatMembers 的 -1 探测特性未被 TGFileBot 使用，不扩展本次范围。 |
| [client](../../../gogram/telegram/client.go) | 移入共享 DC sender、引用计数、空闲清理、并发创建合并；保留有 context 的 CreateExportedSender，授权导出合并/缓存/失效及初始化时间上限。 |
| [media](../../../gogram/telegram/media.go) | 新增 NewDownloadPool 和 ErrWorkerTCPDead；DownloadChunkContext 通过 typed DownloadOptions 接收外部池/单 RPC 超时。原范围、精确模式、CDN、限流和普通下载恢复保留；调用方拥有外部池。 |
| [messages](../../../gogram/telegram/messages.go) | 不搬去掉 context 和绕过 ExSenders 锁直接 append 的退化。 |
| [newmessage](../../../gogram/telegram/newmessage.go) | 按意图修复 Reply(nil)，不是复制仍可能传 nil 的旧修补；复制 options 后设置 ReplyID，不修改共享调用方 options。 |
| [stories](../../../gogram/telegram/stories.go) / [tgcalls](../../../gogram/telegram/tgcalls.go) | 仅格式差异，不搬。 |

## 应用切换

- [依赖](../../../TGFileBot/go.mod)：require xAmarnath 模块，replace 指向 `../gogramN`。
- [探活与登录](../../../TGFileBot/client.go)：PingContext、FloodHandlerContext、明确菜单 scope 和默认语言；QR 已授权时不再上传空 PNG。
- [流式下载](../../../TGFileBot/stream.go)：DownloadChunkContext 与 typed options，保留8秒单 RPC 超时、引用刷新、范围和错误恢复调度。
- [CI](../../../TGFileBot/.github/workflows/main.yml)：检出真实 origin `lm317379829/gogramN`，必须配置完整40位迁移提交 SHA，不再使用旧 fork 或 @latest。
- 中英文构建说明要求 patched sibling checkout，并纠正 `go run main.go` 为构建整个 package 的 `go run .`。

## 验证状态

已检查原目录待覆盖文件的规范化 Git blob 与预期 HEAD 一致，再将31个源代码/配置/测试文件同步回原目录；设计、计划和本台账另存于文档目录。没有覆盖并发用户内容修改，没有提交或推送。

原目录最终验证（Go 1.27.4 / Windows amd64）：

| 范围 | 命令/检查 | 结果 |
|---|---|---|
| 新库 | `go test -json -count=1 ./...` | 32项测试通过 |
| 新库 | `go build ./...`、`go vet ./...` | 通过 |
| TGFileBot | `go test -json -count=1 ./...` | 2项集成契约测试通过 |
| TGFileBot | `go build -o <临时输出> ./...`、`go vet ./...` | 通过 |
| 依赖 | `go list -m all` | xAmarnath v1.8.0-beta.15 → ../gogramN |
| signup 独立模块 | `go test -mod=readonly ./...` | 编译通过，无测试文件 |
| 发布目标 | linux/386、linux/amd64、linux/arm64、freebsd/amd64，CGO=0、netgo | 原目录最终源码全部编译通过 |
| 两仓库 | `git diff --check` | 通过；仅有原有 autocrlf 的换行提示 |

独立审查发现并修复两项重要问题：创建者取消不得污染活跃的共享 sender/授权等待者；FloodWait 回调取消后真实 RPC 分支须返回 context 错误。均先用测试复现失败，再验证修复；等待者相关测试重复10次通过。额外修复 QR Renew 对立即授权结果的状态同步，并防止应用测试被 Git 忽略。复审无剩余 Critical/Important。

已确认 Windows 当前无 gcc，默认 CGO_ENABLED=0；显式启用 CGO 后 race 仍因缺少 C 编译器失败，不宣称 race 通过。真实 Telegram 登录、扫码、2FA、私有频道权限与媒体下载不在离线验证范围；未启动机器人，未读取真实配置或会话密钥。

保留的旧行为/边界：PingContext 与旧 fork 一样只检查发包是否成功，不等待 Pong，返回值不是远端 RTT。RequestTimeout 限制 RPC context，但不承诺能够立即打断无 context 参数的底层阻塞 WriteMsg；它仍受连接自身写超时约束。授权 follower 测试模拟 pending 完成，不是真实跨 DC 授权集成测试。既有 CI tag 步骤早于依赖校验的问题没有扩大范围修改。

详细日志、构建产物和保留 worktree 位于 `F:\文档\临时文件\20261114-gogram-migration`，不作为待提交业务源文件交付。

## CI 启用条件

本次不自动推送。先人工提交并推送迁移后的 gogramN，再把该提交完整 SHA 配置到 TGFileBot 仓库变量 `GOGRAM_REF`。私有库可通过只读 `GOGRAM_TOKEN` secret 检出。未配置 SHA 时 CI 明确失败，避免静默使用尚未迁入补丁的上游或可变 latest。
