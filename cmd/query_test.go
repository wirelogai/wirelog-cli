package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/wirelogai/wirelog-cli/internal/output"
)

type fakeRawQueryClient struct {
	mu      sync.Mutex
	calls   []string
	results map[string][]byte
	errs    map[string]error
}

func (f *fakeRawQueryClient) Query(_ context.Context, q, _ string, _, _ int) ([]byte, string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, q)
	f.mu.Unlock()
	if err := f.errs[q]; err != nil {
		return nil, "", err
	}
	if data := f.results[q]; data != nil {
		return data, "application/json", nil
	}
	return []byte(`{"columns":[],"rows":[],"total":0}`), "application/json", nil
}

func (f *fakeRawQueryClient) calledQueries() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func TestParseQueryTextSingleQuery(t *testing.T) {
	got, err := parseQueryText(" * | last 7d | count ", false)
	if err != nil {
		t.Fatalf("parse query: %v", err)
	}
	assertQueries(t, got, []string{"* | last 7d | count"})
}

func TestParseQueryTextJSONList(t *testing.T) {
	got, err := parseQueryText(`["* | last 7d | count", "users | count"]`, false)
	if err != nil {
		t.Fatalf("parse query list: %v", err)
	}
	assertQueries(t, got, []string{"* | last 7d | count", "users | count"})
}

func TestParseQueryTextJSONListObject(t *testing.T) {
	got, err := parseQueryText(`{"queries":["inspect * | last 30d","users | list"]}`, false)
	if err != nil {
		t.Fatalf("parse query list object: %v", err)
	}
	assertQueries(t, got, []string{"inspect * | last 30d", "users | list"})
}

func TestParseQueryTextLineList(t *testing.T) {
	got, err := parseQueryText("\n* | last 7d | count\n\nusers | count\n", true)
	if err != nil {
		t.Fatalf("parse line list: %v", err)
	}
	assertQueries(t, got, []string{"* | last 7d | count", "users | count"})
}

func TestParseQueryInputsRepeatedFlag(t *testing.T) {
	got, err := parseQueryInputs(nil, []string{" * | last 7d | count ", "users | count"})
	if err != nil {
		t.Fatalf("parse query flags: %v", err)
	}
	assertQueries(t, got, []string{"* | last 7d | count", "users | count"})
}

func TestRunQueryListJSONPreservesInputOrder(t *testing.T) {
	fake := &fakeRawQueryClient{
		results: map[string][]byte{
			"first":  []byte(`{"rows":[{"value":1}]}`),
			"second": []byte(`{"rows":[{"value":2}]}`),
		},
	}
	var out bytes.Buffer
	err := runQueryList(context.Background(), fake, &out, []string{"first", "second"}, output.FormatJSON, 100, 0, 1, true)
	if err != nil {
		t.Fatalf("run query list: %v", err)
	}
	assertQueries(t, fake.calledQueries(), []string{"first", "second"})

	var decoded struct {
		Results []struct {
			Query    string          `json:"query"`
			Response json.RawMessage `json:"response"`
		} `json:"results"`
	}
	err = json.Unmarshal(out.Bytes(), &decoded)
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(decoded.Results) != 2 {
		t.Fatalf("result count = %d, want 2", len(decoded.Results))
	}
	firstValue := queryResultValue(t, decoded.Results[0].Response)
	if decoded.Results[0].Query != "first" || firstValue != 1 {
		t.Fatalf("first result mismatch: %+v", decoded.Results[0])
	}
	secondValue := queryResultValue(t, decoded.Results[1].Response)
	if decoded.Results[1].Query != "second" || secondValue != 2 {
		t.Fatalf("second result mismatch: %+v", decoded.Results[1])
	}
}

func TestRunQueryListJSONIncludesErrors(t *testing.T) {
	fake := &fakeRawQueryClient{
		errs: map[string]error{"bad": errors.New("nope")},
	}
	var out bytes.Buffer
	err := runQueryList(context.Background(), fake, &out, []string{"ok", "bad"}, output.FormatJSON, 100, 0, 2, true)
	if err == nil {
		t.Fatal("expected batch error")
	}
	if !strings.Contains(err.Error(), "1 query list entries failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), `"query": "bad"`) || !strings.Contains(out.String(), `"error": "nope"`) {
		t.Fatalf("output missing query error: %s", out.String())
	}
}

func TestRunQueryListRejectsCSV(t *testing.T) {
	var out bytes.Buffer
	err := printQueryList(&out, output.FormatCSV, []queryRunResult{{Query: "a"}}, true)
	if err == nil {
		t.Fatal("expected csv error")
	}
	if !strings.Contains(err.Error(), "csv") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func assertQueries(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("query %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func queryResultValue(t *testing.T, data json.RawMessage) int {
	t.Helper()
	var decoded struct {
		Rows []map[string]int `json:"rows"`
	}
	err := json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(decoded.Rows) != 1 {
		t.Fatalf("row count = %d, want 1", len(decoded.Rows))
	}
	return decoded.Rows[0]["value"]
}
