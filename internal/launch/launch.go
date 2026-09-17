package launch

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Luaphes/codex-profile-runtime/internal/config"
	"github.com/Luaphes/codex-profile-runtime/internal/process"
	"github.com/Luaphes/codex-profile-runtime/internal/runtime"
)

const (
	defaultVerificationTimeout = 8 * time.Second
	defaultPollInterval        = 100 * time.Millisecond
	openCommandPath            = "/usr/bin/open"
)

var (
	ErrAlreadyRunning      = errors.New("profile is already running")
	ErrVerificationTimeout = errors.New("launch verification timed out")
)

// CommandSpec is the argv and environment explicitly prepared for open(1).
type CommandSpec struct {
	Path string
	Args []string
	Env  []string
}

// CommandRunner executes one already-tokenized command.
type CommandRunner interface {
	Run(CommandSpec) error
}

type execRunner struct{}

// NewCommandRunner returns the production runner backed by os/exec.
func NewCommandRunner() CommandRunner {
	return execRunner{}
}

func (execRunner) Run(spec CommandSpec) error {
	command := exec.Command(spec.Path, spec.Args...)
	command.Env = mergeEnvironment(os.Environ(), spec.Env)
	return command.Run()
}

func mergeEnvironment(base, overrides []string) []string {
	merged := append([]string(nil), base...)
	for _, override := range overrides {
		key, _, ok := strings.Cut(override, "=")
		if !ok || key == "" {
			continue
		}

		filtered := merged[:0]
		for _, entry := range merged {
			entryKey, _, hasValue := strings.Cut(entry, "=")
			if !hasValue || entryKey != key {
				filtered = append(filtered, entry)
			}
		}
		merged = append(filtered, override)
	}
	return merged
}

// Service owns the small launch sequence and its injectable dependencies.
type Service struct {
	Runner       CommandRunner
	Scanner      process.Scanner
	Sleep        func(time.Duration)
	Timeout      time.Duration
	PollInterval time.Duration
}

// NewService creates a launch service with production timing defaults.
func NewService(runner CommandRunner, scanner process.Scanner) Service {
	return Service{
		Runner:       runner,
		Scanner:      scanner,
		Sleep:        time.Sleep,
		Timeout:      defaultVerificationTimeout,
		PollInterval: defaultPollInterval,
	}
}

// Launch validates the app, prepares one profile's directories, starts open -n,
// and waits for the exact ChatGPT main-process identity to appear.
func (s Service) Launch(appPath, runtimeRoot string, profile config.ResolvedProfile) (process.Info, error) {
	if s.Runner == nil {
		return process.Info{}, fmt.Errorf("launch command runner is not configured")
	}
	if s.Scanner == nil {
		return process.Info{}, fmt.Errorf("process scanner is not configured")
	}

	executablePath, err := ValidateApp(appPath)
	if err != nil {
		return process.Info{}, err
	}

	paths, err := runtime.PrepareProfilePaths(runtimeRoot, profile.ID, runtime.ProfilePaths{
		CodexHome:   profile.CodexHome,
		UserDataDir: profile.UserDataDir,
	})
	if err != nil {
		return process.Info{}, err
	}
	profile.CodexHome = paths.CodexHome
	profile.UserDataDir = paths.UserDataDir

	current, found, err := s.Scanner.FindMain(executablePath, profile.UserDataDir)
	if err != nil {
		return process.Info{}, fmt.Errorf("check existing profile process: %w", err)
	}
	if found {
		return current, fmt.Errorf("%w: %q", ErrAlreadyRunning, profile.ID)
	}

	spec, err := BuildOpenCommand(appPath, profile)
	if err != nil {
		return process.Info{}, err
	}
	if err := s.Runner.Run(spec); err != nil {
		return process.Info{}, fmt.Errorf("start ChatGPT: %w", err)
	}

	return s.waitForMain(executablePath, profile.UserDataDir)
}

// BuildOpenCommand derives all launch proxy settings from profile.Proxy.
func BuildOpenCommand(appPath string, profile config.ResolvedProfile) (CommandSpec, error) {
	if strings.TrimSpace(appPath) == "" {
		return CommandSpec{}, fmt.Errorf("ChatGPT app path must not be empty")
	}
	if profile.CodexHome == "" || profile.UserDataDir == "" {
		return CommandSpec{}, fmt.Errorf("profile %q has incomplete runtime paths", profile.ID)
	}

	spec := CommandSpec{
		Path: openCommandPath,
		Args: []string{
			"-n",
			"-a",
			appPath,
			"--args",
			"--user-data-dir=" + profile.UserDataDir,
		},
		Env: []string{
			"CODEX_HOME=" + profile.CodexHome,
			"CODEX_ELECTRON_USER_DATA_PATH=" + profile.UserDataDir,
		},
	}

	if profile.Proxy != "" {
		spec.Args = append(spec.Args, "--proxy-server="+profile.Proxy)
		spec.Env = append(spec.Env,
			"HTTP_PROXY="+profile.Proxy,
			"HTTPS_PROXY="+profile.Proxy,
			"ALL_PROXY="+profile.Proxy,
		)
	}

	return spec, nil
}

// ValidateApp checks the bundle shape and returns its canonical main executable path.
func ValidateApp(appPath string) (string, error) {
	cleanAppPath := filepath.Clean(appPath)
	info, err := os.Stat(cleanAppPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("ChatGPT app not found at %q", cleanAppPath)
		}
		return "", fmt.Errorf("inspect ChatGPT app %q: %w", cleanAppPath, err)
	}
	if !info.IsDir() || !strings.HasSuffix(cleanAppPath, ".app") {
		return "", fmt.Errorf("ChatGPT app path must be a .app directory: %q", cleanAppPath)
	}

	executable := filepath.Join(cleanAppPath, "Contents", "MacOS", "ChatGPT")
	executableInfo, err := os.Stat(executable)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("ChatGPT executable not found at %q", executable)
		}
		return "", fmt.Errorf("inspect ChatGPT executable %q: %w", executable, err)
	}
	if executableInfo.IsDir() {
		return "", fmt.Errorf("ChatGPT executable is a directory: %q", executable)
	}

	realExecutable, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return "", fmt.Errorf("resolve ChatGPT executable %q: %w", executable, err)
	}
	return filepath.Abs(realExecutable)
}

func (s Service) waitForMain(executablePath, userDataDir string) (process.Info, error) {
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = defaultVerificationTimeout
	}
	pollInterval := s.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultPollInterval
	}
	sleep := s.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}

	deadline := time.Now().Add(timeout)
	for {
		info, found, err := s.Scanner.FindMain(executablePath, userDataDir)
		if err != nil {
			return process.Info{}, fmt.Errorf("verify ChatGPT launch: %w", err)
		}
		if found {
			return info, nil
		}
		if time.Now().After(deadline) {
			return process.Info{}, fmt.Errorf("%w for user-data-dir %q", ErrVerificationTimeout, userDataDir)
		}
		sleep(pollInterval)
	}
}
