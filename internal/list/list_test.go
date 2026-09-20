package list

import (
	"bytes"
	"encoding/json"
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

type fakeSnapshotter struct {
	processes  []process.Info
	err        error
	calls      int
	executable string
}

func (s *fakeSnapshotter) Snapshot(executablePath string) ([]process.Info, error) {
	s.calls++
	s.executable = executablePath
	if s.err != nil {
		return nil, s.err
	}
	return s.processes, nil
}

func TestListMapsOnlyConfiguredExactIdentities(t *testing.T) {
	resolved, paths := testConfig(t, map[string]string{
		"personal": "socks5://127.0.0.1:1080",
		"work":     "",
	})
	executable := testExecutablePath(t, resolved.ChatGPTApp)
	start := time.Date(2026, 9, 18, 22, 40, 0, 0, time.FixedZone("CST", 8*60*60))
	snapshotter := &fakeSnapshotter{processes: []process.Info{
		{PID: 101, PPID: 1, StartTime: start, ExecutablePath: executable, Args: []string{"ChatGPT", "--user-data-dir=" + paths["personal"].UserDataDir}},
		{PID: 202, PPID: 1, StartTime: start.Add(-time.Hour), ExecutablePath: executable, Args: []string{"ChatGPT", "--user-data-dir=" + paths["work"].UserDataDir}},
		{PID: 303, PPID: 1, StartTime: start, ExecutablePath: executable, Args: []string{"ChatGPT", "--user-data-dir=/tmp/unmanaged"}},
		{PID: 404, PPID: 1, StartTime: start, ExecutablePath: executable, Args: []string{"ChatGPT", "--user-data-dir=" + paths["personal"].UserDataDir + "-suffix"}},
	}}

	instances, err := List(resolved, snapshotter)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if snapshotter.calls != 1 {
		t.Fatalf("Snapshot() calls = %d, want 1", snapshotter.calls)
	}
	if len(instances) != 2 {
		t.Fatalf("instances = %#v, want two configured matches", instances)
	}
	if instances[0].Profile != "personal" || instances[0].PID != 101 {
		t.Fatalf("first instance = %#v, want personal/101", instances[0])
	}
	if instances[1].Profile != "work" || instances[1].PID != 202 {
		t.Fatalf("second instance = %#v, want work/202", instances[1])
	}
	if instances[0].StartTime != "2026-09-18T22:40:00+08:00" {
		t.Fatalf("start_time = %q, want RFC3339 value", instances[0].StartTime)
	}
	if instances[1].Proxy != "" {
		t.Fatalf("work proxy = %q, want empty", instances[1].Proxy)
	}
}

func TestListOmitsStoppedAndUnmanagedProfiles(t *testing.T) {
	resolved, paths := testConfig(t, map[string]string{"personal": ""})
	executable := testExecutablePath(t, resolved.ChatGPTApp)
	snapshotter := &fakeSnapshotter{processes: []process.Info{{
		PID:            303,
		ExecutablePath: executable,
		Args:           []string{"ChatGPT", "--user-data-dir=/tmp/unmanaged"},
	}}}
	_ = paths

	instances, err := List(resolved, snapshotter)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(instances) != 0 {
		t.Fatalf("instances = %#v, want no configured matches", instances)
	}
}

func TestListRejectsAmbiguousProfileIdentity(t *testing.T) {
	resolved, paths := testConfig(t, map[string]string{"personal": ""})
	executable := testExecutablePath(t, resolved.ChatGPTApp)
	matchingArgs := []string{"ChatGPT", "--user-data-dir=" + paths["personal"].UserDataDir}
	snapshotter := &fakeSnapshotter{processes: []process.Info{
		{PID: 101, StartTime: time.Now(), ExecutablePath: executable, Args: matchingArgs},
		{PID: 102, StartTime: time.Now(), ExecutablePath: executable, Args: matchingArgs},
	}}

	_, err := List(resolved, snapshotter)
	if err == nil || !strings.Contains(err.Error(), `profile "personal" matched multiple ChatGPT main processes`) {
		t.Fatalf("List() error = %v, want ambiguity error", err)
	}
}

func TestListPropagatesSnapshotError(t *testing.T) {
	resolved, _ := testConfig(t, map[string]string{"personal": ""})
	wantErr := errors.New("snapshot unavailable")
	_, err := List(resolved, &fakeSnapshotter{err: wantErr})
	if !errors.Is(err, wantErr) {
		t.Fatalf("List() error = %v, want snapshot error", err)
	}
}

func TestWriteJSONHasStableFieldsAndRFC3339StartTime(t *testing.T) {
	instances := []Instance{{
		Profile:     "personal",
		PID:         101,
		StartTime:   "2026-09-18T22:40:00+08:00",
		UserDataDir: "/tmp/runtime/profiles/personal/chatgpt-user-data",
		CodexHome:   "/tmp/runtime/profiles/personal/codex-home",
		Proxy:       "socks5://127.0.0.1:1080",
	}}
	var output bytes.Buffer
	if err := WriteJSON(&output, instances); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}

	var decoded struct {
		Profiles []map[string]any `json:"profiles"`
	}
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; output = %s", err, output.String())
	}
	if len(decoded.Profiles) != 1 {
		t.Fatalf("profiles = %#v, want one profile", decoded.Profiles)
	}
	for _, field := range []string{"profile", "pid", "start_time", "user_data_dir", "codex_home", "proxy"} {
		if _, ok := decoded.Profiles[0][field]; !ok {
			t.Fatalf("JSON missing field %q: %s", field, output.String())
		}
	}
	for _, forbidden := range []string{"argv", "env", "auth", "token", "session"} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatalf("JSON unexpectedly contains %q: %s", forbidden, output.String())
		}
	}
}

