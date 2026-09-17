//go:build !darwin || !cgo

package process

import "fmt"

type unsupportedScanner struct{}

// NewScanner keeps non-macOS builds explicit without providing a fake process implementation.
func NewScanner() Scanner {
	return unsupportedScanner{}
}

func (unsupportedScanner) FindMain(_, _ string) (Info, bool, error) {
	return Info{}, false, fmt.Errorf("ChatGPT process verification is supported only on macOS")
}
