package clock

import "time"

// Clock keeps calendar-dependent behavior deterministic in tests.
type Clock interface {
	Now() time.Time
	Sleep(time.Duration)
}

// Real uses the process wall clock.
type Real struct{}

func (Real) Now() time.Time            { return time.Now() }
func (Real) Sleep(delay time.Duration) { time.Sleep(delay) }

// Fixed is a non-sleeping test clock.
type Fixed struct {
	Time time.Time
}

func (f Fixed) Now() time.Time    { return f.Time }
func (Fixed) Sleep(time.Duration) {}
