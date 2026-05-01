package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"strings"
)

// ExportMode controls whether HTML is fixed or live.
type ExportMode string

const (
	// ExportReport embeds fixed query results and no token.
	ExportReport ExportMode = "report"
	// ExportInteractive embeds a query token and can re-run queries in the browser.
	ExportInteractive ExportMode = "interactive"
	// ExportLocal is used by the local editable view server.
	ExportLocal ExportMode = "local"
)

// ExportOptions controls static dashboard rendering.
type ExportOptions struct {
	Mode         ExportMode
	Host         string
	Token        string
	SessionToken string
	Variables    map[string]string
	Dashboards   []DashboardRef
	DashboardID  string
}

type htmlPayload struct {
	Mode         ExportMode        `json:"mode"`
	Dashboard    *Dashboard        `json:"dashboard"`
	Results      []CardResult      `json:"results,omitempty"`
	Host         string            `json:"host,omitempty"`
	Token        string            `json:"token,omitempty"`
	SessionToken string            `json:"session_token,omitempty"`
	Variables    map[string]string `json:"variables,omitempty"`
	Dashboards   []DashboardRef    `json:"dashboards,omitempty"`
	DashboardID  string            `json:"dashboard_id,omitempty"`
}

type htmlTemplateData struct {
	CSS     template.CSS
	ECharts template.JS
	AppJS   template.JS
	Payload template.JS
}

// RenderHTML renders a self-contained dashboard HTML document.
func RenderHTML(d *Dashboard, results []CardResult, opts ExportOptions) ([]byte, error) {
	if opts.Mode == "" {
		opts.Mode = ExportReport
	}
	if opts.Mode == ExportInteractive {
		if err := validateInteractiveToken(opts.Token); err != nil {
			return nil, err
		}
	}
	payload := htmlPayload{
		Mode:         opts.Mode,
		Dashboard:    d,
		Results:      results,
		Host:         opts.Host,
		Token:        opts.Token,
		SessionToken: opts.SessionToken,
		Variables:    opts.Variables,
		Dashboards:   opts.Dashboards,
		DashboardID:  opts.DashboardID,
	}
	payloadJSON, err := safeJSON(payload)
	if err != nil {
		return nil, err
	}
	css, err := assets.ReadFile("assets/app.css")
	if err != nil {
		return nil, fmt.Errorf("read dashboard css: %w", err)
	}
	js, err := assets.ReadFile("assets/app.js")
	if err != nil {
		return nil, fmt.Errorf("read dashboard js: %w", err)
	}
	echarts, err := assets.ReadFile("assets/echarts.min.js")
	if err != nil {
		return nil, fmt.Errorf("read echarts asset: %w", err)
	}
	tmpl, err := assets.ReadFile("assets/index.html")
	if err != nil {
		return nil, fmt.Errorf("read dashboard html: %w", err)
	}
	t, err := template.New("dashboard").Parse(string(tmpl))
	if err != nil {
		return nil, fmt.Errorf("parse dashboard html template: %w", err)
	}
	var out bytes.Buffer
	err = t.Execute(&out, htmlTemplateData{
		CSS:     template.CSS(css),
		ECharts: template.JS(echarts),
		AppJS:   template.JS(js),
		Payload: template.JS(payloadJSON),
	})
	if err != nil {
		return nil, fmt.Errorf("render dashboard html: %w", err)
	}
	return out.Bytes(), nil
}

// SaveHTML renders and writes dashboard HTML to a file or stdout.
func SaveHTML(ctx context.Context, qc QueryClient, d *Dashboard, output string, opts ExportOptions) error {
	var results []CardResult
	var err error
	if opts.Mode == "" || opts.Mode == ExportReport {
		if qc == nil {
			return fmt.Errorf("report export requires a query client")
		}
		if err = ResolveDynamicVariables(ctx, qc, d); err != nil {
			return err
		}
		results, err = RunAll(ctx, qc, d, RunOptions{Variables: opts.Variables})
		if err != nil {
			return err
		}
	}
	html, err := RenderHTML(d, results, opts)
	if err != nil {
		return err
	}
	if output == "-" {
		_, err = os.Stdout.Write(html)
		if err != nil {
			return fmt.Errorf("write dashboard html to stdout: %w", err)
		}
		return nil
	}
	perm := os.FileMode(0o644)
	if opts.Mode == ExportInteractive {
		perm = 0o600
	}
	err = os.WriteFile(output, html, perm)
	if err != nil {
		return fmt.Errorf("write dashboard html: %w", err)
	}
	return nil
}

func safeJSON(v any) (string, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(true)
	err := enc.Encode(v)
	if err != nil {
		return "", fmt.Errorf("encode dashboard payload: %w", err)
	}
	return strings.TrimSpace(b.String()), nil
}

func validateInteractiveToken(token string) error {
	if token == "" {
		return fmt.Errorf("interactive export requires a query-scoped aat_ token")
	}
	if !strings.HasPrefix(token, "aat_") {
		return fmt.Errorf("interactive export only accepts aat_ access tokens")
	}
	return nil
}
