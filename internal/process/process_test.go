package process

import (
	"testing"
	"time"
)

func TestMatchesMainProcessRequiresExactUserDataArgument(t *testing.T) {
	info := Info{
		PID:            123,
		ExecutablePath: "/Applications/ChatGPT.app/Contents/MacOS/ChatGPT",
		Args:           []string{"ChatGPT", "--user-data-dir=/tmp/profile"},
	}

	if !MatchesMainProcess(info, info.ExecutablePath, "/tmp/profile") {
		t.Fatal("MatchesMainProcess() rejected exact identity")
	}
	if MatchesMainProcess(info, info.ExecutablePath, "/tmp/pro") {
		t.Fatal("MatchesMainProcess() accepted a substring user-data-dir")
	}
	if MatchesMainProcess(info, info.ExecutablePath, "/tmp/other-profile") {
		t.Fatal("MatchesMainProcess() matched a different profile")
	}
}

func TestMatchesMainProcessRequiresExpectedExecutable(t *testing.T) {
	info := Info{
		ExecutablePath: "/Applications/ChatGPT.app/Contents/MacOS/ChatGPT",
		Args:           []string{"--user-data-dir=/tmp/profile"},
	}

	if MatchesMainProcess(info, "/Applications/Other.app/Contents/MacOS/ChatGPT", "/tmp/profile") {
		t.Fatal("MatchesMainProcess() accepted an unexpected executable")
	}
}

func TestSameIdentityRequiresPIDStartTimeAndExecutable(t *testing.T) {
	base := Info{
		PID:            123,
		StartTime:      time.Unix(100, 0),
		ExecutablePath: "/Applications/ChatGPT.app/Contents/MacOS/ChatGPT",
	}
	if !SameIdentity(base, base) {
		t.Fatal("SameIdentity() rejected matching identity")
	}

	changedPID := base
	changedPID.PID++
	if SameIdentity(base, changedPID) {
		t.Fatal("SameIdentity() accepted a different PID")
	}
	changedStart := base
	changedStart.StartTime = time.Unix(101, 0)
	if SameIdentity(base, changedStart) {
		t.Fatal("SameIdentity() accepted a different start time")
	}
	changedExecutable := base
	changedExecutable.ExecutablePath = "/Applications/Other.app/Contents/MacOS/ChatGPT"
	if SameIdentity(base, changedExecutable) {
		t.Fatal("SameIdentity() accepted a different executable")
	}
}
