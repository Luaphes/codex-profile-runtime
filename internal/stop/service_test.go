package stop

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

type fakeInspector struct {
	snapshots       [][]process.Info
	snapshotIndex   int
	revalidate      map[int][]fakeRevalidation
	revalidateCalls []int
}

type fakeRevalidation struct {
	info process.Info
	ok   bool
	err  error
}

func (f *fakeInspector) Snapshot(executablePath string) ([]process.Info, error) {
	snapshot, err := f.SnapshotAll()
	if err != nil {
		return nil, err
	}
	filtered := make([]process.Info, 0, len(snapshot))
	for _, info := range snapshot {
		if process.MatchesMainProcess(info, executablePath, userDataFromArgs(info.Args)) {
			filtered = append(filtered, info)
		}
	}
	return filtered, nil
}

func (f *fakeInspector) SnapshotAll() ([]process.Info, error) {
	if len(f.snapshots) == 0 {
		return nil, nil
	}
	index := f.snapshotIndex
	if index >= len(f.snapshots) {
		index = len(f.snapshots) - 1
	}
	f.snapshotIndex++
	return append([]process.Info(nil), f.snapshots[index]...), nil
}

func (f *fakeInspector) Revalidate(expected process.Info) (process.Info, bool, error) {
	f.revalidateCalls = append(f.revalidateCalls, expected.PID)
	queued := f.revalidate[expected.PID]
	if len(queued) == 0 {
		return expected, true, nil
	}
	result := queued[0]
	f.revalidate[expected.PID] = queued[1:]
	return result.info, result.ok, result.err
}

type fakeSignaler struct {
	calls    []signalCall
	err      error
	onSignal func(signalCall)
}

type signalCall struct {
	pid    int
	signal Signal
}

func (f *fakeSignaler) Signal(pid int, signal Signal) error {
	call := signalCall{pid: pid, signal: signal}
	f.calls = append(f.calls, call)
	if f.onSignal != nil {
		f.onSignal(call)
	}
	return f.err
}

type stopFixture struct {
	resolved   config.ResolvedConfig
	paths      map[string]runtime.ProfilePaths
	executable string
	appPath    string
}

func TestStopSendsSIGTERMOnlyToTargetAndProtectsOtherProfile(t *testing.T) {
	fixture := newStopFixture(t, "ninibin", "lucas")
	start := time.Unix(100, 0)
	target := fixture.mainInfo(101, start, "ninibin")
	other := fixture.mainInfo(202, start.Add(time.Minute), "lucas")
	inspector := &fakeInspector{snapshots: [][]process.Info{
		{target, other},
		{other},
	}}
	signaler := &fakeSignaler{}

	if err := newTestService(inspector, signaler).Stop(fixture.resolved, fixture.resolved.Profiles["ninibin"], false); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if want := []signalCall{{pid: 101, signal: SignalTERM}}; !equalSignalCalls(signaler.calls, want) {
		t.Fatalf("signals = %#v, want %#v", signaler.calls, want)
	}
}

func TestStopRejectsProfileThatIsNotRunning(t *testing.T) {
	fixture := newStopFixture(t, "ninibin")
	signaler := &fakeSignaler{}
	err := newTestService(&fakeInspector{snapshots: [][]process.Info{{}}}, signaler).Stop(fixture.resolved, fixture.resolved.Profiles["ninibin"], false)
	if !errors.Is(err, ErrNotRunning) || !strings.Contains(err.Error(), `profile "ninibin" is not running`) {
		t.Fatalf("Stop() error = %v, want not-running error", err)
	}
	if len(signaler.calls) != 0 {
		t.Fatalf("signals = %#v, want none", signaler.calls)
	}
}

func TestStopRejectsSubstringUserDataMatch(t *testing.T) {
	fixture := newStopFixture(t, "ninibin")
	start := time.Unix(100, 0)
	candidate := fixture.mainInfo(101, start, "ninibin")
	candidate.Args = []string{"ChatGPT", "--user-data-dir=" + fixture.paths["ninibin"].UserDataDir + "-suffix"}
	signaler := &fakeSignaler{}
	err := newTestService(&fakeInspector{snapshots: [][]process.Info{{candidate}}}, signaler).Stop(fixture.resolved, fixture.resolved.Profiles["ninibin"], false)
	if !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Stop() error = %v, want not-running error", err)
	}
	if len(signaler.calls) != 0 {
		t.Fatalf("signals = %#v, want none", signaler.calls)
	}
}

