package list

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/Luaphes/codex-profile-runtime/internal/config"
	"github.com/Luaphes/codex-profile-runtime/internal/launch"
	"github.com/Luaphes/codex-profile-runtime/internal/process"
	"github.com/Luaphes/codex-profile-runtime/internal/runtime"
)

// Instance is the public v0.1 representation of one confidently identified profile.
type Instance struct {
	Profile     string `json:"profile"`
	PID         int    `json:"pid"`
	StartTime   string `json:"start_time"`
	UserDataDir string `json:"user_data_dir"`
	CodexHome   string `json:"codex_home"`
	Proxy       string `json:"proxy"`
}

// List takes one process snapshot and maps exact process identities back to configured profiles.
func List(resolved config.ResolvedConfig, snapshotter process.Snapshotter) ([]Instance, error) {
	if snapshotter == nil {
		return nil, fmt.Errorf("process snapshotter is not configured")
	}

	executablePath, err := launch.ValidateApp(resolved.ChatGPTApp)
	if err != nil {
		return nil, err
	}
	snapshot, err := snapshotter.Snapshot(executablePath)
	if err != nil {
		return nil, fmt.Errorf("scan ChatGPT processes: %w", err)
	}

	profileIDs := make([]string, 0, len(resolved.Profiles))
	for profileID := range resolved.Profiles {
		profileIDs = append(profileIDs, profileID)
	}
	sort.Strings(profileIDs)

	instances := make([]Instance, 0)
	for _, profileID := range profileIDs {
		profile := resolved.Profiles[profileID]
		paths, err := runtime.ResolveProfilePaths(resolved.RuntimeRoot, profileID, runtime.ProfilePaths{
			CodexHome:   profile.CodexHome,
			UserDataDir: profile.UserDataDir,
		})
		if err != nil {
			return nil, fmt.Errorf("profile %q: resolve runtime identity: %w", profileID, err)
		}

		matches := make([]process.Info, 0, 1)
		for _, candidate := range snapshot {
			if process.MatchesMainProcess(candidate, executablePath, paths.UserDataDir) {
				matches = append(matches, candidate)
			}
		}
		if len(matches) > 1 {
			return nil, fmt.Errorf("profile %q matched multiple ChatGPT main processes", profileID)
		}
		if len(matches) == 0 {
			continue
		}
		if matches[0].StartTime.IsZero() {
			return nil, fmt.Errorf("profile %q matched process without start time", profileID)
		}

		instances = append(instances, Instance{
			Profile:     profileID,
			PID:         matches[0].PID,
			StartTime:   matches[0].StartTime.Format(time.RFC3339),
			UserDataDir: paths.UserDataDir,
			CodexHome:   paths.CodexHome,
			Proxy:       profile.Proxy,
		})
	}
	return instances, nil
}

// WriteHuman writes a stable, concise human-readable list.
func WriteHuman(w io.Writer, instances []Instance) error {
	if len(instances) == 0 {
		_, err := fmt.Fprintln(w, "no running profiles")
		return err
	}
	if _, err := fmt.Fprintln(w, "PROFILE\tPID\tSTARTED\tPROXY"); err != nil {
		return err
	}
	for _, instance := range instances {
		proxy := instance.Proxy
		if proxy == "" {
			proxy = "-"
		}
		if _, err := fmt.Fprintf(w, "%s\t%d\t%s\t%s\n", instance.Profile, instance.PID, instance.StartTime, proxy); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "  user_data_dir: %s\n  codex_home: %s\n", instance.UserDataDir, instance.CodexHome); err != nil {
			return err
		}
	}
	return nil
}

// WriteJSON writes the stable machine-readable v0.1 envelope.
func WriteJSON(w io.Writer, instances []Instance) error {
	if instances == nil {
		instances = []Instance{}
	}
	payload := struct {
		Profiles []Instance `json:"profiles"`
	}{Profiles: instances}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}
