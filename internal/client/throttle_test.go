package client

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestThrottleNilIsNoOp(t *testing.T) {
	var th *Throttle
	// Should not panic.
	th.Wait(100)
}

func TestThrottleZeroRateIsNoOp(t *testing.T) {
	var slept atomic.Int64
	th := &Throttle{
		EventsPerSecond: 0,
		Sleep:           func(d time.Duration) { slept.Add(int64(d)) },
	}
	th.Wait(100)
	if slept.Load() != 0 {
		t.Errorf("expected no sleep with rate=0, got %v", time.Duration(slept.Load()))
	}
}

func TestThrottleNegativeRateIsNoOp(t *testing.T) {
	var slept atomic.Int64
	th := &Throttle{
		EventsPerSecond: -10,
		Sleep:           func(d time.Duration) { slept.Add(int64(d)) },
	}
	th.Wait(100)
	if slept.Load() != 0 {
		t.Errorf("expected no sleep with negative rate, got %v", time.Duration(slept.Load()))
	}
}

func TestThrottleZeroEventsIsNoOp(t *testing.T) {
	var slept atomic.Int64
	th := &Throttle{
		EventsPerSecond: 100,
		Sleep:           func(d time.Duration) { slept.Add(int64(d)) },
	}
	th.Wait(0)
	if slept.Load() != 0 {
		t.Errorf("expected no sleep with 0 events, got %v", time.Duration(slept.Load()))
	}
}

func TestThrottleSleepsProportionalToBatch(t *testing.T) {
	var slept atomic.Int64
	th := &Throttle{
		EventsPerSecond: 100, // 100 evt/sec
		Sleep:           func(d time.Duration) { slept.Add(int64(d)) },
	}
	th.Wait(100) // 100/100 = 1s
	got := time.Duration(slept.Load())
	if got != time.Second {
		t.Errorf("expected 1s sleep, got %v", got)
	}
}

func TestThrottleSubBatchProportional(t *testing.T) {
	var slept atomic.Int64
	th := &Throttle{
		EventsPerSecond: 1000,
		Sleep:           func(d time.Duration) { slept.Add(int64(d)) },
	}
	th.Wait(50) // 50/1000 = 50ms
	got := time.Duration(slept.Load())
	if got != 50*time.Millisecond {
		t.Errorf("expected 50ms sleep, got %v", got)
	}
}

func TestThrottleFractionalRate(t *testing.T) {
	var slept atomic.Int64
	th := &Throttle{
		EventsPerSecond: 0.5, // 1 event every 2 seconds
		Sleep:           func(d time.Duration) { slept.Add(int64(d)) },
	}
	th.Wait(1)
	got := time.Duration(slept.Load())
	if got != 2*time.Second {
		t.Errorf("expected 2s sleep, got %v", got)
	}
}

func TestThrottleDefaultsToTimeSleepWhenInjectorMissing(t *testing.T) {
	// Just verify it doesn't panic. We don't actually want to sleep here.
	th := &Throttle{EventsPerSecond: 1_000_000} // 0 events would sleep ~0 ns
	th.Wait(1)                                  // 1 / 1e6 sec = 1µs — negligible
}
