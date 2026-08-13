package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wirelogai/wirelog-cli/internal/client"
)

func TestDashboardQueryCacheCoalescesInflightQueries(t *testing.T) {
	base := &blockingQueryClient{
		started: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
	cache := newDashboardQueryCache()
	cached := &cachedQueryClient{base: base, cache: cache, ttl: time.Minute}
	results := make(chan *client.QueryJSONResponse, 2)
	errs := make(chan error, 2)

	go queryInTest(cached, results, errs)
	<-base.started
	go queryInTest(cached, results, errs)
	close(base.release)

	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("query: %v", err)
		}
		if result := <-results; result == nil {
			t.Fatal("query result is nil")
		}
	}
	if got := base.calls.Load(); got != 1 {
		t.Fatalf("base calls = %d, want 1", got)
	}
}

func TestDashboardQueryCacheKeepsSharedFlightAliveWhenCallerCancels(t *testing.T) {
	base := &blockingQueryClient{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	cached := &cachedQueryClient{base: base, cache: newDashboardQueryCache(), ttl: time.Minute}
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstErr := make(chan error, 1)
	secondResult := make(chan *client.QueryJSONResponse, 1)
	secondErr := make(chan error, 1)

	go func() {
		_, err := cached.QueryJSON(firstCtx, "signup | count", 1000, 0)
		firstErr <- err
	}()
	<-base.started
	go queryInTest(cached, secondResult, secondErr)
	cancelFirst()
	if err := <-firstErr; err == nil {
		t.Fatal("canceled caller returned no error")
	}
	close(base.release)
	if err := <-secondErr; err != nil {
		t.Fatalf("second query: %v", err)
	}
	if result := <-secondResult; result == nil {
		t.Fatal("second query result is nil")
	}
	if got := base.calls.Load(); got != 1 {
		t.Fatalf("base calls = %d, want 1", got)
	}
}

func TestDashboardQueryCacheHonorsForce(t *testing.T) {
	base := &fakeQueryClient{}
	cache := newDashboardQueryCache()
	cached := &cachedQueryClient{base: base, cache: cache, ttl: time.Minute}

	for range 2 {
		_, err := cached.QueryJSON(context.Background(), "signup | count", 1000, 0)
		if err != nil {
			t.Fatalf("cached query: %v", err)
		}
	}
	forced := &cachedQueryClient{base: base, cache: cache, ttl: time.Minute, force: true, refreshID: "refresh-1"}
	for range 2 {
		_, err := forced.QueryJSON(context.Background(), "signup | count", 1000, 0)
		if err != nil {
			t.Fatalf("forced query: %v", err)
		}
	}
	if got := base.calls.Load(); got != 2 {
		t.Fatalf("base calls = %d, want 2", got)
	}
}

func TestDashboardQueryCacheTTLTracksRefresh(t *testing.T) {
	tests := []struct {
		refresh string
		want    time.Duration
	}{
		{refresh: "", want: defaultDashboardQueryCacheTTL},
		{refresh: "5s", want: 5 * time.Second},
		{refresh: "60s", want: maxDashboardQueryCacheTTL},
		{refresh: "invalid", want: defaultDashboardQueryCacheTTL},
	}
	for _, test := range tests {
		t.Run(test.refresh, func(t *testing.T) {
			if got := dashboardQueryCacheTTL(test.refresh); got != test.want {
				t.Fatalf("ttl = %s, want %s", got, test.want)
			}
		})
	}
}

func TestDashboardQueryCacheDoesNotReplaceNewerResult(t *testing.T) {
	cache := newDashboardQueryCache()
	key := queryCacheKey{Query: "signup | count", Limit: 1000}
	newer := queryCacheEntry{
		response:  &client.QueryJSONResponse{Rows: []map[string]any{{"count": 2}}},
		expiresAt: time.Now().Add(time.Minute),
		storedAt:  time.Now(),
		size:      20,
	}
	older := queryCacheEntry{
		response:  &client.QueryJSONResponse{Rows: []map[string]any{{"count": 1}}},
		expiresAt: time.Now().Add(time.Minute),
		storedAt:  newer.storedAt.Add(-time.Second),
		size:      10,
	}

	cache.storeLocked(key, newer)
	cache.storeLocked(key, older)

	if got := cache.entries[key].response.Rows[0]["count"]; got != 2 {
		t.Fatalf("cached count = %v, want newer result 2", got)
	}
	if cache.bytes != newer.size {
		t.Fatalf("cache bytes = %d, want %d", cache.bytes, newer.size)
	}
}

func TestServerRunCoalescesQueriesAcrossCardRequests(t *testing.T) {
	const dashboardYAML = `version: 1
title: Waterfall
sections:
  - title: One
    cards:
      - id: first
        title: First
        kind: metric
        viz: number
        query: signup | count
      - id: second
        title: Second
        kind: metric
        viz: number
        query: signup | count
`
	file := t.TempDir() + "/dashboard.yaml"
	if err := WriteNewFile(file, []byte(dashboardYAML), false); err != nil {
		t.Fatalf("write dashboard: %v", err)
	}
	base := &blockingQueryClient{
		started: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
	server, err := NewServer(file, "http://example.test", base)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	firstBody, err := json.Marshal(runRequest{DashboardID: "dashboard.yaml", CardIDs: []string{"first"}})
	if err != nil {
		t.Fatalf("marshal first run request: %v", err)
	}
	secondBody, err := json.Marshal(runRequest{DashboardID: "dashboard.yaml", CardIDs: []string{"second"}})
	if err != nil {
		t.Fatalf("marshal second run request: %v", err)
	}
	statuses := make(chan int, 2)

	go runCardRequestInTest(server, firstBody, statuses)
	<-base.started
	go runCardRequestInTest(server, secondBody, statuses)
	close(base.release)

	for range 2 {
		if status := <-statuses; status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
	}
	if got := base.calls.Load(); got != 1 {
		t.Fatalf("base calls = %d, want 1", got)
	}
}

func TestServerRunCapsQueriesAcrossDashboard(t *testing.T) {
	const dashboardYAML = `version: 1
title: Waterfall
sections:
  - title: One
    cards:
      - id: ratio
        title: Ratio
        kind: metric
        viz: number
        queries:
          - name: A
            query: signup | count
          - name: B
            query: purchase | count
          - name: C
            query: page_view | count
          - name: D
            query: login | count
          - name: E
            query: logout | count
`
	file := t.TempDir() + "/dashboard.yaml"
	if err := WriteNewFile(file, []byte(dashboardYAML), false); err != nil {
		t.Fatalf("write dashboard: %v", err)
	}
	base := &concurrencyQueryClient{}
	server, err := NewServer(file, "http://example.test", base)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	body, err := json.Marshal(runRequest{DashboardID: "dashboard.yaml", CardIDs: []string{"ratio"}})
	if err != nil {
		t.Fatalf("marshal run request: %v", err)
	}
	statuses := make(chan int, 1)
	runCardRequestInTest(server, body, statuses)
	if status := <-statuses; status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if got := base.maximum.Load(); got != maxDashboardQueryConcurrency {
		t.Fatalf("maximum query concurrency = %d, want %d", got, maxDashboardQueryConcurrency)
	}
}

func queryInTest(cached QueryClient, results chan<- *client.QueryJSONResponse, errs chan<- error) {
	result, err := cached.QueryJSON(context.Background(), "signup | count", 1000, 0)
	results <- result
	errs <- err
}

func runCardRequestInTest(server *Server, body []byte, statuses chan<- int) {
	req := httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader(string(body)))
	req.Host = "127.0.0.1:7331"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-WireLog-Dashboard-Token", server.Token)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	statuses <- rec.Code
}

type blockingQueryClient struct {
	calls   atomic.Int64
	started chan struct{}
	release chan struct{}
}

func (c *blockingQueryClient) QueryJSON(
	_ context.Context,
	_ string,
	_, _ int,
) (*client.QueryJSONResponse, error) {
	c.calls.Add(1)
	c.started <- struct{}{}
	<-c.release
	return &client.QueryJSONResponse{
		Columns: []string{"count"},
		Rows:    []map[string]any{{"count": 1}},
	}, nil
}
