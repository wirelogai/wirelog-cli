package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/wirelogai/wirelog-cli/internal/client"
)

func TestLoadValidateStarter(t *testing.T) {
	d, _, err := Load(strings.NewReader(StarterYAML))
	if err != nil {
		t.Fatalf("load starter: %v", err)
	}
	if err := Validate(d); err != nil {
		t.Fatalf("validate starter: %v", err)
	}
}

func TestRenderQueryTemplate(t *testing.T) {
	d, _, err := Load(strings.NewReader(StarterYAML))
	if err != nil {
		t.Fatalf("load starter: %v", err)
	}
	got, err := RenderQueryTemplate(`* | last {{range}} {{platform.fragment}} | count`, d, map[string]string{
		"range":    "7d",
		"platform": "web",
	})
	if err != nil {
		t.Fatalf("render query: %v", err)
	}
	want := `* | last 7d | where _platform = "web" | count`
	if got != want {
		t.Fatalf("query mismatch\nwant: %s\n got: %s", want, got)
	}
}

func TestRenderQueryTemplateInputVariable(t *testing.T) {
	d, _, err := Load(strings.NewReader(`version: 1
title: User Lookup
variables:
  subject:
    label: User
    type: input
    input: email
    required: true
    allow_domain_wildcard: true
    fragments:
      events:
        exact_field: user.email
        domain_field: user.email_domain
      users:
        exact_field: email
        domain_field: email_domain
sections:
  - title: One
    cards:
      - id: a
        title: A
        kind: table
        viz: table
        query: '* {{subject.events_fragment}} | count'
`))
	if err != nil {
		t.Fatalf("load dashboard: %v", err)
	}
	got, err := RenderQueryTemplate(d.Sections[0].Cards[0].Query, d, map[string]string{"subject": "Person@Gmail.com"})
	if err != nil {
		t.Fatalf("render exact query: %v", err)
	}
	want := `* | where user.email = "person@gmail.com" | count`
	if got != want {
		t.Fatalf("exact query mismatch\nwant: %s\n got: %s", want, got)
	}
	got, err = RenderQueryTemplate(d.Sections[0].Cards[0].Query, d, map[string]string{"subject": "*@Gmail.com"})
	if err != nil {
		t.Fatalf("render domain query: %v", err)
	}
	want = `* | where user.email_domain = "gmail.com" | count`
	if got != want {
		t.Fatalf("domain query mismatch\nwant: %s\n got: %s", want, got)
	}
	_, err = RenderQueryTemplate(d.Sections[0].Cards[0].Query, d, map[string]string{"subject": ""})
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("expected required error, got %v", err)
	}
}

func TestValidateRejectsUnknownVariable(t *testing.T) {
	src := strings.Replace(StarterYAML, "{{range}}", "{{missing}}", 1)
	d, _, err := Load(strings.NewReader(src))
	if err != nil {
		t.Fatalf("load dashboard: %v", err)
	}
	err = Validate(d)
	if err == nil || !strings.Contains(err.Error(), "unknown variable") {
		t.Fatalf("expected unknown variable error, got %v", err)
	}
}

func TestResolveDynamicVariables(t *testing.T) {
	d, _, err := Load(strings.NewReader(`version: 1
title: Test
variables:
  function:
    label: Function
    type: select
    default: all
    query: 'feature | last 90d | count by event_properties.functionName'
    value_column: functionName
    label_column: functionName
    fragment_template: '| where event_properties.functionName = "{{value}}"'
    options:
      - label: All
        value: all
        fragment: ""
sections:
  - title: One
    cards:
      - id: a
        title: A
        kind: table
        viz: table
        query: 'feature {{function.fragment}} | count'
`))
	if err != nil {
		t.Fatalf("load dashboard: %v", err)
	}
	fake := &fakeQueryClient{response: &client.QueryJSONResponse{
		Columns: []string{"arrayElement(event_properties, 'functionName')", "count"},
		Rows: []map[string]any{
			{"arrayElement(event_properties, 'functionName')": "web_search", "count": 8},
			{"arrayElement(event_properties, 'functionName')": "send_email", "count": 10},
		},
		Total: 2,
		Mode:  "aggregate",
	}}
	if err = ResolveDynamicVariables(context.Background(), fake, d); err != nil {
		t.Fatalf("resolve dynamic variables: %v", err)
	}
	opts := d.Variables["function"].Options
	if len(opts) != 3 {
		t.Fatalf("options length = %d, want 3", len(opts))
	}
	if opts[0].Value != "all" || opts[1].Value != "send_email" || opts[2].Value != "web_search" {
		t.Fatalf("options order = %#v, want all, send_email, web_search", opts)
	}
	got, err := RenderQueryTemplate(d.Sections[0].Cards[0].Query, d, map[string]string{"function": "send_email"})
	if err != nil {
		t.Fatalf("render query: %v", err)
	}
	want := `feature | where event_properties.functionName = "send_email" | count`
	if got != want {
		t.Fatalf("query mismatch\nwant: %s\n got: %s", want, got)
	}
}

