//go:build darwin

package stop

import (
	"fmt"
	"syscall"
)

type osSignaler struct{}

// NewSignaler returns the production Darwin signal sender.
func NewSignaler() Signaler {
	return osSignaler{}
}

func (osSignaler) Signal(pid int, signal Signal) error {
	if pid <= 0 {
		return fmt.Errorf("invalid process PID %d", pid)
	}
	if signal != SignalTERM && signal != SignalKILL {
		return fmt.Errorf("unsupported signal %d", signal)
	}
	if err := syscall.Kill(pid, syscall.Signal(signal)); err != nil {
		return fmt.Errorf("send signal %d to pid %d: %w", signal, pid, err)
	}
	return nil
}
