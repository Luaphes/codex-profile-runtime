package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Luaphes/codex-profile-runtime/internal/config"
	"github.com/Luaphes/codex-profile-runtime/internal/launch"
	"github.com/Luaphes/codex-profile-runtime/internal/process"
	"github.com/Luaphes/codex-profile-runtime/internal/runtime"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}

	switch args[0] {
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	case "launch":
		return runLaunch(args[1:], stdout, stderr)
	default:
		printUsage(stderr)
		return 2
	}
}

func runValidate(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "config file path")
	if err := flags.Parse(args); err != nil {
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

func runLaunch(args []string, stdout, stderr io.Writer) int {
	profileID, configPath, err := parseLaunchArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "cpr launch: %v\n", err)
		return 2
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "cpr launch: resolve home directory: %v\n", err)
		return 1
	}

	if configPath == "" {
		configPath = runtime.DefaultConfigPath(homeDir)
	}
	resolved, err := config.Load(configPath, homeDir)
	if err != nil {
		fmt.Fprintf(stderr, "cpr launch: %v\n", err)
		return 1
	}

	profile, ok := resolved.Profiles[profileID]
	if !ok {
		fmt.Fprintf(stderr, "cpr launch: profile %q not found\n", profileID)
		return 1
	}

	service := launch.NewService(launch.NewCommandRunner(), process.NewScanner())
	info, err := service.Launch(resolved.ChatGPTApp, resolved.RuntimeRoot, profile)
	if err != nil {
		if errors.Is(err, launch.ErrAlreadyRunning) {
			fmt.Fprintf(stderr, "cpr launch: profile %q is already running\n", profileID)
			return 1
		}
		fmt.Fprintf(stderr, "cpr launch: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "launched profile %q (pid %d)\n", profileID, info.PID)
	return 0
}

func parseLaunchArgs(args []string) (string, string, error) {
	var profileID string
	var configPath string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--config":
			if i+1 >= len(args) || args[i+1] == "" {
				return "", "", fmt.Errorf("--config requires a path")
			}
			i++
			configPath = args[i]
		case strings.HasPrefix(arg, "--config="):
			configPath = strings.TrimPrefix(arg, "--config=")
			if configPath == "" {
				return "", "", fmt.Errorf("--config requires a path")
			}
		case strings.HasPrefix(arg, "-"):
			return "", "", fmt.Errorf("unknown option %q", arg)
		case profileID == "":
			profileID = arg
		default:
			return "", "", fmt.Errorf("unexpected positional argument %q", arg)
		}
	}

	if profileID == "" {
		return "", "", fmt.Errorf("profile is required")
	}
	return profileID, configPath, nil
}

func printUsage(stderr io.Writer) {
	fmt.Fprintln(stderr, "usage: cpr validate [--config <path>]")
	fmt.Fprintln(stderr, "       cpr launch <profile> [--config <path>]")
}