func TestRunAllDedupesQueries(t *testing.T) {
	d, _, err := Load(strings.NewReader(`version: 1
title: Test
sections:
  - title: One
    cards:
      - id: a
        title: A
        kind: chart
        viz: line
        query: '* | last 7d | count'
      - id: b
        title: B
        kind: chart
        viz: line
        query: '* | last 7d | count'
`))
	if err != nil {
		t.Fatalf("load dashboard: %v", err)
	}
	fake := &fakeQueryClient{}
	results, err := RunAll(context.Background(), fake, d, RunOptions{})
	if err != nil {
		t.Fatalf("run all: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results length = %d", len(results))
	}
	if got := fake.calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

func TestRenderHTMLEscapesScriptEnd(t *testing.T) {
	d := &Dashboard{
		Version: 1,
		Title:   `bad </script><script>alert(1)</script>`,
		Sections: []Section{{
			Title: "Notes",
			Cards: []Card{{
				ID:       "note",
				Title:    "Note",
				Kind:     CardMarkdown,
				Viz:      VizMarkdown,
				Markdown: `bad </script><script>alert(1)</script>`,
			}},
		}},
	}
	html, err := RenderHTML(d, nil, ExportOptions{Mode: ExportReport})
	if err != nil {
		t.Fatalf("render html: %v", err)
	}
	if strings.Contains(string(html), `</script><script>alert(1)</script>`) {
		t.Fatalf("html contains unescaped script terminator")
	}
}

func TestInteractiveSaveDoesNotQueryAndUsesPrivateMode(t *testing.T) {
	d, _, err := Load(strings.NewReader(StarterYAML))
	if err != nil {
		t.Fatalf("load starter: %v", err)
	}
	out := t.TempDir() + "/interactive.html"
	fake := &fakeQueryClient{}
	err = SaveHTML(context.Background(), fake, d, out, ExportOptions{Mode: ExportInteractive, Token: "aat_test"})
	if err != nil {
		t.Fatalf("save html: %v", err)
	}
	if got := fake.calls.Load(); got != 0 {
		t.Fatalf("interactive export called QueryJSON %d times", got)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %v, want 0600", got)
	}
}

func TestServerSecurity(t *testing.T) {
	server := newTestServer(t)
	tests := []struct {
		name   string
		token  string
		host   string
		origin string
		want   int
	}{
		{
			name: "missing token",
			host: "127.0.0.1:7331",
			want: http.StatusForbidden,
		},
		{
			name:   "foreign origin",
			token:  server.Token,
			host:   "127.0.0.1:7331",
			origin: "http://evil.example.com",
			want:   http.StatusForbidden,
		},
		{
			name:  "non-loopback host",
			token: server.Token,
			host:  "evil.example.com:7331",
			want:  http.StatusForbidden,
		},
		{
			name:   "happy path",
			token:  server.Token,
			host:   "127.0.0.1:7331",
			origin: "http://127.0.0.1:7331",
			want:   http.StatusOK,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
			req.Host = tt.host
			if tt.token != "" {
				req.Header.Set("X-WireLog-Dashboard-Token", tt.token)
			}
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rec := httptest.NewRecorder()
			server.Handler().ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestServerRejectsStaleETag(t *testing.T) {
	server := newTestServer(t)
	body, err := json.Marshal(saveRequest{Raw: StarterYAML, ETag: "stale"})
	if err != nil {
		t.Fatalf("marshal save request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/dashboard", strings.NewReader(string(body)))
	req.Host = "127.0.0.1:7331"
	req.Header.Set("X-WireLog-Dashboard-Token", server.Token)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestDiscoverDashboardFilesDirectory(t *testing.T) {
	dir := t.TempDir()
	first := strings.Replace(StarterYAML, "title: Product Growth", "title: Alpha", 1)
	first = strings.Replace(first, "order: 10", "order: 20", 1)
	second := strings.Replace(StarterYAML, "title: Product Growth", "title: Beta", 1)
	second = strings.Replace(second, "order: 10", "order: 5", 1)
	if err := WriteNewFile(dir+"/alpha.yaml", []byte(first), false); err != nil {
		t.Fatalf("write first dashboard: %v", err)
	}
	if err := WriteNewFile(dir+"/beta.yml", []byte(second), false); err != nil {
		t.Fatalf("write second dashboard: %v", err)
	}
	refs, err := DiscoverDashboardFiles(dir)
	if err != nil {
		t.Fatalf("discover dashboards: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("refs length = %d, want 2", len(refs))
	}
	if refs[0].ID != "beta.yml" || refs[0].Title != "Beta" || refs[0].Order != 5 {
		t.Fatalf("first ref = %#v", refs[0])
	}
	if refs[1].ID != "alpha.yaml" || refs[1].Title != "Alpha" || refs[1].Order != 20 {
		t.Fatalf("second ref = %#v", refs[1])
	}
}

func TestServerDashboardRoutes(t *testing.T) {
	dir := t.TempDir()
	alpha := strings.Replace(StarterYAML, "title: Product Growth", "title: Alpha", 1)
	alpha = strings.Replace(alpha, "order: 10", "order: 20", 1)
	beta := strings.Replace(StarterYAML, "title: Product Growth", "title: Beta", 1)
	beta = strings.Replace(beta, "order: 10", "order: 5", 1)
	if err := WriteNewFile(dir+"/alpha.yaml", []byte(alpha), false); err != nil {
		t.Fatalf("write alpha dashboard: %v", err)
	}
	if err := WriteNewFile(dir+"/beta.yml", []byte(beta), false); err != nil {
		t.Fatalf("write beta dashboard: %v", err)
	}
	server, err := NewServer(dir, "http://example.test", &fakeQueryClient{})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	tests := []struct {
		path        string
		wantStatus  int
		wantPayload string
	}{
		{path: "/", wantStatus: http.StatusOK, wantPayload: `"dashboard_id":"beta.yml"`},
		{path: "/dashboard/beta.yml", wantStatus: http.StatusOK, wantPayload: `"dashboard_id":"beta.yml"`},
		{path: "/dashboard/beta", wantStatus: http.StatusOK, wantPayload: `"dashboard_id":"beta.yml"`},
		{path: "/dashboard/missing", wantStatus: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req.Host = "127.0.0.1:7331"
			rec := httptest.NewRecorder()
			server.Handler().ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantPayload != "" && !strings.Contains(rec.Body.String(), tt.wantPayload) {
				t.Fatalf("response missing %s", tt.wantPayload)
			}
		})
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	d, _, err := Load(strings.NewReader(StarterYAML))
	if err != nil {
		t.Fatalf("load starter: %v", err)
	}
	raw, err := Marshal(d)
	if err != nil {
		t.Fatalf("marshal starter: %v", err)
	}
	file := t.TempDir() + "/dashboard.yaml"
	if err := WriteNewFile(file, raw, false); err != nil {
		t.Fatalf("write dashboard: %v", err)
	}
	server, err := NewServer(file, "http://example.test", &fakeQueryClient{})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return server
}

type fakeQueryClient struct {
	calls    atomic.Int64
	response *client.QueryJSONResponse
}

func (f *fakeQueryClient) QueryJSON(_ context.Context, _ string, _, _ int) (*client.QueryJSONResponse, error) {
	f.calls.Add(1)
	if f.response != nil {
		return f.response, nil
	}
	return &client.QueryJSONResponse{
		Columns: []string{"day", "count"},
		Rows: []map[string]any{
			{"day": "2026-05-01", "count": 10},
		},
		Total:     1,
		ElapsedMS: 2,
		Mode:      "aggregate",
	}, nil
}
