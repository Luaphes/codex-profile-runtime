package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultRuntimeRootSuffix = "Library/Application Support/CodexProfileRuntime"
	DefaultChatGPTApp        = "/Applications/ChatGPT.app"
)

// ProfilePaths contains the runtime-owned directories derived from one profile id.
type ProfilePaths struct {
	CodexHome   string
	UserDataDir string
}

// DefaultRuntimeRoot returns the default runtime root for a resolved home directory.
func DefaultRuntimeRoot(homeDir string) string {
	return filepath.Join(homeDir, defaultRuntimeRootSuffix)
}

// DefaultConfigPath returns the default config path without consulting config contents.
func DefaultConfigPath(homeDir string) string {
	return filepath.Join(DefaultRuntimeRoot(homeDir), "config.json")
}

// NormalizePath performs lexical normalization for a stable runtime path.
// Stable runtime paths must be absolute or use the ~/... form. It intentionally
// does not resolve symlinks, because the path may not exist yet.
func NormalizePath(rawPath, homeDir string) (string, error) {
	cleaned, err := expandAndClean(rawPath, homeDir)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("path must be absolute or use ~/...: %q", rawPath)
	}
	return cleaned, nil
}

// NormalizeConfigPath performs lexical normalization and absolute resolution
// for the config file itself. Relative config paths are intentionally allowed.
func NormalizeConfigPath(rawPath, homeDir string) (string, error) {
	cleaned, err := expandAndClean(rawPath, homeDir)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(cleaned) {
		return cleaned, nil
	}

	absolute, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	return filepath.Clean(absolute), nil
}

// DeriveProfilePaths derives the two v0.1 runtime directories from an absolute root.
func DeriveProfilePaths(runtimeRoot, profileID string) (ProfilePaths, error) {
	if runtimeRoot == "" {
		return ProfilePaths{}, fmt.Errorf("runtime root must not be empty")
	}
	if !filepath.IsAbs(runtimeRoot) {
		return ProfilePaths{}, fmt.Errorf("runtime root must be absolute")
	}
	if profileID == "" || profileID == "." || profileID == ".." || strings.ContainsRune(profileID, filepath.Separator) {
		return ProfilePaths{}, fmt.Errorf("profile id cannot be used as a path component: %q", profileID)
	}

	base := filepath.Join(runtimeRoot, "profiles", profileID)
	return ProfilePaths{
		CodexHome:   filepath.Clean(filepath.Join(base, "codex-home")),
		UserDataDir: filepath.Clean(filepath.Join(base, "chatgpt-user-data")),
	}, nil
}

