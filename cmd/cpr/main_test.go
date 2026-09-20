package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunValidateWithConfigOverride(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	config := []byte(`{"profiles":{"personal":{"proxy":"socks5://127.0.0.1:1080"}}}`)
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"validate", "--config", configPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
	}
	if got, want := stdout.String(), "configuration valid: 1 profile(s)\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestRunRejectsStopMissingProfile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"stop"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("run() unexpectedly accepted a missing stop profile")
	}
	if !strings.Contains(stderr.String(), "profile is required") {
		t.Fatalf("stderr = %q, want missing profile error", stderr.String())
	}
}

func TestParseLaunchArgsSupportsConfigBeforeOrAfterProfile(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "profile then config", args: []string{"personal", "--config", "./test-config.json"}},
		{name: "config then profile", args: []string{"--config", "./test-config.json", "personal"}},
		{name: "config equals", args: []string{"personal", "--config=./test-config.json"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile, configPath, err := parseLaunchArgs(test.args)
			if err != nil {
				t.Fatalf("parseLaunchArgs() error = %v", err)
			}
			if profile != "personal" || configPath != "./test-config.json" {
				t.Fatalf("parseLaunchArgs() = (%q, %q), want (personal, ./test-config.json)", profile, configPath)
			}
		})
	}
}

func TestParseLaunchArgsRejectsMissingProfile(t *testing.T) {
	if _, _, err := parseLaunchArgs(nil); err == nil {
		t.Fatal("parseLaunchArgs() unexpectedly accepted missing profile")
	}
}

func TestParseListArgsSupportsJSONAndConfig(t *testing.T) {
	jsonOutput, configPath, err := parseListArgs([]string{"--json", "--config", "./test-config.json"})
	if err != nil {
		t.Fatalf("parseListArgs() error = %v", err)
	}
	if !jsonOutput || configPath != "./test-config.json" {
		t.Fatalf("parseListArgs() = (%t, %q), want (true, ./test-config.json)", jsonOutput, configPath)
	}
}

func TestParseListArgsRejectsUnexpectedArguments(t *testing.T) {
	if _, _, err := parseListArgs([]string{"profile"}); err == nil {
		t.Fatal("parseListArgs() unexpectedly accepted positional argument")
	}
}

func TestParseStopArgsSupportsForceAndConfig(t *testing.T) {
	profile, force, configPath, err := parseStopArgs([]string{"--force", "personal", "--config=./test-config.json"})
	if err != nil {
		t.Fatalf("parseStopArgs() error = %v", err)
	}
	if profile != "personal" || !force || configPath != "./test-config.json" {
		t.Fatalf("parseStopArgs() = (%q, %t, %q), want (personal, true, ./test-config.json)", profile, force, configPath)
	}
}

func TestParseStopArgsRejectsUnknownOption(t *testing.T) {
	if _, _, _, err := parseStopArgs([]string{"personal", "--kill-all"}); err == nil {
		t.Fatal("parseStopArgs() unexpectedly accepted unknown option")
	}
}