func TestStopFailsIfTargetReappearsOnFinalRescan(t *testing.T) {
	fixture := newStopFixture(t, "ninibin")
	target := fixture.mainInfo(101, time.Unix(100, 0), "ninibin")
	inspector := &fakeInspector{snapshots: [][]process.Info{{target}, {}, {target}}}
	signaler := &fakeSignaler{}
	err := newTestService(inspector, signaler).Stop(fixture.resolved, fixture.resolved.Profiles["ninibin"], false)
	if !errors.Is(err, ErrIdentity) {
		t.Fatalf("Stop() error = %v, want final-rescan identity error", err)
	}
	if want := []signalCall{{pid: 101, signal: SignalTERM}}; !equalSignalCalls(signaler.calls, want) {
		t.Fatalf("signals = %#v, want %#v", signaler.calls, want)
	}
}

func TestStopFailsClosedOnAmbiguousMainIdentity(t *testing.T) {
	fixture := newStopFixture(t, "ninibin")
	first := fixture.mainInfo(101, time.Unix(100, 0), "ninibin")
	second := fixture.mainInfo(102, time.Unix(200, 0), "ninibin")
	signaler := &fakeSignaler{}
	err := newTestService(&fakeInspector{snapshots: [][]process.Info{{first, second}}}, signaler).Stop(fixture.resolved, fixture.resolved.Profiles["ninibin"], false)
	if !errors.Is(err, ErrAmbiguous) || !strings.Contains(err.Error(), "matched multiple") {
		t.Fatalf("Stop() error = %v, want ambiguity error", err)
	}
	if len(signaler.calls) != 0 {
		t.Fatalf("signals = %#v, want none", signaler.calls)
	}
}

func TestStopRevalidatesStartTimeBeforeSIGTERM(t *testing.T) {
	fixture := newStopFixture(t, "ninibin")
	initial := fixture.mainInfo(101, time.Unix(100, 0), "ninibin")
	changed := initial
	changed.StartTime = time.Unix(200, 0)
	inspector := &fakeInspector{
		snapshots:  [][]process.Info{{initial}},
		revalidate: map[int][]fakeRevalidation{101: {{info: changed, ok: true}}},
	}
	signaler := &fakeSignaler{}
	err := newTestService(inspector, signaler).Stop(fixture.resolved, fixture.resolved.Profiles["ninibin"], false)
	if !errors.Is(err, ErrIdentity) {
		t.Fatalf("Stop() error = %v, want identity error", err)
	}
	if len(signaler.calls) != 0 {
		t.Fatalf("signals = %#v, want none", signaler.calls)
	}
}

func TestStopRevalidatesExecutableBeforeSIGTERM(t *testing.T) {
	fixture := newStopFixture(t, "ninibin")
	initial := fixture.mainInfo(101, time.Unix(100, 0), "ninibin")
	changed := initial
	changed.ExecutablePath = filepath.Join(t.TempDir(), "Other.app", "Contents", "MacOS", "ChatGPT")
	inspector := &fakeInspector{
		snapshots:  [][]process.Info{{initial}},
		revalidate: map[int][]fakeRevalidation{101: {{info: changed, ok: true}}},
	}
	signaler := &fakeSignaler{}
	err := newTestService(inspector, signaler).Stop(fixture.resolved, fixture.resolved.Profiles["ninibin"], false)
	if !errors.Is(err, ErrIdentity) {
		t.Fatalf("Stop() error = %v, want identity error", err)
	}
	if len(signaler.calls) != 0 {
		t.Fatalf("signals = %#v, want none", signaler.calls)
	}
}

