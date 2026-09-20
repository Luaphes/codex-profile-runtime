package stop

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Luaphes/codex-profile-runtime/internal/config"
	"github.com/Luaphes/codex-profile-runtime/internal/launch"
	"github.com/Luaphes/codex-profile-runtime/internal/process"
	"github.com/Luaphes/codex-profile-runtime/internal/runtime"
)

const (
	defaultStopTimeout  = 8 * time.Second
	defaultPollInterval = 100 * time.Millisecond
)

var (
	ErrNotRunning  = errors.New("profile is not running")
	ErrAmbiguous   = errors.New("profile identity is ambiguous")
	ErrIdentity    = errors.New("profile process identity changed")
	ErrStopTimeout = errors.New("profile stop timed out")
	ErrResidual    = errors.New("profile residual processes remain")
)

// Service owns the conservative stop sequence and its injectable dependencies.
type Service struct {
	Inspector    process.Inspector
	Signaler     Signaler
	Sleep        func(time.Duration)
	Timeout      time.Duration
	PollInterval time.Duration
}

// NewService creates a stop service with production timing defaults.
func NewService(inspector process.Inspector, signaler Signaler) Service {
	return Service{
		Inspector:    inspector,
		Signaler:     signaler,
		Sleep:        time.Sleep,
		Timeout:      defaultStopTimeout,
		PollInterval: defaultPollInterval,
	}
}

// Stop terminates one configured profile. It never creates or modifies runtime directories.
func (s Service) Stop(resolved config.ResolvedConfig, profile config.ResolvedProfile, force bool) error {
	if s.Inspector == nil {
		return fmt.Errorf("process inspector is not configured")
	}
	if s.Signaler == nil {
		return fmt.Errorf("process signaler is not configured")
	}

	executablePath, err := launch.ValidateApp(resolved.ChatGPTApp)
	if err != nil {
		return err
	}
	bundleRoot, err := bundleRootFromExecutable(executablePath)
	if err != nil {
		return err
	}
	profilePaths, err := resolveProfilePaths(resolved)
	if err != nil {
		return err
	}
	targetPaths, ok := profilePaths[profile.ID]
	if !ok {
		return fmt.Errorf("profile %q is not configured", profile.ID)
	}

	initialSnapshot, err := s.Inspector.SnapshotAll()
	if err != nil {
		return fmt.Errorf("scan ChatGPT processes: %w", err)
	}
	identities, err := identifyConfiguredProfiles(resolved, profilePaths, executablePath, initialSnapshot)
	if err != nil {
		return err
	}
	targetIdentity, ok := identities[profile.ID]
	if !ok {
		return fmt.Errorf(`profile %q is not running: %w`, profile.ID, ErrNotRunning)
	}
	if targetIdentity.Info.StartTime.IsZero() {
		return fmt.Errorf(`profile %q matched a process without a start time: %w`, profile.ID, ErrIdentity)
	}

	otherIdentities := make(map[string]profileIdentity, len(identities)-1)
	for id, identity := range identities {
		if id != profile.ID {
			otherIdentities[id] = identity
		}
	}
	evidence := captureOwnershipEvidence(initialSnapshot, targetIdentity, otherIdentities, targetPaths.UserDataDir, bundleRoot)

	validated, matches, err := s.revalidateMain(targetIdentity, executablePath, targetPaths.UserDataDir)
	if err != nil {
		return fmt.Errorf(`profile %q: %w`, profile.ID, err)
	}
	if !matches {
		return fmt.Errorf(`profile %q: %w`, profile.ID, ErrIdentity)
	}
	if err := s.Signaler.Signal(validated.PID, SignalTERM); err != nil {
		return fmt.Errorf(`profile %q: send SIGTERM: %w`, profile.ID, err)
	}

	snapshot, timedOut, err := s.waitForMainExit(executablePath, targetPaths.UserDataDir)
	if err != nil {
		return fmt.Errorf(`profile %q: %w`, profile.ID, err)
	}
	if timedOut {
		if !force {
			return fmt.Errorf(`profile %q: %w`, profile.ID, ErrStopTimeout)
		}

		if err := s.forceMain(targetIdentity.Info, executablePath, targetPaths.UserDataDir, snapshot, profile.ID); err != nil {
			return err
		}
		snapshot, timedOut, err = s.waitForMainExit(executablePath, targetPaths.UserDataDir)
		if err != nil {
			return fmt.Errorf(`profile %q after SIGKILL: %w`, profile.ID, err)
		}
		if timedOut {
			return fmt.Errorf(`profile %q after SIGKILL: %w`, profile.ID, ErrStopTimeout)
		}
	}

	residuals := classifyResiduals(snapshot, targetIdentity, otherIdentities, targetPaths.UserDataDir, bundleRoot, evidence)
	if residuals.any() {
		if !force {
			return fmt.Errorf(`profile %q main process exited, but residual processes remain: %w`, profile.ID, ErrResidual)
		}
		if len(residuals.owned) > 0 {
			if err := s.forceResiduals(residuals.owned, targetIdentity, otherIdentities, targetPaths.UserDataDir, bundleRoot, evidence, profile.ID); err != nil {
				return err
			}

			snapshot, timedOut, err = s.waitForResidualExit(targetIdentity, otherIdentities, targetPaths.UserDataDir, bundleRoot, evidence)
			if err != nil {
				return fmt.Errorf(`profile %q after residual cleanup: %w`, profile.ID, err)
			}
			if timedOut {
				return fmt.Errorf(`profile %q after residual cleanup: %w`, profile.ID, ErrResidual)
			}
			residuals = classifyResiduals(snapshot, targetIdentity, otherIdentities, targetPaths.UserDataDir, bundleRoot, evidence)
		}
		if residuals.any() {
			return fmt.Errorf(`profile %q after residual cleanup: %w`, profile.ID, ErrResidual)
		}
	}

	if err := s.verifyFinalState(profile.ID, executablePath, targetIdentity, otherIdentities, targetPaths.UserDataDir, bundleRoot, evidence); err != nil {
		return err
	}
	return nil
}

