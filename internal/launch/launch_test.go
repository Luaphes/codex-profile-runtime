package launch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Luaphes/codex-profile-runtime/internal/config"
	"github.com/Luaphes/codex-profile-runtime/internal/process"
	"github.com/Luaphes/codex-profile-runtime/internal/runtime"
)

type fakeRunner struct {
	calls []CommandSpec
	err   error
}

func (r *fakeRunner) Run(spec CommandSpec) error {
	r.calls = append(r.calls, spec)
	return r.err
}

type fakeScanner struct {
	info       process.Info
	foundAfter int
	calls      int
	err        error
}

func (s *fakeScanner) FindMain(executablePath, userDataDir string) (process.Info, bool, error) {
	s.calls++
	if s.err != nil {
		return process.Info{}, false, s.err
	}
	if s.calls < s.foundAfter {
		return process.Info{}, false, nil
	}
	return s.info, process.MatchesMainProcess(s.info, executablePath, userDataDir), nil
}

func TestBuildOpenCommandWithProxy(t *testing.T) {
	profile := config.ResolvedProfile{
		ID:          "personal",
		Proxy:       "socks5://127.0.0.1:1080",
		CodexHome:   "/tmp/runtime/profiles/personal/codex-home",
		UserDataDir: "/tmp/runtime/profiles/personal/chatgpt-user-data",
	}

	spec, err := BuildOpenCommand("/Applications/ChatGPT.app", profile)
	if err != nil {
		t.Fatalf("BuildOpenCommand() error = %v", err)
	}

	wantArgs := []string{
		"-n",
		"-a",
		"/Applications/ChatGPT.app",
		"--args",
		"--user-data-dir=/tmp/runtime/profiles/personal/chatgpt-user-data",
		"--proxy-server=socks5://127.0.0.1:1080",
	}
	if got, want := spec.Path, "/usr/bin/open"; got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
	if !equalStrings(spec.Args, wantArgs) {
		t.Fatalf("Args = %#v, want %#v", spec.Args, wantArgs)
	}

	env := envMap(spec.Env)
	if got, want := env["CODEX_HOME"], profile.CodexHome; got != want {
		t.Fatalf("CODEX_HOME = %q, want %q", got, want)
	}
	if got, want := env["CODEX_ELECTRON_USER_DATA_PATH"], profile.UserDataDir; got != want {
		t.Fatalf("CODEX_ELECTRON_USER_DATA_PATH = %q, want %q", got, want)
	}
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"} {
		if got, want := env[key], profile.Proxy; got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestBuildOpenCommandWithoutProxyOmitsProxySettings(t *testing.T) {
	profile := config.ResolvedProfile{
		ID:          "work",
		CodexHome:   "/tmp/runtime/profiles/work/codex-home",
		UserDataDir: "/tmp/runtime/profiles/work/chatgpt-user-data",
	}

	spec, err := BuildOpenCommand("/Applications/ChatGPT.app", profile)
	if err != nil {
		t.Fatalf("BuildOpenCommand() error = %v", err)
	}
	for _, arg := range spec.Args {
		if strings.HasPrefix(arg, "--proxy-server=") {
			t.Fatalf("Args unexpectedly contained proxy argument: %#v", spec.Args)
		}
	}
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"} {
		if _, ok := envMap(spec.Env)[key]; ok {
			t.Fatalf("Env unexpectedly contained %s: %#v", key, spec.Env)
		}
	}
}

func TestMergeEnvironmentOverridesInheritedValues(t *testing.T) {
	got := mergeEnvironment(
		[]string{"PATH=/bin", "CODEX_HOME=/old", "HTTP_PROXY=http://old"},
		[]string{"CODEX_HOME=/new", "HTTP_PROXY=socks5://new"},
	)

	if got := envMap(got)["CODEX_HOME"]; got != "/new" {
		t.Fatalf("CODEX_HOME = %q, want /new", got)
	}
	if got := envMap(got)["HTTP_PROXY"]; got != "socks5://new" {
		t.Fatalf("HTTP_PROXY = %q, want socks5://new", got)
	}
	if count := countEnvKey(got, "CODEX_HOME"); count != 1 {
		t.Fatalf("CODEX_HOME entries = %d, want 1", count)
	}
}