func TestStopRevalidatesExactUserDataDirBeforeSIGTERM(t *testing.T) {
	fixture := newStopFixture(t, "ninibin")
	initial := fixture.mainInfo(101, time.Unix(100, 0), "ninibin")
	changed := initial
	changed.Args = []string{"ChatGPT", "--user-data-dir=/tmp/other-profile"}
	inspector := &fakeInspector{
		snapshots:  [][]process.Info{{initial}},
		revalidate: map[int][]fakeRevalidation{101: {{info: changed, ok: true}}},
	}
	signaler := &fakeSignaler{}
	err := newTestService(inspector, signaler).Stop(fixture.resolved, fixture.resolved.Profiles["ninibin"], false)
	if !errors.Is(err, ErrIdentity) {
		t.Fatalf("Stop() error = %v, want identity error", err)
	}
	if len(signaler.calls) != 0 {
		t.Fatalf("signals = %#v, want none", signaler.calls)
	}
}

func TestStopTimeoutNeverEscalatesWithoutForce(t *testing.T) {
	fixture := newStopFixture(t, "ninibin")
	target := fixture.mainInfo(101, time.Unix(100, 0), "ninibin")
	inspector := &fakeInspector{snapshots: [][]process.Info{{target}, {target}}}
	signaler := &fakeSignaler{}
	service := newTestService(inspector, signaler)
	service.Timeout = time.Millisecond
	if err := service.Stop(fixture.resolved, fixture.resolved.Profiles["ninibin"], false); !errors.Is(err, ErrStopTimeout) {
		t.Fatalf("Stop() error = %v, want timeout error", err)
	}
	if want := []signalCall{{pid: 101, signal: SignalTERM}}; !equalSignalCalls(signaler.calls, want) {
		t.Fatalf("signals = %#v, want %#v", signaler.calls, want)
	}
}

func TestForceKillsMainOnlyAfterSIGTERMTimeout(t *testing.T) {
	fixture := newStopFixture(t, "ninibin")
	target := fixture.mainInfo(101, time.Unix(100, 0), "ninibin")
	inspector := &fakeInspector{snapshots: [][]process.Info{{target}, {target}}}
	signaler := &fakeSignaler{}
	signaler.onSignal = func(call signalCall) {
		if call.signal == SignalKILL {
			inspector.snapshots = append(inspector.snapshots, []process.Info{})
			inspector.snapshotIndex = 2
		}
	}
	service := newTestService(inspector, signaler)
	service.Timeout = time.Millisecond
	if err := service.Stop(fixture.resolved, fixture.resolved.Profiles["ninibin"], true); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	want := []signalCall{{pid: 101, signal: SignalTERM}, {pid: 101, signal: SignalKILL}}
	if !equalSignalCalls(signaler.calls, want) {
		t.Fatalf("signals = %#v, want %#v", signaler.calls, want)
	}
}

func TestForceDoesNotSIGKILLWhenSIGTERMSucceeds(t *testing.T) {
	fixture := newStopFixture(t, "ninibin")
	target := fixture.mainInfo(101, time.Unix(100, 0), "ninibin")
	signaler := &fakeSignaler{}
	if err := newTestService(&fakeInspector{snapshots: [][]process.Info{{target}, {}}}, signaler).Stop(fixture.resolved, fixture.resolved.Profiles["ninibin"], true); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if want := []signalCall{{pid: 101, signal: SignalTERM}}; !equalSignalCalls(signaler.calls, want) {
		t.Fatalf("signals = %#v, want %#v", signaler.calls, want)
	}
}

func TestForceKillsExactUserDataHelperAfterRevalidation(t *testing.T) {
	fixture := newStopFixture(t, "ninibin")
	target := fixture.mainInfo(101, time.Unix(100, 0), "ninibin")
	helper := fixture.helperInfo(303, 101, time.Unix(110, 0), "--user-data-dir="+fixture.paths["ninibin"].UserDataDir)
	inspector := &fakeInspector{snapshots: [][]process.Info{{target, helper}, {helper}, {helper}, {}}}
	signaler := &fakeSignaler{}
	if err := newTestService(inspector, signaler).Stop(fixture.resolved, fixture.resolved.Profiles["ninibin"], true); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	want := []signalCall{{pid: 101, signal: SignalTERM}, {pid: 303, signal: SignalKILL}}
	if !equalSignalCalls(signaler.calls, want) {
		t.Fatalf("signals = %#v, want %#v", signaler.calls, want)
	}
	if wantCalls := []int{101, 303}; !equalInts(inspector.revalidateCalls, wantCalls) {
		t.Fatalf("revalidation calls = %v, want %v", inspector.revalidateCalls, wantCalls)
	}
}

