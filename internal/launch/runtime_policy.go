package launch

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Luaphes/codex-profile-runtime/internal/config"
)

// BuildWorkspaceDependencyGuard uses the app's own config writer, rather than
// rewriting TOML or changing HOME to redirect the installer's shared cache.
// Only the selected profile's workspace_dependencies feature is changed.
func BuildWorkspaceDependencyGuard(appPath string, profile config.ResolvedProfile) (CommandSpec, error) {
	if profile.CodexHome == "" {
		return CommandSpec{}, fmt.Errorf("profile %q has no Codex home", profile.ID)
	}
	configPath := filepath.Join(profile.CodexHome, "config.toml")
	if info, err := os.Lstat(configPath); err == nil {
		if !info.Mode().IsRegular() {
			return CommandSpec{}, fmt.Errorf("cannot guard shared runtime downloads: profile config must be a regular file, not a symlink or directory: %q", configPath)
		}
	} else if !os.IsNotExist(err) {
		return CommandSpec{}, fmt.Errorf("inspect profile config: %w", err)
	}
	cli := filepath.Join(appPath, "Contents", "Resources", "codex-cli", "CodexCLI.app", "Contents", "MacOS", "codex")
	info, err := os.Stat(cli)
	if err != nil {
		return CommandSpec{}, fmt.Errorf("cannot guard shared runtime downloads: bundled Codex CLI unavailable at %q: %w", cli, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return CommandSpec{}, fmt.Errorf("cannot guard shared runtime downloads: bundled Codex CLI is not executable at %q", cli)
	}
	return CommandSpec{
		Path:    cli,
		Args:    []string{"features", "disable", "workspace_dependencies"},
		Env:     []string{"CODEX_HOME=" + profile.CodexHome},
		Timeout: 15 * time.Second,
	}, nil
}
