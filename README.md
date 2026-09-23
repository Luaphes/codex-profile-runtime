# Codex Profile Runtime

A small macOS runtime manager for running multiple isolated ChatGPT/Codex Desktop profiles.

## What it does

`cpr` turns a manually validated multi-profile launch procedure into a small CLI:

- runs multiple ChatGPT Desktop instances concurrently;
- derives an isolated `CODEX_HOME` for each profile;
- derives an isolated Electron `--user-data-dir` for each profile;
- applies an optional per-profile proxy;
- discovers running profiles from the live process table;
- stops one profile using conservative identity and signal checks.

This project is a runtime glue layer. It is not a Codex replacement and it is not a ccSwitch replacement.

## Requirements

- macOS for runtime functionality;
- the official `ChatGPT.app` installed, normally at `/Applications/ChatGPT.app`;
- Go `1.23`, as declared in [`go.mod`](go.mod), for building and testing;
- cgo and the macOS `libproc` APIs for live process discovery and stop support.

Non-macOS builds keep the CLI and tests buildable, but process discovery and signaling return an explicit unsupported-platform error.

## Install

Download the binary for your Mac (`cpr_darwin_arm64` for Apple Silicon or
`cpr_darwin_amd64` for Intel) and `SHA256SUMS` from the
[latest GitHub Release](https://github.com/Luaphes/codex-profile-runtime/releases/latest).
For example, on Apple Silicon:

```bash
asset=cpr_darwin_arm64 # use cpr_darwin_amd64 on Intel
curl -fLO "https://github.com/Luaphes/codex-profile-runtime/releases/latest/download/$asset"
curl -fLO https://github.com/Luaphes/codex-profile-runtime/releases/latest/download/SHA256SUMS
grep "  $asset$" SHA256SUMS | shasum -a 256 -c -
mkdir -p "$HOME/.local/bin"
install -m 755 "$asset" "$HOME/.local/bin/cpr"
```

There is no package-manager installer yet.

Alternatively, build from source with Go 1.23 or newer:

```bash
git clone https://github.com/Luaphes/codex-profile-runtime.git
cd codex-profile-runtime
go build -o ./bin/cpr ./cmd/cpr
mkdir -p "$HOME/.local/bin"
install -m 755 ./bin/cpr "$HOME/.local/bin/cpr"
```

Make sure `$HOME/.local/bin` is on your `PATH`, or run `./bin/cpr` from the
source checkout. Releases provide separate macOS binaries for Apple Silicon and
Intel; they do not install or update `cpr` automatically.

## Quick start

Create the default configuration file with two example profiles:

```bash
config_dir="$HOME/Library/Application Support/CodexProfileRuntime"
mkdir -p "$config_dir"
cat > "$config_dir/config.json" <<'JSON'
{
  "profiles": {
    "personal": {},
    "work": {}
  }
}
JSON

cpr validate
cpr launch personal
cpr launch work
cpr list
```

Sign in separately in each new ChatGPT Desktop window. To close one profile,
run `cpr stop personal`. Keep the profile names stable: each name determines
its own `CODEX_HOME` and Electron user-data directory. Starting with a new name
creates a fresh profile.

## Configuration

The default configuration path is:

```text
~/Library/Application Support/CodexProfileRuntime/config.json
```

The smallest useful configuration is:

```json
{
  "profiles": {
    "personal": {}
  }
}
```

An optional proxy is configured on the profile itself:

```json
{
  "profiles": {
    "personal": {
      "proxy": "socks5://127.0.0.1:1080"
    },
    "work": {}
  }
}
```

Supported proxy schemes are `socks5`, `socks5h`, `http`, and `https`. Proxy credentials are not supported.

Runtime paths are derived automatically from the profile id. Do not add `codex_home`, `user_data_dir`, auth tokens, or session data to the configuration.

## Usage

```bash
cpr validate
cpr launch personal
cpr list
cpr list --json
cpr stop personal
cpr stop personal --force
```

For development or tests, any command that loads configuration accepts a simple override:

```bash
cpr validate --config ./test-config.json
cpr launch personal --config ./test-config.json
cpr list --config ./test-config.json
cpr stop personal --config ./test-config.json
```

## Runtime layout

With the default runtime root, the layout is:

```text
~/Library/Application Support/CodexProfileRuntime/
├── config.json
└── profiles/
    └── <profile>/
        ├── codex-home/
        └── chatgpt-user-data/
```

The manager creates only the selected profile's runtime directories during launch and keeps them private to the current user.

## Safety model

Profile identity is based on the target ChatGPT executable path and an exact `--user-data-dir` argument. The manager does not use PID files or process-name-only matching.

- `launch` uses one `/usr/bin/open -n` path and verifies the resulting main process;
- `list` performs a fresh process scan and reports configured exact main-process matches;
- normal `stop` sends `SIGTERM` only to the revalidated target main process;
- helper processes are used for ownership and residual checks, not for normal `SIGTERM` fan-out;
- `--force` is required before any residual or main-process `SIGKILL`;
- PID, start time, executable path, exact arguments, bundle location, and conservative ownership evidence are rechecked before force signals;
- ambiguity fails closed;
- a final fresh scan verifies that the target is gone and initially running configured profiles remain intact.

## Authentication

`cpr` does not read, copy, or migrate `auth.json`, sessions, cookies, or tokens.

When a profile is first launched, sign in normally inside that profile's ChatGPT Desktop instance. Do not copy `~/.codex/auth.json` or other state between profiles.

## Existing projects

Project directories are independent of `CODEX_HOME` and Electron user data. A user may open a personal repository from one profile and a work repository from another. `cpr` does not move or manage project files.

A new profile starts with its own local app and Codex state. Existing tasks,
conversation history, `config.toml`, skills, and memory from another profile do
not appear automatically. Opening the same repository gives access to its
files, but does not restore the old task list or conversation context. Back up
existing state before attempting any separate migration; `cpr` does not provide
a migration command.

## Proxy semantics

`profile.proxy` is the only proxy source of truth.

When it is set, `cpr` derives `--proxy-server`, `HTTP_PROXY`, `HTTPS_PROXY`, and `ALL_PROXY` from that one value. When it is empty, the launch environment removes inherited uppercase and lowercase proxy variables, including `NO_PROXY` and `no_proxy`, so the manager's shell cannot silently alter the profile's network semantics.

There is no separate `http_proxy`, `no_proxy`, or arbitrary environment configuration.

## Known limitations

- This is app-state isolation, not an OS sandbox.
- Keychain, filesystem, Git, SSH, IPC, and other shared macOS state may remain shared.
- ChatGPT or Electron updates may change launch arguments, bundle layout, or process behavior.
- Helper or Crashpad processes may survive a graceful `SIGTERM`.
- Normal stop can therefore report an incomplete shutdown.
- `--force` is intentionally conservative and may leave ambiguous processes untouched.
- `CODEX_HOME` inheritance is not used as process identity proof; use the optional manual Auth / `CODEX_HOME` isolation check in [`docs/integration-smoke.md`](docs/integration-smoke.md) when validating a real login flow.
- There is no provider switching, ccSwitch integration, GUI, daemon, workspace manager, or auth/session migration.

## Architecture

See [`docs/architecture-v0.1.md`](docs/architecture-v0.1.md) for the v0.1 design and safety boundaries.

See [`docs/integration-smoke.md`](docs/integration-smoke.md) for the manual macOS integration checklist.

## License

MIT