func TestForceKillsHelperWithValidatedInitialAncestry(t *testing.T) {
	fixture := newStopFixture(t, "ninibin")
	target := fixture.mainInfo(101, time.Unix(100, 0), "ninibin")
	helper := fixture.helperInfo(303, 101, time.Unix(110, 0))
	inspector := &fakeInspector{snapshots: [][]process.Info{{target, helper}, {helper}, {helper}, {}}}
	signaler := &fakeSignaler{}
	if err := newTestService(inspector, signaler).Stop(fixture.resolved, fixture.resolved.Profiles["ninibin"], true); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if want := []signalCall{{pid: 101, signal: SignalTERM}, {pid: 303, signal: SignalKILL}}; !equalSignalCalls(signaler.calls, want) {
		t.Fatalf("signals = %#v, want %#v", signaler.calls, want)
	}
}

func TestHelperWithOnlySharedExecutableIsNeverKilled(t *testing.T) {
	fixture := newStopFixture(t, "ninibin")
	target := fixture.mainInfo(101, time.Unix(100, 0), "ninibin")
	helper := fixture.helperInfo(303, 999, time.Unix(110, 0))
	inspector := &fakeInspector{snapshots: [][]process.Info{{target, helper}, {helper}}}
	signaler := &fakeSignaler{}
	err := newTestService(inspector, signaler).Stop(fixture.resolved, fixture.resolved.Profiles["ninibin"], false)
	if err != nil {
		t.Fatalf("Stop() error = %v, want success for unowned helper", err)
	}
	if want := []signalCall{{pid: 101, signal: SignalTERM}}; !equalSignalCalls(signaler.calls, want) {
		t.Fatalf("signals = %#v, want %#v", signaler.calls, want)
	}
}

func TestForceNeverKillsHelperWithConflictingUserDataDir(t *testing.T) {
	fixture := newStopFixture(t, "ninibin")
	target := fixture.mainInfo(101, time.Unix(100, 0), "ninibin")
	helper := fixture.helperInfo(303, 101, time.Unix(110, 0), "--user-data-dir=/tmp/other-profile")
	inspector := &fakeInspector{snapshots: [][]process.Info{{target, helper}, {helper}}}
	signaler := &fakeSignaler{}
	if err := newTestService(inspector, signaler).Stop(fixture.resolved, fixture.resolved.Profiles["ninibin"], true); err != nil {
		t.Fatalf("Stop() error = %v, want success without killing ambiguous helper", err)
	}
	if want := []signalCall{{pid: 101, signal: SignalTERM}}; !equalSignalCalls(signaler.calls, want) {
		t.Fatalf("signals = %#v, want %#v", signaler.calls, want)
	}
}

func TestHelperBelongingToOtherProfileIsNeverKilled(t *testing.T) {
	fixture := newStopFixture(t, "ninibin", "lucas")
	target := fixture.mainInfo(101, time.Unix(100, 0), "ninibin")
	other := fixture.mainInfo(202, time.Unix(200, 0), "lucas")
	helper := fixture.helperInfo(303, 202, time.Unix(210, 0))
	inspector := &fakeInspector{snapshots: [][]process.Info{{target, other, helper}, {other, helper}}}
	signaler := &fakeSignaler{}
	if err := newTestService(inspector, signaler).Stop(fixture.resolved, fixture.resolved.Profiles["ninibin"], false); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if want := []signalCall{{pid: 101, signal: SignalTERM}}; !equalSignalCalls(signaler.calls, want) {
		t.Fatalf("signals = %#v, want %#v", signaler.calls, want)
	}
}