func TestMergeEnvironmentScrubsInheritedProxyVariables(t *testing.T) {
	base := []string{
		"PATH=/bin",
		"HTTP_PROXY=http://inherited",
		"HTTPS_PROXY=https://inherited",
		"ALL_PROXY=socks5://inherited",
		"http_proxy=http://inherited",
		"https_proxy=https://inherited",
		"all_proxy=socks5://inherited",
		"NO_PROXY=localhost",
		"no_proxy=localhost",
	}

	got := envMap(mergeEnvironment(base, []string{"CODEX_HOME=/runtime"}))
	for _, key := range []string{
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"ALL_PROXY",
		"http_proxy",
		"https_proxy",
		"all_proxy",
		"NO_PROXY",
		"no_proxy",
	} {
		if _, ok := got[key]; ok {
			t.Fatalf("environment unexpectedly retained %s: %#v", key, got)
		}
	}
}

func TestMergeEnvironmentDerivesProxyOnlyFromProfileValue(t *testing.T) {
	profileProxy := "socks5://profile"
	got := envMap(mergeEnvironment(
		[]string{
			"HTTP_PROXY=http://inherited",
			"HTTPS_PROXY=https://inherited",
			"ALL_PROXY=socks5://inherited",
			"http_proxy=http://conflicting",
			"https_proxy=https://conflicting",
			"all_proxy=socks5://conflicting",
			"NO_PROXY=localhost",
			"no_proxy=localhost",
		},
		[]string{
			"HTTP_PROXY=" + profileProxy,
			"HTTPS_PROXY=" + profileProxy,
			"ALL_PROXY=" + profileProxy,
		},
	))

	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"} {
		if got[key] != profileProxy {
			t.Fatalf("%s = %q, want %q", key, got[key], profileProxy)
		}
	}
	for _, key := range []string{"http_proxy", "https_proxy", "all_proxy", "NO_PROXY", "no_proxy"} {
		if _, ok := got[key]; ok {
			t.Fatalf("environment unexpectedly retained %s: %#v", key, got)
		}
	}
}

func TestMergeEnvironmentDoesNotModifyProcessEnvironment(t *testing.T) {
	before := envMap(os.Environ())
	_ = mergeEnvironment(os.Environ(), []string{
		"CODEX_HOME=/runtime",
		"HTTP_PROXY=socks5://profile",
	})
	after := envMap(os.Environ())
	if !equalEnvMaps(before, after) {
		t.Fatalf("mergeEnvironment() modified process environment")
	}
}

func TestServiceRejectsMissingApp(t *testing.T) {
	root := t.TempDir()
	profile := resolvedProfile(t, root, "personal")
	runner := &fakeRunner{}
	service := NewService(runner, &fakeScanner{})

	_, err := service.Launch(filepath.Join(root, "Missing.app"), root, profile)
	if err == nil || !strings.Contains(err.Error(), "ChatGPT app not found") {
		t.Fatalf("Launch() error = %v, want missing app error", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}
}

func TestServiceRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	profile := resolvedProfile(t, root, "personal")
	if err := os.MkdirAll(filepath.Dir(profile.CodexHome), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, profile.CodexHome); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}
	appPath := testApp(t)
	runner := &fakeRunner{}
	service := NewService(runner, &fakeScanner{})

	_, err := service.Launch(appPath, root, profile)
	if err == nil || !strings.Contains(err.Error(), "escapes runtime root") {
		t.Fatalf("Launch() error = %v, want symlink escape error", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}
}

func TestServiceRejectsAlreadyRunningProfile(t *testing.T) {
	root := t.TempDir()
	profile := resolvedProfile(t, root, "personal")
	canonicalPaths := preparedPaths(t, root, profile)
	appPath := testApp(t)
	executable, err := ValidateApp(appPath)
	if err != nil {
		t.Fatalf("ValidateApp() error = %v", err)
	}
	runner := &fakeRunner{}
	scanner := &fakeScanner{
		info: process.Info{
			PID:            12345,
			ExecutablePath: executable,
			Args:           []string{"ChatGPT", "--user-data-dir=" + canonicalPaths.UserDataDir},
		},
		foundAfter: 1,
	}
	service := NewService(runner, scanner)

	info, err := service.Launch(appPath, root, profile)
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("Launch() error = %v, want ErrAlreadyRunning", err)
	}
	if info.PID != 12345 {
		t.Fatalf("PID = %d, want 12345", info.PID)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}
}

