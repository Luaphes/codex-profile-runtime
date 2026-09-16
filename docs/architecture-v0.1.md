# Codex Profile Runtime Manager — v0.1 Architecture Proposal

## Goals

Codex Profile Runtime Manager v0.1 的目标是提供一层轻量、透明、可审计的 macOS runtime/profile glue layer：

- 定义 profile。
- 按 profile 启动独立的 ChatGPT/Codex Desktop runtime。
- 隔离 profile-specific 的 `CODEX_HOME`。
- 隔离 Electron `--user-data-dir`。
- 支持多个 ChatGPT Desktop 实例并发运行。
- 列出当前运行实例。
- 安全停止指定 profile。
- 为指定 profile 注入独立代理配置。
- 记录可审计的启动、发现和停止事件。

Manager 只负责路径、参数、环境和进程生命周期 glue；它不拥有或解释 ChatGPT/Codex 的认证协议和 session 格式。

## Non-goals

v0.1 明确不包含：

- Provider switching。
- ccSwitch integration。
- GUI。
- Daemon。
- Workspace management。
- Session migration。
- 自动处理、复制或迁移 auth token。
- 重写 Codex 或替代 ChatGPT Desktop。

## Technical approach

### 形态

推荐做成一个单二进制 CLI：

```text
cpr validate
cpr launch <profile>
cpr list [--json]
cpr stop <profile> [--force]
```

不使用数据库。`list` 的运行事实以实时进程扫描为准；磁盘上的 state 只用于审计和辅助恢复，不能作为唯一运行状态来源。

建议使用 JSON 配置和 JSONL 审计日志，配套 JSON Schema 做严格校验。

### 语言选择

推荐使用 Go：

- 可生成单一 macOS CLI 二进制，不依赖 Node/Electron runtime。
- 标准库足以覆盖配置、文件、进程启动、信号和 JSON。
- 易于编写配置、进程识别和停止逻辑的单元测试。
- 可在 Darwin 适配层调用 `libproc`/`sysctl` 获取 PID、PPID、启动时间、executable path 和原始 argv。

v0.1 不需要 SwiftUI、AppKit 或原生 GUI。若未来需要签名 GUI、Menu Bar app 或更深的 macOS 集成，再评估 Swift。

### Launch

推荐使用已经人工验证过的 `open -n ... --args ...` 路径启动 ChatGPT Desktop，并传递：

- profile-specific `--user-data-dir`。
- profile-specific `--proxy-server`。
- profile-specific `CODEX_HOME` 和代理环境变量。

启动后必须做启动证据校验：确认目标进程确实使用了预期的 data directory、代理参数和可验证的环境配置。不能因为 `open` 返回成功，就直接把 profile 标记为 running。

macOS LaunchServices 对调用方环境变量的继承可能随应用版本变化。如果无法确认 `CODEX_HOME` 已传入目标 runtime，应直接报告启动失败，而不是静默宣称隔离成功。必要时可在同一个 launcher abstraction 下增加 direct-exec backend，但不应扩大 v0.1 的用户-facing scope。

环境变量只注入目标启动上下文，不修改当前 shell 或系统级全局环境。

### Runtime files

建议运行时目录为：

```text
~/Library/Application Support/CodexProfileRuntime/
├── config.json
├── profiles/
│   └── <profile-id>/
│       ├── chatgpt-user-data/
│       └── codex-home/
├── state/
└── audit/
    └── events.jsonl
```

`chatgpt-user-data/` 和 `codex-home/` 只作为路径传递给目标程序。Manager 不读取、复制或解析其中的 auth/session 内容。

## Repository structure

建议的 repository 结构如下：

