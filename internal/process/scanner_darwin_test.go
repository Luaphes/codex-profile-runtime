//go:build darwin && cgo

package process

import (
	"testing"
	"time"
)

func TestPIDListResultUsesPIDCountNotByteCount(t *testing.T) {
	count, retry, err := pidListResult(8, 32)
	if err != nil {
		t.Fatalf("pidListResult() error = %v", err)
	}
	if count != 8 || retry {
		t.Fatalf("pidListResult() = (%d, %t), want (8, false)", count, retry)
	}
}

func TestPIDListResultRequestsRetryWhenCountExceedsCapacity(t *testing.T) {
	count, retry, err := pidListResult(9, 8)
	if err != nil {
		t.Fatalf("pidListResult() error = %v", err)
	}
	if count != 9 || !retry {
		t.Fatalf("pidListResult() = (%d, %t), want (9, true)", count, retry)
	}
}

func TestProcessStartTimeUsesDarwinSecondsAndMicroseconds(t *testing.T) {
	got := processStartTime(1_758_200_400, 123_456)
	if got.Unix() != 1_758_200_400 || got.Nanosecond() != 123_456_000 {
		t.Fatalf("processStartTime() = %v, want expected epoch value", got)
	}
	if got.Location() != time.Local {
		t.Fatalf("processStartTime() location = %v, want local location", got.Location())
	}
}
