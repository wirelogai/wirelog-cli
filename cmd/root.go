// Package cmd implements the WireLog CLI command tree.
package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/wirelogai/wirelog-cli/internal/client"
	"github.com/wirelogai/wirelog-cli/internal/config"
	"github.com/wirelogai/wirelog-cli/internal/output"
)

// Build info, injected via ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// Global flags.
var (
	flagAPIKey  string
	flagHost    string
	flagFormat  string
	flagJSON    bool
	flagNoColor bool
	flagTimeout time.Duration
	flagQuiet   bool
)

var rootCmd = &cobra.Command{
	Use:           "wl",
	Short:         "WireLog CLI — analytics for agents and LLMs",
	Long:          "Headless analytics from your terminal. Events in, insights out.\n\nhttps://wirelog.ai",
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&flagAPIKey, "api-key", "", "API key (overrides WIRELOG_API_KEY)")
	pf.StringVar(&flagHost, "host", "", "API host (overrides WIRELOG_HOST)")
	pf.StringVar(&flagFormat, "format", "auto", "Output format: table, json, csv, markdown")
	pf.BoolVar(&flagJSON, "json", false, "Shorthand for --format=json")
	pf.BoolVar(&flagNoColor, "no-color", false, "Disable color output")
	pf.DurationVar(&flagTimeout, "timeout", 30*time.Second, "Request timeout")
	pf.BoolVar(&flagQuiet, "quiet", false, "Suppress non-essential stderr output")
}

// Execute runs the root command.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// resolveFormat returns the resolved output format from flags.
func resolveFormat() output.Format {
	if flagJSON {
		return output.FormatJSON
	}
	return output.ResolveFormat(output.Format(flagFormat))
}

// newClient creates a configured API client from resolved config.
func newClient() (*client.Client, error) {
	cfg := config.Resolve(flagAPIKey, flagHost)
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("no API key configured. Run 'wl config init' or set WIRELOG_API_KEY")
	}
	return client.New(cfg.Host, cfg.APIKey, version, flagTimeout), nil
}

// newClientNoAuth creates a client that doesn't require an API key.
func newClientNoAuth() *client.Client {
	cfg := config.Resolve(flagAPIKey, flagHost)
	return client.New(cfg.Host, cfg.APIKey, version, flagTimeout)
}

// cmdContext returns a context with the configured timeout.
func cmdContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), flagTimeout)
}

// stderr prints a diagnostic message to stderr (unless --quiet).
func stderr(format string, args ...any) {
	if !flagQuiet {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}
}

// handleAPIError prints a user-friendly error message for API errors.
func handleAPIError(err error, operation string) {
	apiErr, ok := err.(*client.APIError)
	if !ok {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}

	fmt.Fprintf(os.Stderr, "Error: %s\n", apiErr.Message)

	if apiErr.IsAuthError() {
		fmt.Fprintln(os.Stderr, client.AuthHint(operation))
	}
	if apiErr.IsRateLimit() && apiErr.RetryAfter != "" {
		fmt.Fprintf(os.Stderr, "Rate limited. Retry after %s seconds.\n", apiErr.RetryAfter)
	}
}
