//go:build !darwin || !cgo

package process

import "fmt"

type unsupportedScanner struct{}

// NewScanner keeps non-macOS builds explicit without providing a fake process implementation.
func NewScanner() Scanner {
	return unsupportedScanner{}
}

// NewSnapshotter keeps non-macOS builds explicit without providing a fake snapshot.
func NewSnapshotter() Snapshotter {
	return unsupportedScanner{}
}

func (unsupportedScanner) FindMain(_, _ string) (Info, bool, error) {
	return Info{}, false, fmt.Errorf("ChatGPT process verification is supported only on macOS")
}

func (unsupportedScanner) Snapshot(string) ([]Info, error) {
	return nil, fmt.Errorf("ChatGPT process snapshot is supported only on macOS")
}