```text
codex-profile-runtime/
├── README.md
├── LICENSE
├── go.mod
│
├── cmd/
│   └── cpr/
│       └── main.go
│
├── internal/
│   ├── cli/
│   ├── config/
│   ├── launch/
│   ├── process/
│   │   └── darwin/
│   ├── runtime/
│   └── audit/
│
├── schema/
│   └── profile.schema.json
│
├── examples/
│   └── config.json
│
├── docs/
│   └── architecture-v0.1.md
│
└── tests/
    ├── config/
    ├── process-snapshots/
    └── integration/
```

核心实现应保持在 CLI、配置、启动、进程识别、运行时路径和审计六个边界内，不引入 GUI、daemon 或 provider abstraction。

## Profile config schema

配置的核心字段建议如下：

```json
{
  "schema_version": 1,
  "chatgpt_app": "/Applications/ChatGPT.app",
  "runtime_root": "~/Library/Application Support/CodexProfileRuntime",
  "profiles": [
    {
      "id": "work",
      "display_name": "Work",
      "user_data_dir": "~/Library/Application Support/CodexProfileRuntime/profiles/work/chatgpt-user-data",
      "codex_home": "~/Library/Application Support/CodexProfileRuntime/profiles/work/codex-home",
      "proxy": {
        "chromium_server": "socks5://127.0.0.1:1080",
        "environment": {
          "HTTP_PROXY": "socks5://127.0.0.1:1080",
          "HTTPS_PROXY": "socks5://127.0.0.1:1080",
          "ALL_PROXY": "socks5://127.0.0.1:1080"
        },
        "no_proxy": [
          "127.0.0.1",
          "localhost"
        ]
      }
    }
  ]
}
```

约束：

- `id` 全局唯一，建议限制为小写字母、数字、`-`、`_`、`.`。
- 所有 profile 的 `user_data_dir` 必须唯一。
- 所有 profile 的 `codex_home` 必须唯一。
- 路径展开后必须 canonicalize，避免符号链接和 `../` 造成身份混淆。
- 代理只允许显式的 allowlist 字段。
- v0.1 不支持代理用户名密码，避免 secret 出现在配置和审计日志中。
- 不提供 `provider`、`account`、`auth_token`、`session` 等字段。
- `proxy` 缺省表示不设置代理。
- 不建议允许任意环境变量注入，避免 Manager 变成通用环境劫持器。

`CODEX_HOME` 是由 Manager 传入的运行时路径，不意味着 Manager 会读取、复制或迁移其中的认证状态。

## Runtime/process identification

核心原则：不要使用进程名作为 profile 身份。

每个 profile 的主身份应是：

```text
ChatGPT.app executable path
+ exact --user-data-dir path
```

`CODEX_HOME` 和代理参数可用于启动后校验，但不应作为唯一识别依据，因为它们通常位于环境变量中，不一定可靠地出现在所有子进程中。

识别流程：

1. 读取 profile 配置并得到 canonical `user_data_dir`。
2. 枚举当前用户的进程。
3. 找到可执行文件位于目标 `ChatGPT.app` 内、属于 ChatGPT 主进程且 argv 中精确包含 `--user-data-dir=<path>` 的进程。
4. 记录 PID、PPID、进程启动时间、executable path、原始 argv 和 profile identity hash。
5. 用 PPID 关系递归收集 helper tree。
6. 对 helper 只接受可执行文件位于同一个 ChatGPT.app bundle，或 argv 中同样带有该 profile 的精确 `user_data_dir` 的进程。
7. `list` 每次重新扫描进程；磁盘 state 只用于审计和辅助恢复。

如果使用 `open -n`，Manager 不应依赖 `open` 的 PID，因为它很快退出。应在启动后轮询进程表，直到找到带有精确 `user_data_dir` 的 ChatGPT 主进程；超时则启动失败。

配置校验必须阻止两个 profile 共享同一个 `user_data_dir` 或 `codex_home`。否则无法安全地区分它们。

## Safe stop strategy

采用 fail-closed 策略。

禁止：