func TestServiceWaitsForVerifiedProcess(t *testing.T) {
	root := t.TempDir()
	profile := resolvedProfile(t, root, "personal")
	canonicalPaths := preparedPaths(t, root, profile)
	appPath := testApp(t)
	executable, err := ValidateApp(appPath)
	if err != nil {
		t.Fatalf("ValidateApp() error = %v", err)
	}
	runner := &fakeRunner{}
	scanner := &fakeScanner{
		info: process.Info{
			PID:            54321,
			ExecutablePath: executable,
			Args:           []string{"ChatGPT", "--user-data-dir=" + canonicalPaths.UserDataDir},
		},
		foundAfter: 2,
	}
	service := NewService(runner, scanner)
	service.Sleep = func(_ time.Duration) {}
	service.Timeout = time.Second

	info, err := service.Launch(appPath, root, profile)
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if info.PID != 54321 {
		t.Fatalf("PID = %d, want 54321", info.PID)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(runner.calls))
	}
}

func TestServiceTimesOutWhenVerificationDoesNotFindProcess(t *testing.T) {
	root := t.TempDir()
	profile := resolvedProfile(t, root, "personal")
	appPath := testApp(t)
	executable, err := ValidateApp(appPath)
	if err != nil {
		t.Fatalf("ValidateApp() error = %v", err)
	}
	runner := &fakeRunner{}
	scanner := &fakeScanner{
		info: process.Info{
			PID:            54321,
			ExecutablePath: executable,
			Args:           []string{"ChatGPT", "--user-data-dir=" + filepath.Join(root, "other-profile")},
		},
		foundAfter: 1,
	}
	service := NewService(runner, scanner)
	service.Sleep = func(_ time.Duration) {}
	service.Timeout = 5 * time.Millisecond

	_, err = service.Launch(appPath, root, profile)
	if !errors.Is(err, ErrVerificationTimeout) {
		t.Fatalf("Launch() error = %v, want ErrVerificationTimeout", err)
	}
	if scanner.calls < 2 {
		t.Fatalf("scanner calls = %d, want repeated verification", scanner.calls)
	}
}

func preparedPaths(t *testing.T, root string, profile config.ResolvedProfile) runtime.ProfilePaths {
	t.Helper()
	paths, err := runtime.PrepareProfilePaths(root, profile.ID, runtime.ProfilePaths{
		CodexHome:   profile.CodexHome,
		UserDataDir: profile.UserDataDir,
	})
	if err != nil {
		t.Fatalf("PrepareProfilePaths() error = %v", err)
	}
	return paths
}

func resolvedProfile(t *testing.T, root, id string) config.ResolvedProfile {
	t.Helper()
	paths, err := runtime.DeriveProfilePaths(root, id)
	if err != nil {
		t.Fatalf("DeriveProfilePaths() error = %v", err)
	}
	return config.ResolvedProfile{
		ID:          id,
		CodexHome:   paths.CodexHome,
		UserDataDir: paths.UserDataDir,
	}
}

func testApp(t *testing.T) string {
	t.Helper()
	appPath := filepath.Join(t.TempDir(), "ChatGPT.app")
	executable := filepath.Join(appPath, "Contents", "MacOS", "ChatGPT")
	if err := os.MkdirAll(filepath.Dir(executable), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(executable, []byte("test executable"), 0o700); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return appPath
}

func envMap(values []string) map[string]string {
	result := make(map[string]string, len(values))
	for _, value := range values {
		key, raw, ok := strings.Cut(value, "=")
		if ok {
			result[key] = raw
		}
	}
	return result
}

func countEnvKey(values []string, key string) int {
	count := 0
	for _, value := range values {
		entryKey, _, ok := strings.Cut(value, "=")
		if ok && entryKey == key {
			count++
		}
	}
	return count
}

func equalEnvMaps(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
