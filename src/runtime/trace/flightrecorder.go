package trace

import (
	"errors"
	"io"
	"time"
)

// FlightRecorder is Go 1.25's flight recorder, which keeps the last part of
// an execution trace in memory. TinyGo has no execution tracer, so it cannot
// be started, as Start cannot.
type FlightRecorder struct{}

// FlightRecorderConfig is the configuration for NewFlightRecorder.
type FlightRecorderConfig struct {
	MinAge   time.Duration
	MaxBytes uint64
}

// NewFlightRecorder creates a flight recorder that cannot be started.
func NewFlightRecorder(cfg FlightRecorderConfig) *FlightRecorder {
	return &FlightRecorder{}
}

// Start returns an error: there is no execution trace to record.
func (fr *FlightRecorder) Start() error {
	return errors.New("not implemented")
}

// Stop does nothing, as the recorder never started.
func (fr *FlightRecorder) Stop() {}

// Enabled reports false: the recorder never runs.
func (fr *FlightRecorder) Enabled() bool {
	return false
}

// WriteTo returns an error, as there is no trace to write.
func (fr *FlightRecorder) WriteTo(w io.Writer) (n int64, err error) {
	return 0, errors.New("not implemented")
}
