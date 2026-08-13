package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/wirelogai/wirelog-cli/internal/client"
)

const (
	defaultDashboardQueryCacheTTL = 15 * time.Second
	maxDashboardQueryCacheTTL     = 30 * time.Second
	dashboardQueryTimeout         = time.Minute
	maxDashboardQueryCacheEntries = 128
	maxDashboardQueryCacheBytes   = 16 << 20
	maxDashboardQueryEntryBytes   = 2 << 20
	maxDashboardQueryConcurrency  = 4
)

type queryCacheKey struct {
	Query  string
	Limit  int
	Offset int
}

type queryCacheEntry struct {
	response  *client.QueryJSONResponse
	refreshID string
	expiresAt time.Time
	storedAt  time.Time
	size      int
}

type queryFlight struct {
	done     chan struct{}
	response *client.QueryJSONResponse
	err      error
}

type dashboardQueryCache struct {
	mu         sync.Mutex
	entries    map[queryCacheKey]queryCacheEntry
	flights    map[queryCacheKey]*queryFlight
	bytes      int
	querySlots chan struct{}
}

type cachedQueryClient struct {
	base      QueryClient
	cache     *dashboardQueryCache
	ttl       time.Duration
	force     bool
	refreshID string
}

func newDashboardQueryCache() *dashboardQueryCache {
	return &dashboardQueryCache{
		entries:    make(map[queryCacheKey]queryCacheEntry),
		flights:    make(map[queryCacheKey]*queryFlight),
		querySlots: make(chan struct{}, maxDashboardQueryConcurrency),
	}
}

func (c *cachedQueryClient) QueryJSON(
	ctx context.Context,
	query string,
	limit, offset int,
) (*client.QueryJSONResponse, error) {
	return c.cache.query(
		ctx,
		c.base,
		queryCacheKey{Query: query, Limit: limit, Offset: offset},
		c.ttl,
		c.force,
		c.refreshID,
	)
}

func (c *dashboardQueryCache) query(
	ctx context.Context,
	base QueryClient,
	key queryCacheKey,
	ttl time.Duration,
	force bool,
	refreshID string,
) (*client.QueryJSONResponse, error) {
	now := time.Now()
	c.mu.Lock()
	if flight, ok := c.flights[key]; ok {
		c.mu.Unlock()
		return waitForQueryFlight(ctx, flight)
	}
	if entry, ok := c.entries[key]; ok && now.Before(entry.expiresAt) {
		if !force || entry.refreshID == refreshID {
			c.mu.Unlock()
			return entry.response, nil
		}
	}
	c.removeEntryLocked(key)
	flight := &queryFlight{done: make(chan struct{})}
	c.flights[key] = flight
	c.mu.Unlock()

	go c.runFlight(context.WithoutCancel(ctx), base, key, ttl, refreshID, now, flight)
	return waitForQueryFlight(ctx, flight)
}

func (c *dashboardQueryCache) runFlight(
	ctx context.Context,
	base QueryClient,
	key queryCacheKey,
	ttl time.Duration,
	refreshID string,
	startedAt time.Time,
	flight *queryFlight,
) {
	queryCtx, cancel := context.WithTimeout(ctx, dashboardQueryTimeout)
	defer cancel()
	select {
	case c.querySlots <- struct{}{}:
		defer func() { <-c.querySlots }()
	case <-queryCtx.Done():
		c.finishFlight(key, ttl, refreshID, startedAt, flight, nil, queryCtx.Err())
		return
	}
	response, err := base.QueryJSON(queryCtx, key.Query, key.Limit, key.Offset)
	c.finishFlight(key, ttl, refreshID, startedAt, flight, response, err)
}

func (c *dashboardQueryCache) finishFlight(
	key queryCacheKey,
	ttl time.Duration,
	refreshID string,
	startedAt time.Time,
	flight *queryFlight,
	response *client.QueryJSONResponse,
	err error,
) {
	entrySize := cachedResponseSize(response, err)

	c.mu.Lock()
	flight.response = response
	flight.err = err
	delete(c.flights, key)
	if entrySize > 0 && entrySize <= maxDashboardQueryEntryBytes && ttl > 0 {
		c.storeLocked(key, queryCacheEntry{
			response:  response,
			refreshID: refreshID,
			expiresAt: time.Now().Add(ttl),
			storedAt:  startedAt,
			size:      entrySize,
		})
	}
	close(flight.done)
	c.mu.Unlock()
}

func waitForQueryFlight(ctx context.Context, flight *queryFlight) (*client.QueryJSONResponse, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for dashboard query: %w", ctx.Err())
	case <-flight.done:
		return flight.response, flight.err
	}
}

func cachedResponseSize(response *client.QueryJSONResponse, err error) int {
	if response == nil || err != nil {
		return 0
	}
	encoded, marshalErr := json.Marshal(response)
	if marshalErr != nil {
		return 0
	}
	return len(encoded)
}

func (c *dashboardQueryCache) storeLocked(key queryCacheKey, entry queryCacheEntry) {
	if existing, ok := c.entries[key]; ok && existing.storedAt.After(entry.storedAt) {
		return
	}
	c.removeEntryLocked(key)
	now := time.Now()
	for existingKey, existing := range c.entries {
		if !now.Before(existing.expiresAt) {
			c.removeEntryLocked(existingKey)
		}
	}
	for len(c.entries) >= maxDashboardQueryCacheEntries || c.bytes+entry.size > maxDashboardQueryCacheBytes {
		oldestKey, ok := c.oldestEntryLocked()
		if !ok {
			break
		}
		c.removeEntryLocked(oldestKey)
	}
	c.entries[key] = entry
	c.bytes += entry.size
}

func (c *dashboardQueryCache) oldestEntryLocked() (queryCacheKey, bool) {
	var oldestKey queryCacheKey
	var oldestTime time.Time
	found := false
	for key, entry := range c.entries {
		if !found || entry.storedAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.storedAt
			found = true
		}
	}
	return oldestKey, found
}

func (c *dashboardQueryCache) removeEntryLocked(key queryCacheKey) {
	entry, ok := c.entries[key]
	if !ok {
		return
	}
	delete(c.entries, key)
	c.bytes -= entry.size
}

func dashboardQueryCacheTTL(refresh string) time.Duration {
	if refresh == "" {
		return defaultDashboardQueryCacheTTL
	}
	ttl, err := time.ParseDuration(refresh)
	if err != nil || ttl <= 0 {
		return defaultDashboardQueryCacheTTL
	}
	if ttl > maxDashboardQueryCacheTTL {
		return maxDashboardQueryCacheTTL
	}
	return ttl
}
