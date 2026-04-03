package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/wirelogai/wirelog-cli/internal/config"
	"github.com/wirelogai/wirelog-cli/internal/output"
)

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check API health",
	Long: `Check the health and readiness of the WireLog API.

No authentication required.

Examples:
  wl health
  wl health --host https://my-wirelog.example.com`,
	RunE: runHealth,
}

func init() {
	rootCmd.AddCommand(healthCmd)
}

func runHealth(_ *cobra.Command, _ []string) error {
	c := newClientNoAuth()

	ctx, cancel := cmdContext()
	defer cancel()

	status, err := c.Health(ctx)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	cfg := config.Resolve(flagAPIKey, flagHost)
	format := resolveFormat()

	if format == output.FormatJSON {
		return output.PrintRawJSON(os.Stdout, map[string]any{
			"host":    cfg.Host,
			"healthy": status.Healthy,
			"health":  status.Health,
			"ready":   status.Ready,
		})
	}

	checkMark := func(ok bool) string {
		if ok {
			return output.GreenText("✓", flagNoColor)
		}
		return output.WarnText("✗", flagNoColor)
	}

	_, _ = fmt.Fprintf(os.Stdout, "  %s API         %s\n", checkMark(status.Health == "ok" || status.Ready == "ready"), output.DimText(cfg.Host, flagNoColor))
	_, _ = fmt.Fprintf(os.Stdout, "  %s Health      %s\n", checkMark(status.Health == "ok"), status.Health)
	_, _ = fmt.Fprintf(os.Stdout, "  %s Ready       %s\n", checkMark(status.Ready == "ready"), status.Ready)

	return nil
}
