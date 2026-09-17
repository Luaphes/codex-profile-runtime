package runtime

import (
	"os"
	"path/filepath"
	"strings"
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

func TestNormalizePathRejectsRelativeRuntimePath(t *testing.T) {
	if _, err := NormalizePath("relative/runtime-root", t.TempDir()); err == nil {
		t.Fatal("NormalizePath() unexpectedly accepted a relative path")
	}
}

func TestNormalizeConfigPathAllowsRelativeConfigPath(t *testing.T) {
	got, err := NormalizeConfigPath("relative/config.json", t.TempDir())
	if err != nil {
		t.Fatalf("NormalizeConfigPath() error = %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("NormalizeConfigPath() = %q, want absolute path", got)
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

func TestPrepareProfilePathsCreatesOnlyCurrentProfileDirectories(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runtime-root")
	expected, err := DeriveProfilePaths(root, "ninibin")
	if err != nil {
		t.Fatalf("DeriveProfilePaths() error = %v", err)
	}

	got, err := PrepareProfilePaths(root, "ninibin", expected)
	if err != nil {
		t.Fatalf("PrepareProfilePaths() error = %v", err)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
	}
	want, err := DeriveProfilePaths(realRoot, "ninibin")
	if err != nil {
		t.Fatalf("DeriveProfilePaths(real root) error = %v", err)
	}
	if got != want {
		t.Fatalf("PrepareProfilePaths() = %#v, want %#v", got, want)
	}
	for _, path := range []string{got.CodexHome, got.UserDataDir} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("os.Stat(%q) error = %v", path, err)
		}
		if !info.IsDir() {
			t.Fatalf("%q is not a directory", path)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%q permissions = %o, want no group/other permissions", path, info.Mode().Perm())
		}
	}
	if _, err := os.Stat(filepath.Join(root, "profiles", "other")); !os.IsNotExist(err) {
		t.Fatalf("unexpected other profile directory: err = %v", err)
	}
}

func TestPrepareProfilePathsRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	expected, err := DeriveProfilePaths(root, "ninibin")
	if err != nil {
		t.Fatalf("DeriveProfilePaths() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(expected.CodexHome), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, expected.CodexHome); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}

	_, err = PrepareProfilePaths(root, "ninibin", expected)
	if err == nil || !strings.Contains(err.Error(), "escapes runtime root") {
		t.Fatalf("PrepareProfilePaths() error = %v, want symlink escape error", err)
	}
}

func TestPrepareProfilePathsRejectsInternalProfileAliases(t *testing.T) {
	tests := []struct {
		name string
		link func(t *testing.T, expected, other ProfilePaths)
	}{
		{
			name: "codex home alias",
			link: func(t *testing.T, expected, other ProfilePaths) {
				if err := os.MkdirAll(other.CodexHome, 0o700); err != nil {
					t.Fatalf("MkdirAll() error = %v", err)
				}
				if err := os.MkdirAll(filepath.Dir(expected.CodexHome), 0o700); err != nil {
					t.Fatalf("MkdirAll() error = %v", err)
				}
				if err := os.Symlink(other.CodexHome, expected.CodexHome); err != nil {
					t.Fatalf("os.Symlink() error = %v", err)
				}
			},
		},
		{
			name: "profile root alias",
			link: func(t *testing.T, expected, other ProfilePaths) {
				profileRoot := filepath.Dir(expected.CodexHome)
				otherProfileRoot := filepath.Dir(other.CodexHome)
				if err := os.MkdirAll(otherProfileRoot, 0o700); err != nil {
					t.Fatalf("MkdirAll() error = %v", err)
				}
				if err := os.MkdirAll(filepath.Dir(profileRoot), 0o700); err != nil {
					t.Fatalf("MkdirAll() error = %v", err)
				}
				if err := os.Symlink(otherProfileRoot, profileRoot); err != nil {
					t.Fatalf("os.Symlink() error = %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			expected, err := DeriveProfilePaths(root, "ninibin")
			if err != nil {
				t.Fatalf("DeriveProfilePaths() error = %v", err)
			}
			other, err := DeriveProfilePaths(root, "lucas")
			if err != nil {
				t.Fatalf("DeriveProfilePaths() error = %v", err)
			}
			test.link(t, expected, other)

			_, err = PrepareProfilePaths(root, "ninibin", expected)
			if err == nil || !strings.Contains(err.Error(), "must not be a symlink") {
				t.Fatalf("PrepareProfilePaths() error = %v, want internal alias rejection", err)
			}
		})
	}
}

func TestPrepareProfilePathsTightensExistingDirectoryPermissions(t *testing.T) {
	root := t.TempDir()
	expected, err := DeriveProfilePaths(root, "ninibin")
	if err != nil {
		t.Fatalf("DeriveProfilePaths() error = %v", err)
	}
	if err := os.MkdirAll(expected.CodexHome, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.MkdirAll(expected.UserDataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatalf("Chmod(root) error = %v", err)
	}
	for _, path := range []string{
		filepath.Join(root, "profiles"),
		filepath.Dir(expected.CodexHome),
		expected.CodexHome,
		expected.UserDataDir,
	} {
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatalf("Chmod(%q) error = %v", path, err)
		}
	}

	got, err := PrepareProfilePaths(root, "ninibin", expected)
	if err != nil {
		t.Fatalf("PrepareProfilePaths() error = %v", err)
	}
	for _, path := range []string{root, filepath.Dir(got.CodexHome), got.CodexHome, got.UserDataDir} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("os.Stat(%q) error = %v", path, err)
		}
		if gotPerms := info.Mode().Perm(); gotPerms != 0o700 {
			t.Fatalf("%q permissions = %o, want 700", path, gotPerms)
		}
	}
}

func TestResolveProfilePathsDoesNotCreateMissingRuntimeDirectories(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing-runtime-root")
	expected, err := DeriveProfilePaths(root, "ninibin")
	if err != nil {
		t.Fatalf("DeriveProfilePaths() error = %v", err)
	}

	got, err := ResolveProfilePaths(root, "ninibin", expected)
	if err != nil {
		t.Fatalf("ResolveProfilePaths() error = %v", err)
	}
	if got != expected {
		t.Fatalf("ResolveProfilePaths() = %#v, want %#v", got, expected)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("ResolveProfilePaths() created runtime root: err = %v", err)
	}
}

func TestResolveProfilePathsRejectsInternalSymlinkAlias(t *testing.T) {
	root := t.TempDir()
	expected, err := DeriveProfilePaths(root, "ninibin")
	if err != nil {
		t.Fatalf("DeriveProfilePaths() error = %v", err)
	}
	other, err := DeriveProfilePaths(root, "lucas")
	if err != nil {
		t.Fatalf("DeriveProfilePaths() error = %v", err)
	}
	if err := os.MkdirAll(other.CodexHome, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(expected.CodexHome), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.Symlink(other.CodexHome, expected.CodexHome); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}

	_, err = ResolveProfilePaths(root, "ninibin", expected)
	if err == nil || !strings.Contains(err.Error(), "must not be a symlink") {
		t.Fatalf("ResolveProfilePaths() error = %v, want internal alias rejection", err)
	}
}
