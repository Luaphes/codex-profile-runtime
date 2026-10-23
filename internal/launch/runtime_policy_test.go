package launch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceGuardFailureNeverLaunchesDesktop(t *testing.T) {
	root := t.TempDir()
	profile := resolvedProfile(t, root, "personal")
	runner := &fakeRunner{err: errors.New("unsupported feature or unwritable config")}
	service := NewService(runner, &fakeScanner{})
	_, err := service.Launch(testApp(t), root, profile)
	if err == nil || !strings.Contains(err.Error(), "ChatGPT was not started") {
		t.Fatalf("expected guard failure, got %v", err)
	}
	if len(runner.calls) != 1 || runner.calls[0].Path == openCommandPath {
		t.Fatalf("desktop launch was attempted: %#v", runner.calls)
	}
}

func TestWorkspaceGuardRequiresBundledCLI(t *testing.T) {
	for _, mode := range []string{"missing", "directory", "not-executable"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			profile := resolvedProfile(t, root, "personal")
			app := testApp(t)
			guard, err := BuildWorkspaceDependencyGuard(app, profile)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(guard.Path); err != nil {
				t.Fatal(err)
			}
			if mode == "directory" {
				if err := os.Mkdir(guard.Path, 0o700); err != nil {
					t.Fatal(err)
				}
			} else if mode == "not-executable" {
				if err := os.WriteFile(guard.Path, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			runner := &fakeRunner{}
			_, err = NewService(runner, &fakeScanner{}).Launch(app, root, profile)
			if err == nil || len(runner.calls) != 0 {
				t.Fatalf("launch was not blocked: %v, %#v", err, runner.calls)
			}
		})
	}
}

func TestWorkspaceGuardIsProfileScopedAndDoesNotRedirectHome(t *testing.T) {
	root, app := t.TempDir(), testApp(t)
	for _, id := range []string{"personal", "work"} {
		profile := resolvedProfile(t, root, id)
		guard, err := BuildWorkspaceDependencyGuard(app, profile)
		if err != nil {
			t.Fatal(err)
		}
		if !equalStrings(guard.Args, []string{"features", "disable", "workspace_dependencies"}) {
			t.Fatalf("unexpected args: %#v", guard.Args)
		}
		env := envMap(mergeEnvironment([]string{"HOME=/real-home", "CODEX_HOME=/wrong-profile"}, guard.Env))
		if env["CODEX_HOME"] != profile.CodexHome || env["HOME"] != "/real-home" {
			t.Fatalf("wrong profile environment: %#v", env)
		}
		if guard.Timeout <= 0 {
			t.Fatal("guard must have a bounded execution time")
		}
	}
}

func TestCommandRunnerTimeout(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("Unix test executable")
	}
	path := filepath.Join(t.TempDir(), "blocked-cli")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec sleep 10\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	err := NewCommandRunner().Run(CommandSpec{Path: path, Timeout: 30 * time.Millisecond})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want timeout", err)
	}
}

func TestWorkspaceGuardRejectsConfigSymlink(t *testing.T) {
	root := t.TempDir()
	profile := resolvedProfile(t, root, "personal")
	paths := preparedPaths(t, root, profile)
	outside := filepath.Join(t.TempDir(), "shared-config.toml")
	want := []byte("model = \"keep-me\"\n")
	if err := os.WriteFile(outside, want, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(paths.CodexHome, "config.toml")); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	_, err := NewService(runner, &fakeScanner{}).Launch(testApp(t), root, profile)
	if err == nil || len(runner.calls) != 0 {
		t.Fatalf("external config was not protected: %v", err)
	}
	got, err := os.ReadFile(outside)
	if err != nil || string(got) != string(want) {
		t.Fatalf("external config changed: %v", err)
	}
}

// Opt-in integration test: only invokes the real bundled CLI's local config
// writer against a temporary profile. It never launches the desktop app.
func TestBundledWorkspaceGuardPreservesUnrelatedConfig(t *testing.T) {
	app := os.Getenv("CPR_TEST_CODEX_APP")
	if app == "" {
		t.Skip("set CPR_TEST_CODEX_APP to test the installed bundled CLI")
	}
	root := t.TempDir()
	for _, id := range []string{"personal", "work"} {
		profile := resolvedProfile(t, root, id)
		paths := preparedPaths(t, root, profile)
		profile.CodexHome = paths.CodexHome
		path := filepath.Join(paths.CodexHome, "config.toml")
		original := "# Preserve this profile's configuration\nmodel = \"gpt-6-astra\"\n\n[features]\nworkspace_dependencies = true\nshell_tool = true\n"
		if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
			t.Fatal(err)
		}
		guard, err := BuildWorkspaceDependencyGuard(app, profile)
		if err != nil {
			t.Fatal(err)
		}
		for attempt := 0; attempt < 2; attempt++ {
			if err := NewCommandRunner().Run(guard); err != nil {
				t.Fatalf("real bundled CLI guard: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want := strings.Replace(original, "workspace_dependencies = true", "workspace_dependencies = false", 1)
			if string(got) != want {
				t.Fatalf("unexpected config changes for %s: %q", id, got)
			}
		}
	}
}