func TestForceRevalidatesEveryResidualBeforeSIGKILL(t *testing.T) {
	fixture := newStopFixture(t, "ninibin")
	target := fixture.mainInfo(101, time.Unix(100, 0), "ninibin")
	first := fixture.helperInfo(303, 101, time.Unix(110, 0), "--user-data-dir="+fixture.paths["ninibin"].UserDataDir)
	second := fixture.helperInfo(304, 101, time.Unix(120, 0), "--user-data-dir="+fixture.paths["ninibin"].UserDataDir)
	inspector := &fakeInspector{snapshots: [][]process.Info{{target, first, second}, {first, second}, {first, second}, {second}, {}}, revalidate: map[int][]fakeRevalidation{}}
	signaler := &fakeSignaler{}
	if err := newTestService(inspector, signaler).Stop(fixture.resolved, fixture.resolved.Profiles["ninibin"], true); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	want := []signalCall{{pid: 101, signal: SignalTERM}, {pid: 303, signal: SignalKILL}, {pid: 304, signal: SignalKILL}}
	if !equalSignalCalls(signaler.calls, want) {
		t.Fatalf("signals = %#v, want %#v", signaler.calls, want)
	}
	if wantCalls := []int{101, 303, 304}; !equalInts(inspector.revalidateCalls, wantCalls) {
		t.Fatalf("revalidation calls = %v, want %v", inspector.revalidateCalls, wantCalls)
	}
}

func TestResidualRevalidationFailureStopsFurtherSIGKILL(t *testing.T) {
	fixture := newStopFixture(t, "ninibin")
	target := fixture.mainInfo(101, time.Unix(100, 0), "ninibin")
	first := fixture.helperInfo(303, 101, time.Unix(110, 0), "--user-data-dir="+fixture.paths["ninibin"].UserDataDir)
	second := fixture.helperInfo(304, 101, time.Unix(120, 0), "--user-data-dir="+fixture.paths["ninibin"].UserDataDir)
	changed := second
	changed.StartTime = time.Unix(999, 0)
	inspector := &fakeInspector{
		snapshots:  [][]process.Info{{target, first, second}, {first, second}, {first, second}},
		revalidate: map[int][]fakeRevalidation{304: {{info: changed, ok: true}}},
	}
	signaler := &fakeSignaler{}
	err := newTestService(inspector, signaler).Stop(fixture.resolved, fixture.resolved.Profiles["ninibin"], true)
	if !errors.Is(err, ErrIdentity) {
		t.Fatalf("Stop() error = %v, want identity error", err)
	}
	if want := []signalCall{{pid: 101, signal: SignalTERM}, {pid: 303, signal: SignalKILL}}; !equalSignalCalls(signaler.calls, want) {
		t.Fatalf("signals = %#v, want %#v", signaler.calls, want)
	}
}

func TestStopPropagatesSignalerErrorWithoutEscalation(t *testing.T) {
	fixture := newStopFixture(t, "ninibin")
	target := fixture.mainInfo(101, time.Unix(100, 0), "ninibin")
	signaler := &fakeSignaler{err: errors.New("signal denied")}
	err := newTestService(&fakeInspector{snapshots: [][]process.Info{{target}}}, signaler).Stop(fixture.resolved, fixture.resolved.Profiles["ninibin"], true)
	if err == nil || !strings.Contains(err.Error(), "signal denied") {
		t.Fatalf("Stop() error = %v, want signal error", err)
	}
	if len(signaler.calls) != 1 || signaler.calls[0].signal != SignalTERM {
		t.Fatalf("signals = %#v, want one SIGTERM", signaler.calls)
	}
}

func TestStopDoesNotCreateRuntimeDirectories(t *testing.T) {
	fixture := newStopFixture(t, "ninibin")
	target := fixture.mainInfo(101, time.Unix(100, 0), "ninibin")
	signaler := &fakeSignaler{}
	if err := newTestService(&fakeInspector{snapshots: [][]process.Info{{target}, {}}}, signaler).Stop(fixture.resolved, fixture.resolved.Profiles["ninibin"], false); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	for _, path := range []string{
		filepath.Join(fixture.resolved.RuntimeRoot, "profiles"),
		fixture.paths["ninibin"].CodexHome,
		fixture.paths["ninibin"].UserDataDir,
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("runtime path %q exists or returned unexpected error: %v", path, err)
		}
	}
}

