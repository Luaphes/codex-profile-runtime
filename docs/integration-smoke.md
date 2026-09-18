# v0.1 macOS Integration Smoke Checklist

This document is a manual checklist for validating the v0.1 runtime behavior on a real macOS machine. It is intentionally not an automated destructive test and does not require authentication.

Build the binary first:

```bash
go build -o ./bin/cpr ./cmd/cpr
```

Create a disposable configuration for the smoke run. Use a fresh temporary runtime root and the reserved temporary profile name `smoke-test`; do not use any existing user profile.

```bash
smoke_root="$(mktemp -d "${TMPDIR:-/tmp}/cpr-smoke.XXXXXX")"
smoke_config="$smoke_root/config.json"
smoke_runtime="$smoke_root/runtime"

cat > "$smoke_config" <<EOF
{
  "runtime_root": "$smoke_runtime",
  "chatgpt_app": "/Applications/ChatGPT.app",
  "profiles": {
    "smoke-test": {}
  }
}
EOF
```

Do not copy auth files, session files, cookies, or existing profile directories into `smoke_root`.

## A. Read-only checks

These checks do not send signals or launch applications:

```bash
go test ./...
go vet ./...
./bin/cpr validate --config "$smoke_config"
./bin/cpr list --config "$smoke_config"
./bin/cpr list --json --config "$smoke_config"
```

Expected results:

- tests and vet pass;
- validation succeeds for one profile;
- list reports `no running profiles` before the smoke launch, unless this disposable identity was already used and is still running;
- JSON list output contains a `profiles` array and no auth, token, session, argv, or environment data.

The list command scans live processes but does not send signals.

## B. Isolated launch and list smoke

1. Confirm that the configuration contains only the disposable `smoke-test` profile and the temporary runtime root.
2. Launch it:

   ```bash
   ./bin/cpr launch smoke-test --config "$smoke_config"
   ```

3. Confirm that the launch succeeds only after an exact ChatGPT executable and exact `--user-data-dir` identity is observed.
4. Inspect the instance using both list formats:

   ```bash
   ./bin/cpr list --config "$smoke_config"
   ./bin/cpr list --json --config "$smoke_config"
   ```

5. Confirm that the reported paths are under the temporary runtime root:

   ```text
   <temporary-root>/runtime/profiles/smoke-test/codex-home
   <temporary-root>/runtime/profiles/smoke-test/chatgpt-user-data
   ```

6. Confirm that any already running ChatGPT instances remain open and are not reconfigured or migrated. Do not use broad process commands such as `killall ChatGPT`, `pkill ChatGPT`, or `pkill -f ChatGPT`.

No login is required for this smoke. If the disposable instance displays a login screen, leave existing profiles untouched.

## C. Graceful stop smoke

Only after confirming that the running target is the disposable `smoke-test` instance, run:

```bash
./bin/cpr stop smoke-test --config "$smoke_config"
```

This is the only standard smoke step that may send a signal. It should send `SIGTERM` to the revalidated target ChatGPT main process only. The manager should then rescan and confirm that the target main process disappeared.

Verify that unrelated ChatGPT instances remain running. A helper or Crashpad process may survive graceful shutdown; if that produces a conservative incomplete-stop result, record the result and do not run `--force` merely to make the smoke appear green.

## D. Force behavior

The standard integration checklist does not require a real `SIGKILL`. Force semantics are primarily covered by the unit tests and their injectable process/signal fixtures.

If a contributor separately chooses to test force behavior, they must create a new disposable profile and runtime root, verify ownership immediately before the test, and obtain explicit authorization for any real `SIGKILL`. Never use a broad process-name kill. Do not add an automatic force script to the repository.

## Optional E. Auth / `CODEX_HOME` isolation check

This is an optional, interactive check for a contributor who explicitly wants to validate real login-state placement. It is not part of the standard non-interactive B+C smoke and must use the same disposable `smoke-test` profile and temporary runtime root.

1. Before login, record only metadata for the existing `~/.codex/auth.json` if it exists (existence, size, inode, and modification time). Do not print or read its contents.
2. Complete the normal login flow in the disposable ChatGPT Desktop instance. Do not copy any auth, session, cookie, or token data.
3. Inspect only directory names and file metadata under `<temporary-root>/runtime/profiles/smoke-test/codex-home`; do not dump or parse auth/state contents. Confirm that the expected profile-specific auth/state appears under this disposable `CODEX_HOME` when the current ChatGPT/Codex build writes such state.
4. Confirm that the original `~/.codex/auth.json` remains absent or has identical metadata after the check.

This check is environment- and application-version-sensitive. A failure is evidence that needs investigation; it is not a reason to weaken process identity or safe-stop checks.

## Cleanup and reporting

After confirming that the disposable instance is closed, remove only the temporary smoke root if desired. Do not remove any default runtime root or existing user profile.

Record:

- macOS and ChatGPT app versions;
- the exact commands and outcomes;
- whether `SIGTERM` was sent;
- whether any `SIGKILL` was sent (the standard checklist expects no);
- whether a helper or Crashpad residual caused an incomplete graceful stop;
- whether unrelated ChatGPT instances remained unaffected.
