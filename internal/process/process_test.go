package process

import "testing"

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
