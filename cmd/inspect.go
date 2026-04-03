package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/wirelogai/wirelog-cli/internal/output"
)

var inspectLast string

var inspectCmd = &cobra.Command{
	Use:   "inspect [event]",
	Short: "Discover events and properties",
	Long: `Discover events and their properties using the inspect source.

Requires a secret key (sk_) or access token with query scope.

Examples:
  wl inspect              # inspect * | last 30d
  wl inspect signup       # inspect signup | last 7d
  wl inspect --last 90d   # inspect * | last 90d`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInspect,
}

func init() {
	inspectCmd.Flags().StringVar(&inspectLast, "last", "", "Time range (default: 30d for *, 7d for specific event)")
	rootCmd.AddCommand(inspectCmd)
}

func runInspect(_ *cobra.Command, args []string) error {
	target := "*"
	defaultLast := "30d"

	if len(args) > 0 {
		target = args[0]
		defaultLast = "7d"
	}

	last := inspectLast
	if last == "" {
		last = defaultLast
	}

	q := fmt.Sprintf("inspect %s | last %s", target, last)

	c, err := newClient()
	if err != nil {
		return err
	}

	format := resolveFormat()
	serverFmt := output.ServerFormat(format)

	ctx, cancel := cmdContext()
	defer cancel()

	data, _, err := c.Query(ctx, q, serverFmt, 100, 0)
	if err != nil {
		handleAPIError(err, "query")
		return err
	}

	return output.PrintQueryResult(os.Stdout, format, data, flagNoColor)
}
