package config

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeMinimalConfig(t *testing.T) {
	homeDir := t.TempDir()
	resolved, err := Decode([]byte(`{"profiles":{"ninibin":{"proxy":"socks5://127.0.0.1:18081"}}}`), homeDir)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	profile, ok := resolved.Profiles["ninibin"]
	if !ok {
		t.Fatal("expected ninibin profile")
	}
	if profile.Proxy != "socks5://127.0.0.1:18081" {
		t.Fatalf("Proxy = %q", profile.Proxy)
	}
}

func TestDecodeTwoProfiles(t *testing.T) {
	resolved, err := Decode([]byte(`{"profiles":{"ninibin":{},"lucas":{}}}`), t.TempDir())
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(resolved.Profiles) != 2 {
		t.Fatalf("profile count = %d, want 2", len(resolved.Profiles))
	}
}

func TestDefaultRuntimeRoot(t *testing.T) {
	homeDir := t.TempDir()
	resolved, err := Decode([]byte(`{"profiles":{"ninibin":{}}}`), homeDir)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	want := filepath.Join(homeDir, "Library", "Application Support", "CodexProfileRuntime")
	if resolved.RuntimeRoot != want {
		t.Fatalf("RuntimeRoot = %q, want %q", resolved.RuntimeRoot, want)
	}
}

func TestCustomRuntimeRootAndTildeExpansion(t *testing.T) {
	homeDir := t.TempDir()
	resolved, err := Decode([]byte(`{"runtime_root":"~/custom-runtime","profiles":{"ninibin":{}}}`), homeDir)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	want := filepath.Join(homeDir, "custom-runtime")
	if resolved.RuntimeRoot != want {
		t.Fatalf("RuntimeRoot = %q, want %q", resolved.RuntimeRoot, want)
	}
}

func TestProfilePathsAreDerived(t *testing.T) {
	homeDir := t.TempDir()
	resolved, err := Decode([]byte(`{"runtime_root":"/tmp/runtime-root","profiles":{"ninibin":{}}}`), homeDir)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	profile := resolved.Profiles["ninibin"]
	if want := "/tmp/runtime-root/profiles/ninibin/codex-home"; profile.CodexHome != want {
		t.Fatalf("CodexHome = %q, want %q", profile.CodexHome, want)
	}
	if want := "/tmp/runtime-root/profiles/ninibin/chatgpt-user-data"; profile.UserDataDir != want {
		t.Fatalf("UserDataDir = %q, want %q", profile.UserDataDir, want)
	}
}

func TestInvalidProfileIDs(t *testing.T) {
	for _, id := range []string{"", "Upper", "bad/id", "bad space", "../escape", "."} {
		t.Run(id, func(t *testing.T) {
			_, err := Decode([]byte(`{"profiles":{"`+id+`":{}}}`), t.TempDir())
			if err == nil {
				t.Fatalf("Decode() expected error for profile id %q", id)
			}
		})
	}
}

func TestInvalidProxies(t *testing.T) {
	for _, proxy := range []string{
		"",
		"ftp://127.0.0.1:21",
		"socks5://",
		"not a uri",
		" socks5://127.0.0.1:18081",
	} {
		t.Run(proxy, func(t *testing.T) {
			encodedProxy, err := json.Marshal(proxy)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			data := []byte(`{"profiles":{"ninibin":{"proxy":` + string(encodedProxy) + `}}}`)
			if _, err := Decode(data, t.TempDir()); err == nil {
				t.Fatalf("Decode() expected error for proxy %q", proxy)
			}
		})
	}
}

func TestAllowedProxySchemes(t *testing.T) {
	for _, scheme := range []string{"socks5", "socks5h", "http", "https"} {
		t.Run(scheme, func(t *testing.T) {
			data := []byte(`{"profiles":{"ninibin":{"proxy":"` + scheme + `://127.0.0.1:1080"}}}`)
			if _, err := Decode(data, t.TempDir()); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
		})
	}
}

func TestProxyCredentialsAreRejected(t *testing.T) {
	data := []byte(`{"profiles":{"ninibin":{"proxy":"socks5://user:password@127.0.0.1:1080"}}}`)
	if _, err := Decode(data, t.TempDir()); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("Decode() error = %v, want credential rejection", err)
	}
}

func TestUnknownFieldsAreRejected(t *testing.T) {
	for _, field := range []string{
		`{"profiles":{"ninibin":{}},"provider":"openai"}`,
		`{"profiles":{"ninibin":{"env":{}}}}`,
		`{"profiles":{"ninibin":{"codex_home":"/tmp/codex"}}}`,
		`{"profiles":{"ninibin":{"user_data_dir":"/tmp/chatgpt"}}}`,
		`{"profiles":{"ninibin":{"account":"work"}}}`,
		`{"profiles":{"ninibin":{"auth_token":"secret"}}}`,
		`{"profiles":{"ninibin":{"session":"session"}}}`,
	} {
		t.Run(field, func(t *testing.T) {
			if _, err := Decode([]byte(field), t.TempDir()); err == nil {
				t.Fatalf("Decode() expected unknown-field error")
			}
		})
	}
}

func TestProfilesFieldIsRequired(t *testing.T) {
	if _, err := Decode([]byte(`{"runtime_root":"/tmp/runtime-root"}`), t.TempDir()); err == nil {
		t.Fatal("Decode() expected missing profiles error")
	}
}
