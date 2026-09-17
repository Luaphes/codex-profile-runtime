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

func TestRunValidateRejectsUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"launch", "ninibin"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("run() unexpectedly accepted an unimplemented command")
	}
	if !strings.Contains(stderr.String(), "usage: cpr validate") {
		t.Fatalf("stderr = %q, want validate usage", stderr.String())
	}
}
