# Codex Profile Runtime Manager — v0.1 Architecture Proposal

## Goals

Codex Profile Runtime Manager v0.1 的目标是把已经在 macOS 上人工验证过的多 profile 启动流程收敛成一个小型、透明的 runtime glue CLI：

- 定义 profile。
- 按 profile 启动独立的 ChatGPT/Codex Desktop runtime。
- 隔离 profile-specific 的 `CODEX_HOME`。
- 隔离 Electron `--user-data-dir`。
- 支持多个 ChatGPT Desktop 实例并发运行。
- 列出当前运行实例。
- 安全停止指定 profile。
- 为指定 profile 使用独立代理。

Manager 只负责路径、参数、环境和进程生命周期 glue；它不拥有或解释 ChatGPT/Codex 的认证协议和 session 格式。

指导原则是：把手工验证过的多 profile 启动过程变成一个小而可观察的 CLI，而不是构建通用 runtime orchestration platform。

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
- 持久化 audit subsystem 或 JSONL event logging。
- profile-specific 的高级路径覆盖；`CODEX_HOME` 和 Electron user-data 路径在 v0.1 中自动派生。

## Technical approach

### 形态

推荐做成一个单二进制 CLI：

```text
cpr validate
cpr launch <profile>
cpr list [--json]
cpr stop <profile> [--force]
```

不使用数据库、daemon 或持久化 PID 状态。`list` 的运行事实以每次执行时的实时进程扫描为准。

配置模型保持很小：使用 Go structs 加显式校验，不把 JSON Schema 作为 v0.1 要求。

### 语言选择

推荐使用 Go：

- 可生成单一 macOS CLI 二进制，不依赖 Node/Electron runtime。
- 标准库足以覆盖配置、文件、进程启动、信号和 JSON。
- 易于为配置、进程识别和停止逻辑编写单元测试。
- 可在 Darwin 适配层调用 `libproc`/`sysctl` 获取 PID、PPID、启动时间、executable path 和原始 argv。

v0.1 不需要 SwiftUI、AppKit 或原生 GUI。若未来需要签名 GUI、Menu Bar app 或更深的 macOS 集成，再评估 Swift。

### Launch

v0.1 只保留已经人工验证过的一个启动路径：使用 `open -n ... --args ...` 启动 ChatGPT Desktop。

启动参数和环境由 profile 意图派生：

- `--user-data-dir` 使用 profile id 和 runtime root 派生的目录。
- `--proxy-server` 使用 profile 的单一 `proxy` 值派生。
- `HTTP_PROXY`、`HTTPS_PROXY` 和 `ALL_PROXY` 使用同一个 `proxy` 值派生。
- `CODEX_HOME` 使用 profile id 和 runtime root 派生的目录。

启动后必须严格验证：目标进程的 executable path 属于预期的 `ChatGPT.app`，且 argv 中包含精确匹配的 `--user-data-dir`。不能因为 `open` 返回成功，就直接把 profile 标记为 running。

v0.1 不要求把“目标进程继承了 `CODEX_HOME`”作为启动成功的硬条件。`CODEX_HOME` 的隔离通过真实 ChatGPT/Codex 集成测试验证；进程识别仍以 executable path 和 exact `--user-data-dir` 为准。

环境变量只注入目标启动上下文，不修改当前 shell 或系统级全局环境。v0.1 不提供 direct-exec fallback；只有后续证据表明单一启动路径不足时，才重新评估。

### Runtime files

建议运行时目录为：

