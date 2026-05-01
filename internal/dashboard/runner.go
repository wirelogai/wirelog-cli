package dashboard

import (
	"context"
	"fmt"
	"sync"

	"github.com/wirelogai/wirelog-cli/internal/client"
)

const defaultConcurrency = 32

// QueryClient is the subset of the WireLog client used by dashboards.
type QueryClient interface {
	QueryJSON(ctx context.Context, q string, limit, offset int) (*client.QueryJSONResponse, error)
}

// RunOptions controls dashboard query execution.
type RunOptions struct {
	Variables   map[string]string
	CardIDs     map[string]struct{}
	Concurrency int
	Limit       int
}

// CardResult contains all rendered query results for a card.
type CardResult struct {
	ID      string         `json:"id"`
	Title   string         `json:"title"`
	Kind    CardKind       `json:"kind"`
	Viz     VizKind        `json:"viz"`
	Series  []SeriesResult `json:"series,omitempty"`
	Error   string         `json:"error,omitempty"`
	Skipped bool           `json:"skipped,omitempty"`
}

// SeriesResult is one query result series.
type SeriesResult struct {
	Name       string         `json:"name"`
	Query      string         `json:"query"`
	Response   *QueryResponse `json:"response,omitempty"`
	Error      string         `json:"error,omitempty"`
	RetryAfter string         `json:"retry_after,omitempty"`
}

// QueryResponse is the query JSON shape embedded into dashboard results.
type QueryResponse struct {
	Columns     []string         `json:"columns"`
	Rows        []map[string]any `json:"rows"`
	Total       int              `json:"total"`
	ElapsedMS   int64            `json:"elapsed_ms"`
	RowsScanned int64            `json:"rows_scanned"`
	Period      string           `json:"period"`
	Mode        string           `json:"mode"`
	Metadata    map[string]any   `json:"metadata,omitempty"`
}

type queryJob struct {
	query string
}

type queryOutcome struct {
	response *QueryResponse
	err      error
	retry    string
}

// RunAll runs dashboard card queries with dedupe and bounded concurrency.
func RunAll(ctx context.Context, qc QueryClient, d *Dashboard, opts RunOptions) ([]CardResult, error) {
	if err := Validate(d); err != nil {
		return nil, err
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 1000
	}

	results, jobs := buildRunPlan(d, opts)
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = defaultConcurrency
	}
	if len(jobs) == 0 {
		concurrency = 0
	} else if concurrency > len(jobs) {
		concurrency = len(jobs)
	}
	outcomes := runJobs(ctx, qc, jobs, concurrency, limit)
	for ri := range results {
		for si := range results[ri].Series {
			series := &results[ri].Series[si]
			outcome := outcomes[series.Query]
			if outcome.err != nil {
				series.Error = outcome.err.Error()
				series.RetryAfter = outcome.retry
				continue
			}
			series.Response = outcome.response
		}
	}
	return results, nil
}

func buildRunPlan(d *Dashboard, opts RunOptions) ([]CardResult, []queryJob) {
	var results []CardResult
	seenQueries := map[string]struct{}{}
	var jobs []queryJob

	for _, section := range d.Sections {
		for _, card := range section.Cards {
			if len(opts.CardIDs) > 0 {
				if _, ok := opts.CardIDs[card.ID]; !ok {
					continue
				}
			}
			result := CardResult{ID: card.ID, Title: card.Title, Kind: card.Kind, Viz: card.Viz}
			if card.Kind == CardMarkdown {
				result.Skipped = true
				results = append(results, result)
				continue
			}
			queries := cardQueries(card)
			for _, nq := range queries {
				rendered, err := RenderQueryTemplate(nq.Query, d, opts.Variables)
				series := SeriesResult{Name: nq.Name, Query: rendered}
				if err != nil {
					series.Error = err.Error()
					result.Series = append(result.Series, series)
					continue
				}
				if _, ok := seenQueries[rendered]; !ok {
					seenQueries[rendered] = struct{}{}
					jobs = append(jobs, queryJob{query: rendered})
				}
				result.Series = append(result.Series, series)
			}
			results = append(results, result)
		}
	}
	return results, jobs
}

func runJobs(ctx context.Context, qc QueryClient, jobs []queryJob, concurrency, limit int) map[string]queryOutcome {
	outcomes := make(map[string]queryOutcome, len(jobs))
	var mu sync.Mutex
	work := make(chan queryJob)
	var wg sync.WaitGroup
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range work {
				resp, err := qc.QueryJSON(ctx, job.query, limit, 0)
				outcome := queryOutcome{}
				if err != nil {
					outcome.err = err
					if apiErr, ok := err.(*client.APIError); ok {
						outcome.retry = apiErr.RetryAfter
					}
				} else {
					outcome.response = fromClientResponse(resp)
				}
				mu.Lock()
				outcomes[job.query] = outcome
				mu.Unlock()
			}
		}()
	}
	for i, job := range jobs {
		select {
		case <-ctx.Done():
			markCanceled(outcomes, &mu, jobs[i:], ctx.Err())
			close(work)
			wg.Wait()
			return outcomes
		case work <- job:
		}
	}
	close(work)
	wg.Wait()
	return outcomes
}

func markCanceled(outcomes map[string]queryOutcome, mu *sync.Mutex, jobs []queryJob, err error) {
	mu.Lock()
	defer mu.Unlock()
	for _, job := range jobs {
		outcomes[job.query] = queryOutcome{err: fmt.Errorf("query canceled: %w", err)}
	}
}

func cardQueries(card Card) []NamedQuery {
	if card.Query != "" {
		return []NamedQuery{{Name: card.Title, Query: card.Query}}
	}
	return card.Queries
}

func fromClientResponse(resp *client.QueryJSONResponse) *QueryResponse {
	if resp == nil {
		return nil
	}
	return &QueryResponse{
		Columns:     resp.Columns,
		Rows:        resp.Rows,
		Total:       resp.Total,
		ElapsedMS:   resp.ElapsedMS,
		RowsScanned: resp.RowsScanned,
		Period:      resp.Period,
		Mode:        resp.Mode,
		Metadata:    resp.Metadata,
	}
}
