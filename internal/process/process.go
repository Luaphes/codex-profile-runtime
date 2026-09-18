package process

import (
	"path/filepath"
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
	expectedArg := "--user-data-dir=" + filepath.Clean(userDataDir)
	for _, arg := range info.Args {
		if arg == expectedArg {
			return true
		}
	}
	return false
}

// MatchesMainProcess applies the exact identity rule shared by scanners and tests.
func MatchesMainProcess(info Info, executablePath, userDataDir string) bool {
	if filepath.Clean(info.ExecutablePath) != filepath.Clean(executablePath) {
		return false
	}
	return HasExactUserDataDir(info, userDataDir)
}

// SameIdentity compares the PID and stable process identity fields needed to
// defend against PID reuse before sending a signal.
func SameIdentity(left, right Info) bool {
	return left.PID > 0 && left.PID == right.PID &&
		!left.StartTime.IsZero() && !right.StartTime.IsZero() &&
		left.StartTime.Equal(right.StartTime) &&
		filepath.Clean(left.ExecutablePath) == filepath.Clean(right.ExecutablePath)
}
