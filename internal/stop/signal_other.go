//go:build !darwin

package stop

import "fmt"

type unsupportedSignaler struct{}

// NewSignaler keeps non-macOS builds explicit without sending signals.
func NewSignaler() Signaler {
	return unsupportedSignaler{}
}

func (unsupportedSignaler) Signal(int, Signal) error {
	return fmt.Errorf("ChatGPT process signaling is supported only on macOS")
}
