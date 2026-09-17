package runtime

import (
	"path/filepath"
	"testing"
)

func TestNormalizePathDoesNotRequireExistingDirectory(t *testing.T) {
	homeDir := t.TempDir()
	rawPath := "~/profiles/ninibin/codex-home"

	got, err := NormalizePath(rawPath, homeDir)
	if err != nil {
		t.Fatalf("NormalizePath() error = %v", err)
	}

	want := filepath.Join(homeDir, "profiles", "ninibin", "codex-home")
	if got != want {
		t.Fatalf("NormalizePath() = %q, want %q", got, want)
	}
}

func TestDefaultConfigPath(t *testing.T) {
	homeDir := "/Users/example"
	want := filepath.Join(homeDir, "Library", "Application Support", "CodexProfileRuntime", "config.json")

	if got := DefaultConfigPath(homeDir); got != want {
		t.Fatalf("DefaultConfigPath() = %q, want %q", got, want)
	}
}

func TestDeriveProfilePaths(t *testing.T) {
	runtimeRoot := "/tmp/CodexProfileRuntime"
	got, err := DeriveProfilePaths(runtimeRoot, "ninibin")
	if err != nil {
		t.Fatalf("DeriveProfilePaths() error = %v", err)
	}

	wantCodex := filepath.Join(runtimeRoot, "profiles", "ninibin", "codex-home")
	wantUserData := filepath.Join(runtimeRoot, "profiles", "ninibin", "chatgpt-user-data")
	if got.CodexHome != wantCodex {
		t.Fatalf("CodexHome = %q, want %q", got.CodexHome, wantCodex)
	}
	if got.UserDataDir != wantUserData {
		t.Fatalf("UserDataDir = %q, want %q", got.UserDataDir, wantUserData)
	}
}
