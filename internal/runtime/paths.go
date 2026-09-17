package runtime

import (
	"fmt"
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

// NormalizePath performs lexical normalization and absolute path resolution.
// It intentionally does not resolve symlinks, because the path may not exist yet.
func NormalizePath(rawPath, homeDir string) (string, error) {
	if rawPath == "" {
		return "", fmt.Errorf("path must not be empty")
	}

	expanded, err := expandHome(rawPath, homeDir)
	if err != nil {
		return "", err
	}

	cleaned := filepath.Clean(expanded)
	if !filepath.IsAbs(cleaned) {
		absolute, err := filepath.Abs(cleaned)
		if err != nil {
			return "", fmt.Errorf("resolve absolute path: %w", err)
		}
		cleaned = absolute
	}

	return filepath.Clean(cleaned), nil
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
