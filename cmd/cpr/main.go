package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Luaphes/codex-profile-runtime/internal/config"
	"github.com/Luaphes/codex-profile-runtime/internal/runtime"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "validate" {
		fmt.Fprintln(stderr, "usage: cpr validate [--config <path>]")
		return 2
	}

	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "config file path")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "cpr validate: unexpected positional arguments")
		return 2
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "cpr validate: resolve home directory: %v\n", err)
		return 1
	}

	path := *configPath
	if path == "" {
		path = runtime.DefaultConfigPath(homeDir)
	}
	resolved, err := config.Load(path, homeDir)
	if err != nil {
		fmt.Fprintf(stderr, "cpr validate: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "configuration valid: %d profile(s)\n", len(resolved.Profiles))
	return 0
}
