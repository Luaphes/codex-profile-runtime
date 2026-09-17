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
	config := []byte(`{"profiles":{"ninibin":{"proxy":"socks5://127.0.0.1:18081"}}}`)
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

func TestRunRejectsUnimplementedCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"stop", "ninibin"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("run() unexpectedly accepted an unimplemented command")
	}
	if !strings.Contains(stderr.String(), "usage: cpr validate") {
		t.Fatalf("stderr = %q, want validate usage", stderr.String())
	}
}

func TestParseLaunchArgsSupportsConfigBeforeOrAfterProfile(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "profile then config", args: []string{"ninibin", "--config", "./test-config.json"}},
		{name: "config then profile", args: []string{"--config", "./test-config.json", "ninibin"}},
		{name: "config equals", args: []string{"ninibin", "--config=./test-config.json"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile, configPath, err := parseLaunchArgs(test.args)
			if err != nil {
				t.Fatalf("parseLaunchArgs() error = %v", err)
			}
			if profile != "ninibin" || configPath != "./test-config.json" {
				t.Fatalf("parseLaunchArgs() = (%q, %q), want (ninibin, ./test-config.json)", profile, configPath)
			}
		})
	}
}

func TestParseLaunchArgsRejectsMissingProfile(t *testing.T) {
	if _, _, err := parseLaunchArgs(nil); err == nil {
		t.Fatal("parseLaunchArgs() unexpectedly accepted missing profile")
	}
}
