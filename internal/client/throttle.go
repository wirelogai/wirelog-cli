package client

import "time"

// Throttle is a simple events-per-second pacer for the CLI's stdin mode.
//
// Unlike the SDK rate limiter (token bucket + rolling windows), this is a
// post-hoc throttle: after a batch is sent the caller invokes Wait(N), which
// sleeps long enough that the running average stays at or below
// EventsPerSecond. Bursts inside a single batch are not throttled — the
// CLI's natural batching (default 100 events/batch, sequential HTTP) is the
// only inter-batch pacing layer.
//
// EventsPerSecond ≤ 0 disables throttling. Sleep is injectable so tests can
// observe how long the throttle would have slept without actually waiting.
type Throttle struct {
	EventsPerSecond float64
	Sleep           func(time.Duration)
}

// Wait blocks long enough to keep the average send rate ≤ EventsPerSecond.
// Safe to call with a nil receiver, with non-positive rates, or with zero
// events — all are no-ops.
func (t *Throttle) Wait(events int) {
	if t == nil || t.EventsPerSecond <= 0 || events <= 0 {
		return
	}
	d := time.Duration(float64(events) / t.EventsPerSecond * float64(time.Second))
	if d <= 0 {
		return
	}
	sleep := t.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	sleep(d)
}
