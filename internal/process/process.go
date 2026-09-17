package process

import "path/filepath"

// Info is the minimum process identity needed for launch verification.
type Info struct {
	PID            int
	ExecutablePath string
	Args           []string
}

// Scanner finds the current user's ChatGPT main process for one exact profile identity.
type Scanner interface {
	FindMain(executablePath, userDataDir string) (Info, bool, error)
}

// MatchesMainProcess applies the exact identity rule shared by scanners and tests.
func MatchesMainProcess(info Info, executablePath, userDataDir string) bool {
	if filepath.Clean(info.ExecutablePath) != filepath.Clean(executablePath) {
		return false
	}

	expectedArg := "--user-data-dir=" + filepath.Clean(userDataDir)
	for _, arg := range info.Args {
		if arg == expectedArg {
			return true
		}
	}
	return false
}