// PrepareProfilePaths creates the current profile's runtime directories and
// returns their canonical identities. It performs lexical path derivation
// before creation, then resolves symlinks after creation and rejects paths
// that escape the runtime root.
func PrepareProfilePaths(runtimeRoot, profileID string, expected ProfilePaths) (ProfilePaths, error) {
	derived, err := DeriveProfilePaths(runtimeRoot, profileID)
	if err != nil {
		return ProfilePaths{}, err
	}
	if err := os.MkdirAll(runtimeRoot, 0o700); err != nil {
		return ProfilePaths{}, fmt.Errorf("create runtime root: %w", err)
	}
	realRuntimeRoot, err := filepath.EvalSymlinks(runtimeRoot)
	if err != nil {
		return ProfilePaths{}, fmt.Errorf("resolve runtime root: %w", err)
	}
	realRuntimeRoot, err = filepath.Abs(realRuntimeRoot)
	if err != nil {
		return ProfilePaths{}, fmt.Errorf("resolve runtime root absolute path: %w", err)
	}
	canonicalDerived, err := DeriveProfilePaths(realRuntimeRoot, profileID)
	if err != nil {
		return ProfilePaths{}, err
	}
	if !matchesDerivedPath(expected.CodexHome, derived.CodexHome, canonicalDerived.CodexHome) ||
		!matchesDerivedPath(expected.UserDataDir, derived.UserDataDir, canonicalDerived.UserDataDir) {
		return ProfilePaths{}, fmt.Errorf("profile paths do not match derived paths")
	}
	if err := secureDirectory(realRuntimeRoot, "runtime root"); err != nil {
		return ProfilePaths{}, err
	}

	profilesRoot := filepath.Join(realRuntimeRoot, "profiles")
	profileRoot := filepath.Join(profilesRoot, profileID)
	codexHome := filepath.Join(profileRoot, "codex-home")
	userDataDir := filepath.Join(profileRoot, "chatgpt-user-data")

	for _, path := range []string{profilesRoot, profileRoot, codexHome, userDataDir} {
		if err := ensureDirectoryWithin(path, realRuntimeRoot); err != nil {
			return ProfilePaths{}, err
		}
	}

	realProfileRoot, err := filepath.EvalSymlinks(profileRoot)
	if err != nil {
		return ProfilePaths{}, fmt.Errorf("resolve profile runtime root: %w", err)
	}
	realProfileRoot, err = filepath.Abs(realProfileRoot)
	if err != nil {
		return ProfilePaths{}, fmt.Errorf("resolve profile runtime root absolute path: %w", err)
	}
	if !isWithin(realRuntimeRoot, realProfileRoot) {
		return ProfilePaths{}, fmt.Errorf("profile runtime root escapes runtime root: %q", profileID)
	}

	realCodexHome, err := filepath.EvalSymlinks(codexHome)
	if err != nil {
		return ProfilePaths{}, fmt.Errorf("resolve codex home: %w", err)
	}
	realCodexHome, err = filepath.Abs(realCodexHome)
	if err != nil {
		return ProfilePaths{}, fmt.Errorf("resolve codex home absolute path: %w", err)
	}
	if !isWithin(realRuntimeRoot, realCodexHome) || !isWithin(realProfileRoot, realCodexHome) {
		return ProfilePaths{}, fmt.Errorf("codex home escapes profile runtime root: %q", realCodexHome)
	}

	realUserDataDir, err := filepath.EvalSymlinks(userDataDir)
	if err != nil {
		return ProfilePaths{}, fmt.Errorf("resolve user data directory: %w", err)
	}
	realUserDataDir, err = filepath.Abs(realUserDataDir)
	if err != nil {
		return ProfilePaths{}, fmt.Errorf("resolve user data directory absolute path: %w", err)
	}
	if !isWithin(realRuntimeRoot, realUserDataDir) || !isWithin(realProfileRoot, realUserDataDir) {
		return ProfilePaths{}, fmt.Errorf("user data directory escapes profile runtime root: %q", realUserDataDir)
	}

	return ProfilePaths{
		CodexHome:   realCodexHome,
		UserDataDir: realUserDataDir,
	}, nil
}

func ensureDirectoryWithin(path, runtimeRoot string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("inspect runtime directory %q: %w", path, err)
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			return fmt.Errorf("create runtime directory %q: %w", path, err)
		}
		return nil
	}

	if info.Mode()&os.ModeSymlink != 0 {
		realPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("resolve runtime directory symlink %q: %w", path, err)
		}
		realPath, err = filepath.Abs(realPath)
		if err != nil {
			return fmt.Errorf("resolve runtime directory symlink absolute path %q: %w", path, err)
		}
		if !isWithin(runtimeRoot, realPath) {
			return fmt.Errorf("runtime directory symlink escapes runtime root: %q -> %q", path, realPath)
		}
		return fmt.Errorf("manager-owned runtime path must not be a symlink: %q -> %q", path, realPath)
	}

	if !info.IsDir() {
		return fmt.Errorf("runtime path is not a directory: %q", path)
	}
	return secureDirectory(path, "runtime directory")
}

func matchesDerivedPath(actual, lexical, canonical string) bool {
	actual = filepath.Clean(actual)
	return actual == filepath.Clean(lexical) || actual == filepath.Clean(canonical)
}

func secureDirectory(path, description string) error {
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure %s %q: %w", description, path, err)
	}
	return nil
}

func isWithin(root, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func expandHome(rawPath, homeDir string) (string, error) {
	if rawPath != "~" && !strings.HasPrefix(rawPath, "~"+string(filepath.Separator)) {
		if strings.HasPrefix(rawPath, "~") {
			return "", fmt.Errorf("unsupported home expansion in path %q", rawPath)
		}
		return rawPath, nil
	}
	if homeDir == "" {
		return "", fmt.Errorf("home directory is required to expand %q", rawPath)
	}

	resolvedHome, err := filepath.Abs(filepath.Clean(homeDir))
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	if rawPath == "~" {
		return resolvedHome, nil
	}

	relative := strings.TrimPrefix(rawPath, "~"+string(filepath.Separator))
	return filepath.Join(resolvedHome, relative), nil
}

func expandAndClean(rawPath, homeDir string) (string, error) {
	if rawPath == "" {
		return "", fmt.Errorf("path must not be empty")
	}

	expanded, err := expandHome(rawPath, homeDir)
	if err != nil {
		return "", err
	}
	return filepath.Clean(expanded), nil
}
