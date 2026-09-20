package process

import (
	"path/filepath"
	"strings"
	"time"
)

// Info is the minimum process identity needed for runtime verification.
type Info struct {
	PID            int
	PPID           int
	StartTime      time.Time
	ExecutablePath string
	Args           []string
}

// Scanner finds the current user's ChatGPT main process for one exact profile identity.
type Scanner interface {
	FindMain(executablePath, userDataDir string) (Info, bool, error)
}

// Snapshotter returns one read-only snapshot filtered to a target executable.
type Snapshotter interface {
	Snapshot(executablePath string) ([]Info, error)
}

// AllSnapshotter returns one snapshot of all current-user processes.
type AllSnapshotter interface {
	SnapshotAll() ([]Info, error)
}

// Revalidator re-reads one PID and confirms its stable process identity.
type Revalidator interface {
	Revalidate(info Info) (Info, bool, error)
}

// Inspector is the process capability required by safe stop.
type Inspector interface {
	Snapshotter
	AllSnapshotter
	Revalidator
}

// HasExactUserDataDir reports whether argv contains the exact profile identity argument.
func HasExactUserDataDir(info Info, userDataDir string) bool {
	expected := filepath.Clean(userDataDir)
	for _, candidate := range UserDataDirs(info) {
		if candidate == expected {
			return true
		}
	}
	return false
}

// UserDataDirs returns every explicit --user-data-dir value in argv using the
// same lexical normalization as the runtime identity checks.
func UserDataDirs(info Info) []string {
	const prefix = "--user-data-dir="

	dirs := make([]string, 0, 1)
	for _, arg := range info.Args {
		if strings.HasPrefix(arg, prefix) {
			dirs = append(dirs, filepath.Clean(strings.TrimPrefix(arg, prefix)))
		}
	}
	return dirs
}

// MatchesMainProcess applies the exact identity rule shared by scanners and tests.
func MatchesMainProcess(info Info, executablePath, userDataDir string) bool {
	if filepath.Clean(info.ExecutablePath) != filepath.Clean(executablePath) {
		return false
	}
	if !HasExactUserDataDir(info, userDataDir) {
		return false
	}

	// Multiple conflicting identity arguments are ambiguous and must not be
	// treated as a match for either profile.
	expected := filepath.Clean(userDataDir)
	for _, candidate := range UserDataDirs(info) {
		if candidate != expected {
			return false
		}
	}
	return true
}

// SameIdentity compares the PID and stable process identity fields needed to
// defend against PID reuse before sending a signal.
func SameIdentity(left, right Info) bool {
	return left.PID > 0 && left.PID == right.PID &&
		!left.StartTime.IsZero() && !right.StartTime.IsZero() &&
		left.StartTime.Equal(right.StartTime) &&
		filepath.Clean(left.ExecutablePath) == filepath.Clean(right.ExecutablePath)
}
