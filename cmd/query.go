package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/wirelogai/wirelog-cli/internal/output"
)

const defaultQueryConcurrency = 8

var (
	queryLimit       int
	queryOffset      int
	queryConcurrency int
	queryInputs      []string
)

var queryCmd = &cobra.Command{
	Use:     "query [dsl|json-list|-]",
	Aliases: []string{"q"},
	Short:   "Run a pipe-DSL analytics query",
	Long: `Run a pipe-based DSL query against your WireLog project.

Requires a secret key (sk_) or access token with query scope.

Examples:
  wl query "* | last 7d | count by event_type"
  wl query "page_view | last 30d | count by day" --format csv > report.csv
  wl query "funnel signup -> purchase | last 30d" --json
  echo "* | last 7d | count" | wl query -
  wl query --query "* | last 7d | count" --query "users | count" --json
  wl query '["* | last 7d | count", "users | count"]' --json
  printf '%s\n' "* | last 7d | count" "users | count" | wl query - --json`,
	Args: validateQueryArgs,
	RunE: runQuery,
}

func init() {
	queryCmd.Flags().IntVar(&queryLimit, "limit", 100, "Max rows to return")
	queryCmd.Flags().IntVar(&queryOffset, "offset", 0, "Pagination offset")
	queryCmd.Flags().IntVar(&queryConcurrency, "concurrency", defaultQueryConcurrency, "Max concurrent queries when running a query list")
	queryCmd.Flags().StringArrayVar(&queryInputs, "query", nil, "Query DSL to run; repeat for a query list")
	rootCmd.AddCommand(queryCmd)
}

var (
	errNoQueries       = errors.New("no queries provided")
	errBatchCSV        = errors.New("csv output supports only one query; use --json for query lists")
	errBatchQueryFails = errors.New("one or more queries failed")
)

type rawQueryClient interface {
	Query(ctx context.Context, q, format string, limit, offset int) ([]byte, string, error)
}

type queryListObject struct {
	Queries []string `json:"queries"`
}

type queryRunResult struct {
	Query string
	Data  []byte
	Err   error
}

type queryBatchJSONResult struct {
	Query    string          `json:"query"`
	Response json.RawMessage `json:"response,omitempty"`
	Error    string          `json:"error,omitempty"`
}

func runQuery(_ *cobra.Command, args []string) error {
	queries, err := parseQueryInputs(args, queryInputs)
	if err != nil {
		return err
	}
	format := resolveFormat()
	if len(queries) > 1 && format == output.FormatCSV {
		return errBatchCSV
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	ctx, cancel := cmdContext()
	defer cancel()

	if len(queries) > 1 {
		return runQueryList(ctx, c, os.Stdout, queries, format, queryLimit, queryOffset, queryConcurrency, flagNoColor)
	}

	q := queries[0]
	serverFmt := output.ServerFormat(format)
	data, _, err := c.Query(ctx, q, serverFmt, queryLimit, queryOffset)
	if err != nil {
		handleAPIError(err, "query")
		return err
	}

	return output.PrintQueryResult(os.Stdout, format, data, flagNoColor)
}

func validateQueryArgs(_ *cobra.Command, args []string) error {
	if len(queryInputs) > 0 {
		if len(args) > 0 {
			return fmt.Errorf("--query cannot be combined with positional query input")
		}
		return nil
	}
	if len(args) != 1 {
		return fmt.Errorf("accepts 1 arg(s), received %d", len(args))
	}
	return nil
}

func parseQueryInputs(args, flagQueries []string) ([]string, error) {
	if len(flagQueries) > 0 {
		return cleanQueries(flagQueries)
	}
	return parseQueryArgument(args[0])
}

func parseQueryArgument(arg string) ([]string, error) {
	if arg == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return parseQueryText(string(data), true)
	}
	return parseQueryText(arg, false)
}

func parseQueryText(text string, splitLines bool) ([]string, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, errNoQueries
	}

	if strings.HasPrefix(trimmed, "[") {
		var queries []string
		err := json.Unmarshal([]byte(trimmed), &queries)
		if err != nil {
			return nil, fmt.Errorf("parse query list: %w", err)
		}
		return cleanQueries(queries)
	}
	if strings.HasPrefix(trimmed, "{") {
		var obj queryListObject
		err := json.Unmarshal([]byte(trimmed), &obj)
		if err != nil {
			return nil, fmt.Errorf("parse query list object: %w", err)
		}
		if obj.Queries == nil {
			return nil, fmt.Errorf("parse query list object: missing queries")
		}
		return cleanQueries(obj.Queries)
	}
	if splitLines {
		return cleanQueries(strings.Split(trimmed, "\n"))
	}
	return []string{trimmed}, nil
}