```text
~/Library/Application Support/CodexProfileRuntime/
├── config.json
└── profiles/
    └── <profile-id>/
        ├── chatgpt-user-data/
        └── codex-home/
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
│   └── runtime/
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

核心实现保持在 CLI、配置、启动、进程识别和运行时路径五个边界内，不引入 GUI、daemon、audit 或 provider abstraction。

## Profile config schema

### 最小配置

profile 主要表达意图。最小配置可以只有 profile id 和可选代理：

```json
{
  "profiles": {
    "ninibin": {
      "proxy": "socks5://127.0.0.1:18081"
    }
  }
}
```

### 全局可选设置

如果需要改变默认值，可以在配置顶层指定：

```json
{
  "runtime_root": "~/Library/Application Support/CodexProfileRuntime",
  "chatgpt_app": "/Applications/ChatGPT.app",
  "profiles": {
    "ninibin": {
      "proxy": "socks5://127.0.0.1:18081"
    },
    "personal": {}
  }
}
```

默认值：

- `runtime_root`：`~/Library/Application Support/CodexProfileRuntime`
- `chatgpt_app`：目标机器上的 ChatGPT Desktop app path

对于 profile `<id>`，Manager 自动派生：

```text
<runtime_root>/profiles/<id>/codex-home
<runtime_root>/profiles/<id>/chatgpt-user-data
```

v0.1 不允许 profile 自定义 `codex_home` 或 `user_data_dir`。这样 profile identity、路径隔离和 stop 目标之间保持一一对应；高级路径覆盖可以在确有技术需求时单独设计。

### Proxy

`proxy` 是 profile proxy 的唯一 source of truth：

- 有值时，使用该值派生 `--proxy-server`。
- 同时使用该值派生 `HTTP_PROXY`、`HTTPS_PROXY` 和 `ALL_PROXY`。
- 缺省时，不设置这些代理参数和环境变量。
- v0.1 不接受独立的 `chromium_server`、`HTTP_PROXY`、`HTTPS_PROXY` 或 `ALL_PROXY` 配置。
- v0.1 不支持把代理用户名密码写入配置。

### Validation

使用 Go structs 和显式校验：

- profile id 必须非空，并限制为可预测的安全字符集合。
- profile id 在 map 中天然唯一。
- proxy 必须是支持的 URI 格式；非法 proxy 应在 launch 前失败。
- 不允许任意环境变量注入。
- 不提供 `provider`、`account`、`auth_token`、`session` 等字段。

### Path normalization

派生目录在第一次启动前可能还不存在，因此路径规范化分两个阶段：

- 目录创建前，使用 `filepath.Clean` 加绝对路径解析做词法规范化；不能依赖要求路径已存在的 `filepath.EvalSymlinks()` 一类操作。
- 目录创建后，再对已存在路径做 symlink resolution 和 identity validation。

## Runtime/process identification

核心原则：不要使用进程名作为 profile 身份。

每个 profile 的主身份应是：

```text
ChatGPT.app executable path
+ exact --user-data-dir path derived from profile id
```

`CODEX_HOME` 仍然会在启动上下文中按 profile 派生并传入，但不作为唯一识别依据；它不一定可靠地出现在所有子进程中。

识别流程：

1. 读取 profile 配置并得到经过词法规范化的 `user_data_dir`；目录创建后再对已存在路径做 symlink resolution / identity validation。
2. 枚举当前用户的进程。
3. 找到可执行文件位于目标 `ChatGPT.app` 内、属于 ChatGPT 主进程且 argv 中精确包含 `--user-data-dir=<path>` 的进程。
4. 记录 PID、PPID、进程启动时间、executable path 和原始 argv。
5. 用 PPID 关系递归收集 helper tree。
6. 对 helper 只接受可执行文件位于同一个 ChatGPT.app bundle，或 argv 中同样带有该 profile 精确 `user_data_dir` 的进程。
7. `list` 每次重新扫描进程，不依赖 PID 文件或持久化运行状态。

如果使用 `open -n`，Manager 不应依赖 `open` 的 PID，因为它很快退出。应在启动后轮询进程表，直到找到带有精确 `user_data_dir` 的 ChatGPT 主进程；超时则启动失败。

启动验证严格要求 executable path 和 exact `--user-data-dir`；不把运行时 `CODEX_HOME` 环境继承证明作为 launch 成功条件。`CODEX_HOME` 隔离由 integration tests 覆盖。

## Safe stop strategy

采用 fail-closed 策略。

禁止：

- `killall ChatGPT`。
- 宽泛的 `pkill -f ChatGPT`。
- 只相信旧 PID 文件。
- 按显示名称、窗口标题或端口号匹配。

停止前：

1. 重新扫描目标 profile。
2. 重新验证目标 ChatGPT 主进程：PID 仍存在、进程启动时间未变化、executable path 仍属于目标 ChatGPT.app、`user_data_dir` 仍精确匹配。
3. 任何候选出现歧义或身份不匹配时，立即中止，不发送信号。
4. 正常执行 `cpr stop <profile>` 时，只向通过重新校验的 ChatGPT 主进程发送 `SIGTERM`，不直接向 helper tree 中的进程逐个发送 `SIGTERM`，让 Electron 自己优雅关闭 renderer/helper 进程。
5. helper tree 只用于发现、归属验证和优雅关闭后的残留检查。
6. 等待优雅关闭结束后重新扫描。若仍有残留，普通 stop 报告未完全停止，不扩大信号目标。
7. 只有显式执行 `cpr stop <profile> --force` 时，才允许对仍然残留的进程发送 `SIGKILL`；发送前必须再次确认每个进程属于目标 profile。
8. 最终再次扫描，确认目标 profile 已不存在，同时确认其他 profile 的 PID 仍存活。

由于 v0.1 的路径由 profile id 自动派生，不同 profile 不会共享同一个 `user_data_dir` 或 `codex_home`。如果未来支持路径覆盖，必须保留相同的唯一性校验。

## Acceptance criteria

### Configuration

- 能使用 Go structs 加显式校验加载小型 profile 配置。
- 最小 profile 配置只需 profile id 和可选的单一 `proxy` 字段。
- `codex_home` 和 `user_data_dir` 能从 profile id 和 runtime root 稳定派生。
- 非法 profile id 或 proxy 地址在 launch 前失败。
- 不读取或复制任何 auth/session/token 内容。
- 不要求或生成 JSON Schema、audit log 或持久化 PID 状态。

### Launch

- `launch A` 能启动 profile A。
- `launch B` 能同时启动 profile B。
- 两个实例使用不同的派生 `user_data_dir` 和 `CODEX_HOME`。
- 两个实例可以同时访问本地项目。
- `--proxy-server`、`HTTP_PROXY`、`HTTPS_PROXY` 和 `ALL_PROXY` 全部从同一个 profile `proxy` 值派生。
- 启动后能严格验证目标 executable path 和 exact `--user-data-dir`。
- `CODEX_HOME` 隔离通过真实 integration tests 验证，而不是作为复杂的 runtime launch proof。

### List

- `list` 能显示 profile、主 PID、启动时间、数据目录、Codex 目录和代理摘要。
- 能识别 ChatGPT 主进程及其 helper tree。
- Manager 退出后再次运行 `list`，仍能正确发现存活实例。
- 支持机器可读的 `--json` 输出。
- 不依赖过期 PID 文件或持久化运行状态。

### Stop

- `stop A` 只停止 A。
- B 和用户手动启动的其他 ChatGPT 实例保持运行。
- 普通停止只向重新校验通过的 ChatGPT 主进程发送 `SIGTERM`，不逐个终止 helper tree。
- helper tree 仅用于发现、归属验证和停止后的残留检查。
- 强制停止必须显式使用 `--force`，且只允许终止发送信号前再次校验通过的残留进程。
- 身份有歧义时宁可失败，也不发送信号。
- 停止完成后重新扫描确认目标进程已消失。

## Technical risks

### 1. ChatGPT/Electron 更新改变启动行为

参数、helper 结构、单实例锁或 bundle 路径可能变化。必须把 ChatGPT 版本、启动参数和 executable path 验证纳入 integration tests。

### 2. LaunchServices 的环境变量继承不稳定

`--proxy-server` 和环境变量的实际继承行为可能随应用版本变化。v0.1 不把 `CODEX_HOME` 的运行时继承证明作为 launch 硬条件，而是用真实集成测试验证隔离结果。

### 3. PID 复用和进程树变化导致停止风险

helper 可能被重新挂载，PID 也可能复用。需要启动时间、精确 argv、bundle 路径和发送信号前的二次校验。

### 4. profile 隔离不一定覆盖所有 macOS 状态

Keychain、全局缓存、共享 IPC、系统级应用状态可能仍然共享。v0.1 只能承诺 `CODEX_HOME` 和 Electron `user-data-dir` 层面的隔离。

### 5. 代理语义和运行状态观察范围有限

Chromium 参数、环境变量、DNS、WebSocket、QUIC 和子进程的代理行为可能不同；同时，没有 daemon 或持久化日志意味着 `list` 只反映当前时刻的进程状态。v0.1 应明确这些边界，并通过真实连通性和进程集成测试验证。

## Scope boundary

v0.1 只实现“配置解析 + 自动路径派生 + 单一启动路径 + 进程扫描 + 安全停止”的 runtime glue。它不实现 provider switching、GUI、daemon、workspace 管理、session migration、audit subsystem 或自动 auth/token 处理。
