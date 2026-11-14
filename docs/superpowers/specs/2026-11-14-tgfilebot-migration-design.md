# TGFileBot 定制能力迁移设计

用户已批准：以 gogramN 为基础，按功能迁移旧 gogram 中面向 TGFileBot 的定制，不用整文件覆盖；让 TGFileBot 全面使用新库。未经批准不提交、不推送，不改旧 gogram。

## 边界

- 保留 `github.com/xAmarnath/gogram` 模块、layer 233、v1.8.0-beta.15 和上游公开接口。
- Go 最低要求沿用新库 1.25.0；不为与 TGFileBot 无关的 signup HTTP/3 示例引入 quic-go。
- 保留 TGFileBot 既有业务行为：Bot/User 双客户端、QR/手机号/2FA、权限、搜索评论、HTTP Range、引用刷新、分片重试和缓存。
- 现有 TGFileBot 的 Git M 标记经原始 blob 哈希验证，没有实际未提交内容变更。仍禁止 reset/clean/覆盖无关修改。

## 迁移方式

1. 从共同上游 `f29af37` 对比旧库，建立迁移台账：移植、上游已覆盖、以安全实现替代、不相关不搬。拒绝把去除 context/清理/锁的历史退化照搬。
2. 保留上游 `DownloadChunkContext`，在 DownloadOptions 增加可选外部 WorkerPool 和单请求超时。补充 NewDownloadPool、ErrWorkerTCPDead，保留共享 DC 下载连接和调用方调度能力；池关闭幂等、不得关闭其他持有者的连接；请求取消不误判断连。
3. 保留上游 Ping() 与旧 FloodHandler；新增 PingContext(ctx) (time.Duration,error) 和 FloodHandlerContext 配置。TGFileBot 使用新增接口，既保留上游源码兼容，也保留原有取消语义。
4. QR：保留 PNG 双返回值，迁入固定可扫码尺寸、跨 DC 后 2FA、明确区分扫描未批准和登录成功。Reply(nil) 不 panic 且正确设置 ReplyID。Bot 命令使用显式 BotCommandScope，防止 nil 解引用。
5. 缓存与生命周期按实际使用迁移：保留 owner 隔离、min peer、LRU、合并写、上游建连/重连保护；测试证明必要的加载/重绑能力，避免未知用户缓存污染其他账号。旧连接恢复/强制关闭补丁仅在新上游确有缺口时引入，不降级现有清理。
6. TGFileBot 全部导入改为 xAmarnath，go.mod 本地 replace 指向 ../gogramN；CI 从本地 fork 的现有 origin 解析真实模块替换目标，不能再引用旧库；无法确认远端时明确说明并提供可配置方式，不猜账号。

## 验收

- 对新增接口与关键边界写离线回归测试：取消/超时、关闭池/共享连接、空范围/文件尾、FloodWait 取消、QR 结果分类、Reply nil、命令 scope、缓存隔离。
- 两项目 go test ./...、go build ./...、go vet ./...；可行时运行 race；Windows 不支持或缺 C 编译器时如实报告并用替代离线检查，不宣称 race 通过。
- 全局查找旧导入/旧目录依赖，检查 CI、说明与 go.mod 一致；审查最终 diff，独立 reviewer 检查后修正。
- 不启动真实机器人，不访问实际会话/配置密钥，不联网登录 Telegram。真实登录和媒体下载验收需另有运行条件。

## 隔离与交付

执行阶段使用隔离 worktree；同步回用户原目录前检查原目录未发生并发修改。测试临时数据写测试临时目录或用户指定临时目录。最终交付在 gogramN 和 TGFileBot 原目录可见；保留旧库。没有自动 commit/push。
为节省上下文和保证接口一致，建议主代理顺序实现，最后一次独立代码审查。
