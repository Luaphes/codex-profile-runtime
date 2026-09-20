package stop

// Signal is the deliberately small signal vocabulary used by safe stop.
type Signal int

const (
	SignalTERM Signal = 15
	SignalKILL Signal = 9
)

// Signaler isolates signal delivery from stop policy and makes it injectable in tests.
type Signaler interface {
	Signal(pid int, signal Signal) error
}
