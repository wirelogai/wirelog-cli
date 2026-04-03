package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/wirelogai/wirelog-cli/internal/output"
)

var (
	queryLimit  int
	queryOffset int
)

var queryCmd = &cobra.Command{
	Use:     "query <dsl>",
	Aliases: []string{"q"},
	Short:   "Run a pipe-DSL analytics query",
	Long: `Run a pipe-based DSL query against your WireLog project.

Requires a secret key (sk_) or access token with query scope.

Examples:
  wl query "* | last 7d | count by event_type"
  wl query "page_view | last 30d | count by day" --format csv > report.csv
  wl query "funnel signup -> purchase | last 30d" --json
  echo "* | last 7d | count" | wl query -`,
	Args: cobra.ExactArgs(1),
	RunE: runQuery,
}

func init() {
	queryCmd.Flags().IntVar(&queryLimit, "limit", 100, "Max rows to return")
	queryCmd.Flags().IntVar(&queryOffset, "offset", 0, "Pagination offset")
	rootCmd.AddCommand(queryCmd)
}

func runQuery(_ *cobra.Command, args []string) error {
	q := args[0]

	// Read from stdin if arg is "-"
	if q == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		q = string(data)
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	format := resolveFormat()
	serverFmt := output.ServerFormat(format)

	ctx, cancel := cmdContext()
	defer cancel()

	data, _, err := c.Query(ctx, q, serverFmt, queryLimit, queryOffset)
	if err != nil {
		handleAPIError(err, "query")
		return err
	}

	return output.PrintQueryResult(os.Stdout, format, data, flagNoColor)
}