func TestWriteHumanShowsDashForMissingProxy(t *testing.T) {
	var output bytes.Buffer
	instances := []Instance{{Profile: "work", PID: 202, StartTime: "2026-09-18T21:10:00+08:00"}}
	if err := WriteHuman(&output, instances); err != nil {
		t.Fatalf("WriteHuman() error = %v", err)
	}
	if !strings.Contains(output.String(), "work\t202\t2026-09-18T21:10:00+08:00\t-") {
		t.Fatalf("human output = %q, want missing proxy marker", output.String())
	}
}

func testConfig(t *testing.T, proxies map[string]string) (config.ResolvedConfig, map[string]runtime.ProfilePaths) {
	t.Helper()
	root := t.TempDir()
	appPath := filepath.Join(t.TempDir(), "ChatGPT.app")
	executable := filepath.Join(appPath, "Contents", "MacOS", "ChatGPT")
	if err := os.MkdirAll(filepath.Dir(executable), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(executable, []byte("test executable"), 0o700); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	profiles := make(map[string]config.ResolvedProfile, len(proxies))
	paths := make(map[string]runtime.ProfilePaths, len(proxies))
	for id, proxy := range proxies {
		derived, err := runtime.DeriveProfilePaths(root, id)
		if err != nil {
			t.Fatalf("DeriveProfilePaths() error = %v", err)
		}
		if err := os.MkdirAll(derived.CodexHome, 0o700); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.MkdirAll(derived.UserDataDir, 0o700); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		profiles[id] = config.ResolvedProfile{ID: id, Proxy: proxy, CodexHome: derived.CodexHome, UserDataDir: derived.UserDataDir}
		resolvedPaths, err := runtime.ResolveProfilePaths(root, id, derived)
		if err != nil {
			t.Fatalf("ResolveProfilePaths() error = %v", err)
		}
		paths[id] = resolvedPaths
	}
	return config.ResolvedConfig{RuntimeRoot: root, ChatGPTApp: appPath, Profiles: profiles}, paths
}

func testExecutablePath(t *testing.T, appPath string) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(filepath.Join(appPath, "Contents", "MacOS", "ChatGPT"))
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
	}
	return path
}