func TestStopFailsIfOtherConfiguredMainDisappears(t *testing.T) {
	fixture := newStopFixture(t, "ninibin", "lucas")
	target := fixture.mainInfo(101, time.Unix(100, 0), "ninibin")
	other := fixture.mainInfo(202, time.Unix(200, 0), "lucas")
	signaler := &fakeSignaler{}
	err := newTestService(&fakeInspector{snapshots: [][]process.Info{{target, other}, {}}}, signaler).Stop(fixture.resolved, fixture.resolved.Profiles["ninibin"], false)
	if err == nil || !strings.Contains(err.Error(), `other profile "lucas"`) {
		t.Fatalf("Stop() error = %v, want other-profile protection error", err)
	}
	if want := []signalCall{{pid: 101, signal: SignalTERM}}; !equalSignalCalls(signaler.calls, want) {
		t.Fatalf("signals = %#v, want %#v", signaler.calls, want)
	}
}

func newTestService(inspector process.Inspector, signaler Signaler) Service {
	service := NewService(inspector, signaler)
	service.Timeout = time.Millisecond
	service.PollInterval = time.Nanosecond
	service.Sleep = func(time.Duration) {}
	return service
}

func newStopFixture(t *testing.T, profileIDs ...string) stopFixture {
	t.Helper()
	root := t.TempDir()
	appBase := t.TempDir()
	appPath := filepath.Join(appBase, "ChatGPT.app")
	executable := filepath.Join(appPath, "Contents", "MacOS", "ChatGPT")
	if err := os.MkdirAll(filepath.Dir(executable), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(executable, []byte("test executable"), 0o700); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks(root) error = %v", err)
	}
	canonicalAppPath, err := filepath.EvalSymlinks(appPath)
	if err != nil {
		t.Fatalf("EvalSymlinks(appPath) error = %v", err)
	}
	canonicalExecutable, err := filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatalf("EvalSymlinks(executable) error = %v", err)
	}

	profiles := make(map[string]config.ResolvedProfile, len(profileIDs))
	paths := make(map[string]runtime.ProfilePaths, len(profileIDs))
	for _, id := range profileIDs {
		derived, err := runtime.DeriveProfilePaths(canonicalRoot, id)
		if err != nil {
			t.Fatalf("DeriveProfilePaths() error = %v", err)
		}
		paths[id] = derived
		profiles[id] = config.ResolvedProfile{ID: id, CodexHome: derived.CodexHome, UserDataDir: derived.UserDataDir}
	}
	return stopFixture{
		resolved:   config.ResolvedConfig{RuntimeRoot: canonicalRoot, ChatGPTApp: canonicalAppPath, Profiles: profiles},
		paths:      paths,
		executable: canonicalExecutable,
		appPath:    canonicalAppPath,
	}
}

func (f stopFixture) mainInfo(pid int, start time.Time, profileID string) process.Info {
	return process.Info{
		PID:            pid,
		PPID:           1,
		StartTime:      start,
		ExecutablePath: f.executable,
		Args:           []string{"ChatGPT", "--user-data-dir=" + f.paths[profileID].UserDataDir},
	}
}

func (f stopFixture) helperInfo(pid, ppid int, start time.Time, args ...string) process.Info {
	return process.Info{
		PID:            pid,
		PPID:           ppid,
		StartTime:      start,
		ExecutablePath: filepath.Join(f.appPath, "Contents", "Frameworks", "ChatGPT Helper"),
		Args:           append([]string{"ChatGPT Helper"}, args...),
	}
}

func userDataFromArgs(args []string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, "--user-data-dir=") {
			return strings.TrimPrefix(arg, "--user-data-dir=")
		}
	}
	return ""
}

func equalSignalCalls(left, right []signalCall) bool {
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

func equalInts(left, right []int) bool {
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