- `killall ChatGPT`。
- 宽泛的 `pkill -f ChatGPT`。
- 只相信旧 PID 文件。
- 按显示名称、窗口标题或端口号匹配。

停止前：

1. 重新扫描目标 profile。
2. 对每个候选 PID 重新验证：PID 仍存在、进程启动时间未变化、executable path 仍属于目标 ChatGPT.app、`user_data_dir` 仍精确匹配，且 profile 配置没有路径冲突。
3. 任何候选出现歧义或身份不匹配时，立即中止，不发送信号。
4. 先发送 `SIGTERM`，等待退出并重新扫描。
5. 只有显式使用 `--force` 时，才对仍然通过身份校验的目标进程发送 `SIGKILL`。
6. 最终再次扫描，确认目标 profile 已不存在，同时确认其他 profile 的 PID 仍存活。

如果用户让两个实例共享同一个 `user_data_dir`，它们在 Manager 看来就是同一个 profile，无法安全区分；配置校验必须禁止这种情况。

## Acceptance criteria

### Configuration

- 能加载并严格校验 profile 配置。
- 重复 profile ID、重复路径、非法代理地址都会失败。
- 不读取或复制任何 auth/session/token 内容。

### Launch

- `launch A` 能启动 profile A。
- `launch B` 能同时启动 profile B。
- 两个实例使用不同的 `user_data_dir` 和 `CODEX_HOME`。
- 两个实例可以同时访问本地项目。
- 代理参数和环境变量只作用于目标实例。
- 启动失败或无法证明隔离时，不写入“运行成功”状态。

### List

- `list` 能显示 profile、主 PID、启动时间、数据目录、Codex 目录和代理摘要。
- 能识别 ChatGPT 主进程及其 helper tree。
- Manager 退出后再次运行 `list`，仍能正确发现存活实例。
- 支持机器可读的 `--json` 输出。
- 不显示或记录 proxy credentials、auth token 等敏感内容。

### Stop

- `stop A` 只停止 A。
- B 和用户手动启动的其他 ChatGPT 实例保持运行。
- 普通停止使用 graceful termination。
- 强制停止必须显式使用 `--force`。
- 身份有歧义时宁可失败，也不发送信号。
- 停止完成后重新扫描确认目标进程已消失。

### Audit

- launch/list/stop 都产生结构化审计事件。
- 审计记录包含 profile、PID、启动时间、配置 hash、结果和信号。
- 审计日志不包含 auth/session 内容和代理密码。
- 不需要 daemon 也能重建基本运行事实。

## Technical risks

### 1. ChatGPT/Electron 更新改变启动行为

参数、helper 结构、单实例锁或 bundle 路径可能变化。必须把 ChatGPT 版本和启动证据纳入集成测试。

### 2. LaunchServices 的环境变量继承不稳定

`--proxy-server` 通常可直接验证，但 `CODEX_HOME` 是否传入后续子进程不能只靠假设。必须启动后校验，必要时使用 direct-exec backend。

### 3. PID 复用和进程树变化导致停止风险

helper 可能被重新挂载，PID 也可能复用。需要启动时间、精确 argv、bundle 路径和发送信号前的二次校验。

### 4. profile 隔离不一定覆盖所有 macOS 状态

Keychain、全局缓存、共享 IPC、系统级应用状态可能仍然共享。v0.1 只能承诺 `CODEX_HOME` 和 Electron `user-data-dir` 层面的隔离。

### 5. 代理语义和流量覆盖范围不确定

Chromium 参数、环境变量、DNS、WebSocket、QUIC 和子进程的代理行为可能不同。v0.1 应明确支持范围，并通过真实连通性测试验证，而不是仅记录配置。

## Scope boundary

v0.1 只实现“配置解析 + 启动证据 + 进程扫描 + 安全停止 + 审计日志”五个边界内的 runtime glue。它不实现 provider switching、GUI、daemon、workspace 管理、session migration 或自动 auth/token 处理。