func cleanQueries(queries []string) ([]string, error) {
	cleaned := make([]string, 0, len(queries))
	for _, q := range queries {
		q = strings.TrimSpace(q)
		if q != "" {
			cleaned = append(cleaned, q)
		}
	}
	if len(cleaned) == 0 {
		return nil, errNoQueries
	}
	return cleaned, nil
}

func runQueryList(ctx context.Context, c rawQueryClient, w io.Writer, queries []string, format output.Format, limit, offset, concurrency int, noColor bool) error {
	if concurrency <= 0 {
		return fmt.Errorf("concurrency must be greater than 0")
	}
	serverFmt := output.ServerFormat(format)
	results := runQueryRequests(ctx, c, queries, serverFmt, limit, offset, concurrency)
	err := printQueryList(w, format, results, noColor)
	if err != nil {
		return err
	}
	if failed := countQueryFailures(results); failed > 0 {
		printQueryListErrors(results)
		return fmt.Errorf("%d query list entries failed: %w", failed, errBatchQueryFails)
	}
	return nil
}

func runQueryRequests(ctx context.Context, c rawQueryClient, queries []string, serverFmt string, limit, offset, concurrency int) []queryRunResult {
	if concurrency > len(queries) {
		concurrency = len(queries)
	}

	results := make([]queryRunResult, len(queries))
	work := make(chan int)
	var wg sync.WaitGroup
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				data, _, err := c.Query(ctx, queries[i], serverFmt, limit, offset)
				results[i] = queryRunResult{Query: queries[i], Data: data, Err: err}
			}
		}()
	}

sendLoop:
	for i := range queries {
		select {
		case <-ctx.Done():
			for j := i; j < len(queries); j++ {
				results[j] = queryRunResult{
					Query: queries[j],
					Err:   fmt.Errorf("query canceled: %w", ctx.Err()),
				}
			}
			break sendLoop
		case work <- i:
		}
	}
	close(work)
	wg.Wait()
	return results
}

func printQueryList(w io.Writer, format output.Format, results []queryRunResult, noColor bool) error {
	switch format {
	case output.FormatJSON:
		return printQueryListJSON(w, results)
	case output.FormatTable:
		return printQueryListTables(w, results, noColor)
	case output.FormatMarkdown:
		return printQueryListMarkdown(w, results)
	case output.FormatCSV:
		return errBatchCSV
	default:
		return fmt.Errorf("query list output does not support %s format", format)
	}
}

func printQueryListJSON(w io.Writer, results []queryRunResult) error {
	out := make([]queryBatchJSONResult, 0, len(results))
	for _, result := range results {
		item := queryBatchJSONResult{Query: result.Query}
		if result.Err != nil {
			item.Error = result.Err.Error()
			out = append(out, item)
			continue
		}
		if json.Valid(result.Data) {
			item.Response = json.RawMessage(result.Data)
		} else {
			encoded, err := json.Marshal(string(result.Data))
			if err != nil {
				return fmt.Errorf("encode query response: %w", err)
			}
			item.Response = json.RawMessage(encoded)
		}
		out = append(out, item)
	}
	return output.PrintRawJSON(w, map[string]any{"results": out})
}

func printQueryListTables(w io.Writer, results []queryRunResult, noColor bool) error {
	printed := false
	for i, result := range results {
		if result.Err != nil {
			continue
		}
		if printed {
			_, err := fmt.Fprintln(w)
			if err != nil {
				return err
			}
		}
		_, err := fmt.Fprintf(w, "Query %d: %s\n", i+1, result.Query)
		if err != nil {
			return err
		}
		err = output.PrintQueryResult(w, output.FormatTable, result.Data, noColor)
		if err != nil {
			return err
		}
		printed = true
	}
	return nil
}

func printQueryListMarkdown(w io.Writer, results []queryRunResult) error {
	printed := false
	for i, result := range results {
		if result.Err != nil {
			continue
		}
		if printed {
			_, err := fmt.Fprintln(w)
			if err != nil {
				return err
			}
		}
		_, err := fmt.Fprintf(w, "## Query %d\n\n```wirelog\n%s\n```\n\n", i+1, result.Query)
		if err != nil {
			return err
		}
		_, err = w.Write(result.Data)
		if err != nil {
			return err
		}
		if len(result.Data) > 0 && result.Data[len(result.Data)-1] != '\n' {
			_, err = fmt.Fprintln(w)
			if err != nil {
				return err
			}
		}
		printed = true
	}
	return nil
}

func countQueryFailures(results []queryRunResult) int {
	failed := 0
	for _, result := range results {
		if result.Err != nil {
			failed++
		}
	}
	return failed
}

func printQueryListErrors(results []queryRunResult) {
	for i, result := range results {
		if result.Err == nil {
			continue
		}
		fmt.Fprintf(os.Stderr, "Query %d failed: %s\n", i+1, result.Query)
		handleAPIError(result.Err, "query")
	}
}
