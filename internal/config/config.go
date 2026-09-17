package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/Luaphes/codex-profile-runtime/internal/runtime"
)

var profileIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

var allowedProxySchemes = map[string]struct{}{
	"http":    {},
	"https":   {},
	"socks5":  {},
	"socks5h": {},
}

// FileConfig is the intentionally small JSON configuration model for v0.1.
type FileConfig struct {
	RuntimeRoot string                   `json:"runtime_root"`
	ChatGPTApp  string                   `json:"chatgpt_app"`
	Profiles    map[string]ProfileConfig `json:"profiles"`
}

type ProfileConfig struct {
	Proxy *string `json:"proxy"`
}

// ResolvedConfig contains defaults and derived paths ready for later runtime use.
type ResolvedConfig struct {
	RuntimeRoot string
	ChatGPTApp  string
	Profiles    map[string]ResolvedProfile
}

// ResolvedProfile is the v0.1 profile representation after validation and path derivation.
type ResolvedProfile struct {
	ID          string
	Proxy       string
	CodexHome   string
	UserDataDir string
}

// Load reads, strictly decodes, validates, and resolves one config file.
func Load(path, homeDir string) (ResolvedConfig, error) {
	resolvedPath, err := runtime.NormalizeConfigPath(path, homeDir)
	if err != nil {
		return ResolvedConfig{}, fmt.Errorf("config path: %w", err)
	}

	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return ResolvedConfig{}, fmt.Errorf("read config %q: %w", resolvedPath, err)
	}

	resolved, err := Decode(data, homeDir)
	if err != nil {
		return ResolvedConfig{}, fmt.Errorf("config %q: %w", resolvedPath, err)
	}
	return resolved, nil
}

// Decode strictly decodes and resolves JSON config without reading any runtime files.
func Decode(data []byte, homeDir string) (ResolvedConfig, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var raw FileConfig
	if err := decoder.Decode(&raw); err != nil {
		return ResolvedConfig{}, fmt.Errorf("decode: %w", err)
	}

	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return ResolvedConfig{}, fmt.Errorf("decode: config must contain one JSON value")
		}
		return ResolvedConfig{}, fmt.Errorf("decode: %w", err)
	}

	return Resolve(raw, homeDir)
}

// Resolve validates a decoded config and derives all v0.1 profile paths.
func Resolve(raw FileConfig, homeDir string) (ResolvedConfig, error) {
	if raw.Profiles == nil {
		return ResolvedConfig{}, fmt.Errorf("profiles is required")
	}

	resolvedHome, err := resolveHomeDir(homeDir)
	if err != nil {
		return ResolvedConfig{}, err
	}

	runtimeRootRaw := raw.RuntimeRoot
	if runtimeRootRaw == "" {
		runtimeRootRaw = runtime.DefaultRuntimeRoot(resolvedHome)
	}
	if strings.TrimSpace(runtimeRootRaw) == "" {
		return ResolvedConfig{}, fmt.Errorf("runtime_root must not be empty")
	}
	runtimeRoot, err := runtime.NormalizePath(runtimeRootRaw, resolvedHome)
	if err != nil {
		return ResolvedConfig{}, fmt.Errorf("runtime_root: %w", err)
	}

	chatGPTAppRaw := raw.ChatGPTApp
	if chatGPTAppRaw == "" {
		chatGPTAppRaw = runtime.DefaultChatGPTApp
	}
	if strings.TrimSpace(chatGPTAppRaw) == "" {
		return ResolvedConfig{}, fmt.Errorf("chatgpt_app must not be empty")
	}
	chatGPTApp, err := runtime.NormalizePath(chatGPTAppRaw, resolvedHome)
	if err != nil {
		return ResolvedConfig{}, fmt.Errorf("chatgpt_app: %w", err)
	}

	profiles := make(map[string]ResolvedProfile, len(raw.Profiles))
	for id, profile := range raw.Profiles {
		if !profileIDPattern.MatchString(id) {
			return ResolvedConfig{}, fmt.Errorf("profile %q: id must match [a-z0-9][a-z0-9._-]{0,63}", id)
		}

		proxy := ""
		if profile.Proxy != nil {
			if err := validateProxy(*profile.Proxy); err != nil {
				return ResolvedConfig{}, fmt.Errorf("profile %q: proxy: %w", id, err)
			}
			proxy = *profile.Proxy
		}

		paths, err := runtime.DeriveProfilePaths(runtimeRoot, id)
		if err != nil {
			return ResolvedConfig{}, fmt.Errorf("profile %q: derive paths: %w", id, err)
		}
		profiles[id] = ResolvedProfile{
			ID:          id,
			Proxy:       proxy,
			CodexHome:   paths.CodexHome,
			UserDataDir: paths.UserDataDir,
		}
	}

	return ResolvedConfig{
		RuntimeRoot: runtimeRoot,
		ChatGPTApp:  chatGPTApp,
		Profiles:    profiles,
	}, nil
}

func validateProxy(raw string) error {
	if raw == "" {
		return fmt.Errorf("must not be empty")
	}
	if strings.TrimSpace(raw) != raw {
		return fmt.Errorf("must not contain leading or trailing whitespace")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URI: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if _, ok := allowedProxySchemes[scheme]; !ok {
		return fmt.Errorf("scheme %q is not supported", parsed.Scheme)
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return fmt.Errorf("host is required")
	}
	if parsed.User != nil {
		return fmt.Errorf("credentials are not supported")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf("path is not supported")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("query and fragment are not supported")
	}
	if err := validateProxyPort(parsed); err != nil {
		return err
	}

	return nil
}

func validateProxyPort(parsed *url.URL) error {
	port := parsed.Port()
	if port == "" {
		if strings.HasSuffix(parsed.Host, ":") {
			return fmt.Errorf("port must be a decimal integer from 1 to 65535")
		}
		return nil
	}

	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 {
		return fmt.Errorf("port must be a decimal integer from 1 to 65535")
	}
	return nil
}

func resolveHomeDir(homeDir string) (string, error) {
	if homeDir == "" {
		resolved, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return runtime.NormalizePath(resolved, "")
	}
	return runtime.NormalizePath(homeDir, "")
}
