//go:build darwin && cgo

package process

import "testing"

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
