package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetryOn5xxThenSucceed(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		n := attempts.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"transient"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"accepted":1}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "sk_test", "0.0.0", 5*time.Second)
	resp, err := c.Track(context.Background(), []TrackEvent{{EventType: "t"}})
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if resp.Accepted != 1 {
		t.Errorf("expected accepted=1, got %d", resp.Accepted)
	}
	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts (2 5xx + 1 success), got %d", attempts.Load())
	}
}

func TestRetryOn429HonoursRetryAfter(t *testing.T) {
	var attempts atomic.Int32
	var firstAt time.Time
	var secondAt time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		n := attempts.Add(1)
		if n == 1 {
			firstAt = time.Now()
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"slow"}`))
			return
		}
		secondAt = time.Now()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"accepted":1}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "sk_test", "0.0.0", 5*time.Second)
	_, err := c.Track(context.Background(), []TrackEvent{{EventType: "t"}})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	delta := secondAt.Sub(firstAt)
	if delta < 900*time.Millisecond {
		t.Errorf("expected ~1s gap honouring Retry-After, got %v", delta)
	}
}

func TestNoRetryOn4xx(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "sk_test", "0.0.0", 5*time.Second)
	_, err := c.Track(context.Background(), []TrackEvent{{EventType: "t"}})
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", apiErr.StatusCode)
	}
	if attempts.Load() != 1 {
		t.Errorf("expected 1 attempt (no retry on 4xx), got %d", attempts.Load())
	}
}

func TestParseRetryAfterSeconds(t *testing.T) {
	got := parseRetryAfter("5", time.Unix(0, 0))
	if got != 5*time.Second {
		t.Errorf("expected 5s, got %v", got)
	}
}

func TestParseRetryAfterEmpty(t *testing.T) {
	if got := parseRetryAfter("", time.Unix(0, 0)); got != 0 {
		t.Errorf("expected 0, got %v", got)
	}
	if got := parseRetryAfter("   ", time.Unix(0, 0)); got != 0 {
		t.Errorf("expected 0 for whitespace, got %v", got)
	}
}

func TestParseRetryAfterNegative(t *testing.T) {
	if got := parseRetryAfter("-5", time.Unix(0, 0)); got != 0 {
		t.Errorf("expected 0 for negative, got %v", got)
	}
	if got := parseRetryAfter("0", time.Unix(0, 0)); got != 0 {
		t.Errorf("expected 0 for zero, got %v", got)
	}
}

func TestParseRetryAfterHTTPDate(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	future := now.Add(45 * time.Second)
	header := future.Format(http.TimeFormat)
	got := parseRetryAfter(header, now)
	if got < 44*time.Second || got > 46*time.Second {
		t.Errorf("expected ~45s, got %v", got)
	}
}

func TestParseRetryAfterPastHTTPDate(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	past := now.Add(-45 * time.Second)
	header := past.Format(http.TimeFormat)
	if got := parseRetryAfter(header, now); got != 0 {
		t.Errorf("expected 0 for past date, got %v", got)
	}
}

func TestParseRetryAfterGarbage(t *testing.T) {
	if got := parseRetryAfter("not a date", time.Unix(0, 0)); got != 0 {
		t.Errorf("expected 0 for garbage, got %v", got)
	}
}

func TestRetryDelayCapsAtMax(t *testing.T) {
	// Server says wait an hour. We should cap at 30s.
	got := retryDelay(0, "3600")
	if got != maxRetryDelay {
		t.Errorf("expected delay capped at %v, got %v", maxRetryDelay, got)
	}
}

func TestRetryDelayBackoffCapsAtMax(t *testing.T) {
	// 2^20 * 500ms is way over 30s; should still cap.
	got := retryDelay(20, "")
	if got != maxRetryDelay {
		t.Errorf("expected backoff capped at %v, got %v", maxRetryDelay, got)
	}
}

func TestRetryDelayHonoursServerValue(t *testing.T) {
	got := retryDelay(0, "5")
	if got != 5*time.Second {
		t.Errorf("expected 5s, got %v", got)
	}
}

func TestRetryDelayFallsBackToBackoffOnNegative(t *testing.T) {
	// Negative server value should be ignored; fall back to exponential backoff.
	got := retryDelay(0, "-5")
	expected := baseRetryDelay // 2^0 * baseRetryDelay = baseRetryDelay
	if got != expected {
		t.Errorf("expected %v, got %v", expected, got)
	}
}

func TestIsRetryableStatus(t *testing.T) {
	cases := []struct {
		status int
		want   bool
	}{
		{200, false},
		{301, false},
		{400, false},
		{401, false},
		{403, false},
		{404, false},
		{429, true},
		{500, true},
		{502, true},
		{503, true},
		{504, true},
		{599, true},
		{600, false},
	}
	for _, c := range cases {
		if got := isRetryableStatus(c.status); got != c.want {
			t.Errorf("isRetryableStatus(%d) = %v, want %v", c.status, got, c.want)
		}
	}
}
