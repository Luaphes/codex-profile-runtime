# Retirement and lessons learned

Retired: 2026-10-27.

## Decision

Codex Profile Runtime validated a small, explicit launcher for multiple desktop
profiles on one macOS user account. Its original multi-account requirement has
ended. The project is retained for reference, with its implementation, tests,
release history, and download-loop mitigation intact. No further compatibility
or maintenance commitment is made.

The retirement target is one account, one primary application state directory,
one desktop instance, and one understood network route. Retirement of this
repository does not itself migrate a user's local data.

## What was isolated

The launcher selected a separate `CODEX_HOME` and Electron user-data directory
for each profile. This separated the checked authentication files, configuration,
conversation databases, and browser state. Separate desktop processes could
start separate app-server processes using the same installed application binary.

The launcher did not create separate operating-system users, filesystems,
network namespaces, package-manager environments, or independent installations
of every executable. The macOS home directory, accessible source trees, system
resources, and some application caches remained shared.

## Shared-runtime download incident

In the inspected desktop versions, the dependency runtime installer used a
directory under `~/.cache/codex-runtimes`, outside the profile's `CODEX_HOME` and
Electron user-data directory. Two running instances selected different runtime
bundle versions while targeting that same directory.

Successful installation records showed repeated replacement by the two versions.
The installer checked the cached version against its desired version and
downloaded a full archive when they differed. The combined evidence supports
this cycle:

1. Instance A installs version A into the shared directory.
2. Instance B observes that its desired version B is absent and installs B.
3. A subsequent periodic check in A observes B and reinstalls A.
4. The two instances continue replacing each other's selected runtime.

The failure involved shared mutable cache state and incompatible desired
versions. Concurrent desktop processes alone do not establish that a download
loop exists, and distinct profile directories do not establish that all runtime
resources are isolated.

The launcher mitigation disabled `features.workspace_dependencies` before a new
profile launch and failed closed if that preflight could not be completed. This
disabled the affected automatic dependency management; it did not partition the
shared directory or repair the application's cache architecture. See the
[implementation](../internal/launch/runtime_policy.go) and
[regression checks](integration-smoke.md#f-shared-runtime-download-protection).

## Proxy and accounting lessons

An Electron proxy argument and proxy environment variables have different
coverage. Tools spawned by the application may honor `HTTP_PROXY`, `HTTPS_PROXY`,
or `ALL_PROXY`; that behavior is tool-specific. Changing a launcher can therefore
change which downloads use a proxy even when account isolation is unchanged.

Keep client payload size, client upload/download, server ingress/egress, and
provider billing separate. A relay can receive and send the same payload, so an
ingress-plus-egress graph is not a client-download counter. Installation counts
times archive sizes are an estimate, not a per-connection billing ledger.
Short samples and model token totals cannot establish a full month's network
usage. Unattributed traffic must remain unattributed until suitable records exist.

## Retirement and rollback principles

- Inventory the active profile and the pre-existing default state separately.
- Back up both; do not merge unrelated SQLite databases or overwrite the only copy.
- Use SQLite's backup mechanism for online database snapshots. Take the final
  browser/LevelDB copy after the application has exited.
- Preserve authentication locally, and migrate live configuration and metadata
  path references deliberately. Keep historical conversation content unchanged.
- Verify database integrity, conversation identities, configuration, startup
  environment, and the new application's visible state before removing the old
  launcher from normal use.
- Retain the previous directories and launcher for rollback. A rollback should
  preserve any new state created after cutover rather than discard it.
- Publish source code and generalized lessons only. Authentication, cookies,
  conversation backups, private logs, and proxy credentials do not belong in
  this repository.

## 中文复盘

本项目完成了多账号桌面 profile 启动方案的验证。随着多账号需求结束，继续维护
额外启动层的收益已经下降，因此退役并保留历史代码、测试和故障防护。

最重要的经验是：`CODEX_HOME` 与 Electron 数据目录分开，只能证明相应应用状态
分开，不能推出工具链、系统用户、网络和所有缓存也已隔离。

本次故障发生在 profile 之外的共享依赖运行环境目录。两个实例要求不同版本，
每个实例都认为另一个版本需要更新，启动检查和周期检查使完整安装包反复下载。
不同版本的成功安装回执、共同目标目录和安装器版本判断逻辑共同支持这一机制。

已有修复在启动前关闭 `workspace_dependencies`，并在防护失败时拒绝启动。
这是一项止血措施，不是完成了共享环境隔离，也不是对未来应用版本的永久保证。

另一个容易遗漏的边界是代理：界面启动参数与子进程代理环境变量覆盖范围不同。
启动器变化可能使更多工具下载进入代理。客户端包体、服务器入出站和套餐扣量
必须分别核算，不能用短时采样或模型 token 推断全部历史流量。

退役时应先备份正在使用的 profile 和原默认目录，退出应用后做最终一致性迁移，
核对聊天、配置、路径引用及实际启动状态，再停用旧启动器。旧数据和启动器应
保留回滚副本；回滚也应保存切换后新增的数据。公开归档只保留通用技术经验，
不包含聊天、认证、Cookie、私人日志或代理凭据。