type profileIdentity struct {
	Info        process.Info
	UserDataDir string
}

type ownershipEvidence struct {
	targetOwned map[int]process.Info
}

type residualProcesses struct {
	owned []process.Info
}

func (r residualProcesses) any() bool {
	return len(r.owned) > 0
}

func resolveProfilePaths(resolved config.ResolvedConfig) (map[string]runtime.ProfilePaths, error) {
	ids := make([]string, 0, len(resolved.Profiles))
	for id := range resolved.Profiles {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	paths := make(map[string]runtime.ProfilePaths, len(ids))
	for _, id := range ids {
		profile := resolved.Profiles[id]
		resolvedPaths, err := runtime.ResolveProfilePaths(resolved.RuntimeRoot, id, runtime.ProfilePaths{
			CodexHome:   profile.CodexHome,
			UserDataDir: profile.UserDataDir,
		})
		if err != nil {
			return nil, fmt.Errorf("profile %q: resolve runtime identity: %w", id, err)
		}
		paths[id] = resolvedPaths
	}
	return paths, nil
}

func identifyConfiguredProfiles(resolved config.ResolvedConfig, paths map[string]runtime.ProfilePaths, executablePath string, snapshot []process.Info) (map[string]profileIdentity, error) {
	ids := make([]string, 0, len(resolved.Profiles))
	for id := range resolved.Profiles {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	identities := make(map[string]profileIdentity)
	for _, id := range ids {
		matches := matchingMainProcesses(snapshot, executablePath, paths[id].UserDataDir)
		if len(matches) > 1 {
			return nil, fmt.Errorf(`profile %q matched multiple ChatGPT main processes: %w`, id, ErrAmbiguous)
		}
		if len(matches) == 1 {
			if matches[0].StartTime.IsZero() {
				return nil, fmt.Errorf(`profile %q matched a process without a start time: %w`, id, ErrIdentity)
			}
			identities[id] = profileIdentity{Info: matches[0], UserDataDir: paths[id].UserDataDir}
		}
	}
	return identities, nil
}

func matchingMainProcesses(snapshot []process.Info, executablePath, userDataDir string) []process.Info {
	matches := make([]process.Info, 0, 1)
	for _, candidate := range snapshot {
		if process.MatchesMainProcess(candidate, executablePath, userDataDir) {
			matches = append(matches, candidate)
		}
	}
	return matches
}

func (s Service) revalidateMain(identity profileIdentity, executablePath, userDataDir string) (process.Info, bool, error) {
	current, ok, err := s.Inspector.Revalidate(identity.Info)
	if err != nil {
		return process.Info{}, false, fmt.Errorf("revalidate target process: %w", err)
	}
	if !ok || !process.SameIdentity(identity.Info, current) || !process.MatchesMainProcess(current, executablePath, userDataDir) {
		return current, false, nil
	}
	return current, true, nil
}

func (s Service) waitForMainExit(executablePath, userDataDir string) ([]process.Info, bool, error) {
	deadline := time.Now().Add(s.timeout())
	for {
		snapshot, err := s.Inspector.SnapshotAll()
		if err != nil {
			return nil, false, fmt.Errorf("rescan ChatGPT processes: %w", err)
		}
		matches := matchingMainProcesses(snapshot, executablePath, userDataDir)
		if len(matches) > 1 {
			return nil, false, fmt.Errorf("profile matched multiple ChatGPT main processes: %w", ErrAmbiguous)
		}
		if len(matches) == 0 {
			return snapshot, false, nil
		}
		if time.Now().After(deadline) {
			return snapshot, true, nil
		}
		s.sleep()
	}
}

func (s Service) waitForResidualExit(target profileIdentity, other map[string]profileIdentity, userDataDir, bundleRoot string, evidence ownershipEvidence) ([]process.Info, bool, error) {
	deadline := time.Now().Add(s.timeout())
	for {
		snapshot, err := s.Inspector.SnapshotAll()
		if err != nil {
			return nil, false, fmt.Errorf("rescan ChatGPT processes: %w", err)
		}
		if matches := matchingMainProcesses(snapshot, target.Info.ExecutablePath, userDataDir); len(matches) > 0 {
			return nil, false, fmt.Errorf("profile main process reappeared: %w", ErrIdentity)
		}
		residuals := classifyResiduals(snapshot, target, other, userDataDir, bundleRoot, evidence)
		if len(residuals.owned) == 0 {
			return snapshot, false, nil
		}
		if time.Now().After(deadline) {
			return snapshot, true, nil
		}
		s.sleep()
	}
}

func (s Service) forceMain(target process.Info, executablePath, userDataDir string, snapshot []process.Info, profileID string) error {
	matches := matchingMainProcesses(snapshot, executablePath, userDataDir)
	if len(matches) != 1 || !process.SameIdentity(target, matches[0]) {
		return fmt.Errorf(`profile %q: %w`, profileID, ErrIdentity)
	}
	current, ok, err := s.Inspector.Revalidate(target)
	if err != nil {
		return fmt.Errorf(`profile %q: revalidate target before SIGKILL: %w`, profileID, err)
	}
	if !ok || !process.SameIdentity(target, current) || !process.MatchesMainProcess(current, executablePath, userDataDir) {
		return fmt.Errorf(`profile %q: %w`, profileID, ErrIdentity)
	}
	if err := s.Signaler.Signal(current.PID, SignalKILL); err != nil {
		return fmt.Errorf(`profile %q: send SIGKILL: %w`, profileID, err)
	}
	return nil
}

func (s Service) forceResiduals(residuals []process.Info, target profileIdentity, other map[string]profileIdentity, userDataDir, bundleRoot string, evidence ownershipEvidence, profileID string) error {
	for _, expected := range residuals {
		freshSnapshot, err := s.Inspector.SnapshotAll()
		if err != nil {
			return fmt.Errorf(`profile %q: rescan before residual pid %d SIGKILL: %w`, profileID, expected.PID, err)
		}
		if matches := matchingMainProcesses(freshSnapshot, target.Info.ExecutablePath, userDataDir); len(matches) > 0 {
			return fmt.Errorf(`profile %q: main process reappeared before residual pid %d SIGKILL: %w`, profileID, expected.PID, ErrIdentity)
		}
		current, ok, err := s.Inspector.Revalidate(expected)
		if err != nil {
			return fmt.Errorf(`profile %q: revalidate residual pid %d before SIGKILL: %w`, profileID, expected.PID, err)
		}
		if !ok || !process.SameIdentity(expected, current) {
			return fmt.Errorf(`profile %q: residual pid %d: %w`, profileID, expected.PID, ErrIdentity)
		}
		if !isTargetOwned(current, freshSnapshot, target, other, userDataDir, bundleRoot, evidence) {
			return fmt.Errorf(`profile %q: residual pid %d ownership is ambiguous: %w`, profileID, expected.PID, ErrIdentity)
		}
		if err := s.Signaler.Signal(current.PID, SignalKILL); err != nil {
			return fmt.Errorf(`profile %q: send SIGKILL to residual pid %d: %w`, profileID, current.PID, err)
		}
	}
	return nil
}

func captureOwnershipEvidence(snapshot []process.Info, target profileIdentity, other map[string]profileIdentity, targetUserData, bundleRoot string) ownershipEvidence {
	evidence := ownershipEvidence{
		targetOwned: make(map[int]process.Info),
	}
	for _, candidate := range snapshot {
		if candidate.PID == target.Info.PID || !pathWithin(bundleRoot, candidate.ExecutablePath) {
			continue
		}
		if hasConflictingUserDataDir(candidate, targetUserData) {
			continue
		}
		if isForeignOwned(candidate, snapshot, other) {
			continue
		}
		if process.HasExactUserDataDir(candidate, targetUserData) || hasAncestryTo(candidate, target.Info, snapshot) {
			evidence.targetOwned[candidate.PID] = candidate
		}
	}
	return evidence
}

func classifyResiduals(snapshot []process.Info, target profileIdentity, other map[string]profileIdentity, targetUserData, bundleRoot string, evidence ownershipEvidence) residualProcesses {
	// A shared ChatGPT.app executable alone is not ownership evidence. Such
	// processes stay untouched and do not make this profile appear stopped
	// incompletely unless stronger target evidence exists.
	residuals := residualProcesses{}
	for _, candidate := range snapshot {
		if candidate.PID <= 0 || candidate.PID == target.Info.PID || !pathWithin(bundleRoot, candidate.ExecutablePath) {
			continue
		}
		if isForeignOwned(candidate, snapshot, other) {
			continue
		}
		if isTargetOwned(candidate, snapshot, target, other, targetUserData, bundleRoot, evidence) {
			residuals.owned = append(residuals.owned, candidate)
		}
	}
	sort.Slice(residuals.owned, func(i, j int) bool { return residuals.owned[i].PID < residuals.owned[j].PID })
	return residuals
}

func isTargetOwned(candidate process.Info, snapshot []process.Info, target profileIdentity, other map[string]profileIdentity, targetUserData, bundleRoot string, evidence ownershipEvidence) bool {
	if candidate.PID <= 0 || candidate.PID == target.Info.PID || !pathWithin(bundleRoot, candidate.ExecutablePath) {
		return false
	}
	if hasConflictingUserDataDir(candidate, targetUserData) {
		return false
	}
	if isForeignOwned(candidate, snapshot, other) {
		return false
	}
	if process.HasExactUserDataDir(candidate, targetUserData) {
		return true
	}
	if initial, ok := evidence.targetOwned[candidate.PID]; ok && process.SameIdentity(initial, candidate) {
		return true
	}
	return hasAncestryTo(candidate, target.Info, snapshot)
}

func hasConflictingUserDataDir(info process.Info, targetUserData string) bool {
	expected := filepath.Clean(targetUserData)
	for _, candidate := range process.UserDataDirs(info) {
		if candidate != expected {
			return true
		}
	}
	return false
}

func isForeignOwned(candidate process.Info, snapshot []process.Info, other map[string]profileIdentity) bool {
	for _, identity := range other {
		if candidate.PPID == identity.Info.PID || process.HasExactUserDataDir(candidate, identity.UserDataDir) || hasAncestryTo(candidate, identity.Info, snapshot) {
			return true
		}
	}
	return false
}

func hasAncestryTo(candidate, ancestor process.Info, snapshot []process.Info) bool {
	byPID := make(map[int]process.Info, len(snapshot))
	for _, info := range snapshot {
		if info.PID > 0 {
			byPID[info.PID] = info
		}
	}
	visited := make(map[int]struct{})
	current := candidate
	for current.PPID > 0 {
		if _, seen := visited[current.PID]; seen {
			return false
		}
		visited[current.PID] = struct{}{}
		parent, ok := byPID[current.PPID]
		if !ok {
			return false
		}
		if process.SameIdentity(parent, ancestor) {
			return true
		}
		current = parent
	}
	return false
}

func verifyOtherProfiles(snapshot []process.Info, other map[string]profileIdentity) error {
	ids := make([]string, 0, len(other))
	for id := range other {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		identity := other[id]
		matches := matchingMainProcesses(snapshot, identity.Info.ExecutablePath, identity.UserDataDir)
		if len(matches) != 1 || !process.SameIdentity(identity.Info, matches[0]) {
			return fmt.Errorf("other profile %q was affected or became ambiguous", id)
		}
	}
	return nil
}

func (s Service) verifyFinalState(profileID, executablePath string, target profileIdentity, other map[string]profileIdentity, targetUserData, bundleRoot string, evidence ownershipEvidence) error {
	snapshot, err := s.Inspector.SnapshotAll()
	if err != nil {
		return fmt.Errorf(`profile %q: final process scan: %w`, profileID, err)
	}

	matches := matchingMainProcesses(snapshot, executablePath, targetUserData)
	if len(matches) > 1 {
		return fmt.Errorf(`profile %q reappeared with multiple ChatGPT main processes: %w`, profileID, ErrAmbiguous)
	}
	if len(matches) == 1 {
		return fmt.Errorf(`profile %q reappeared before stop completed: %w`, profileID, ErrIdentity)
	}

	residuals := classifyResiduals(snapshot, target, other, targetUserData, bundleRoot, evidence)
	if residuals.any() {
		return fmt.Errorf(`profile %q still has target-owned residual processes: %w`, profileID, ErrResidual)
	}
	if err := verifyOtherProfiles(snapshot, other); err != nil {
		return err
	}
	return nil
}

func bundleRootFromExecutable(executablePath string) (string, error) {
	macOSDir := filepath.Dir(filepath.Clean(executablePath))
	contentsDir := filepath.Dir(macOSDir)
	bundleRoot := filepath.Dir(contentsDir)
	if filepath.Base(contentsDir) != "Contents" || !strings.HasSuffix(bundleRoot, ".app") {
		return "", fmt.Errorf("ChatGPT executable is not inside a .app bundle: %q", executablePath)
	}
	return bundleRoot, nil
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && relative != "."
}

func (s Service) timeout() time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	return defaultStopTimeout
}

func (s Service) sleep() {
	sleep := s.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	interval := s.PollInterval
	if interval <= 0 {
		interval = defaultPollInterval
	}
	sleep(interval)
}
