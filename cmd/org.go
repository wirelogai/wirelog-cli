package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/wirelogai/wirelog-cli/internal/output"
)

// Note: org commands use the session-authenticated /api/orgs endpoints.
// These are not accessible via API keys in the current server.
// For now, org list/get are only available if the server adds admin-key
// support for org endpoints. This command group is scaffolded for
// completeness and will return clear errors if the endpoint is unavailable.

var orgCmd = &cobra.Command{
	Use:   "org",
	Short: "Manage organizations",
	Long:  "Manage organizations. Some operations may require session auth not yet supported in the CLI.",
}

var orgListCmd = &cobra.Command{
	Use:   "list",
	Short: "List organizations",
	Long: `List organizations accessible with the configured API key.

Note: org endpoints currently require session auth on the server.
This command is scaffolded for future admin-key support.`,
	RunE: runOrgList,
}

func init() {
	orgCmd.AddCommand(orgListCmd)
	rootCmd.AddCommand(orgCmd)
}

func runOrgList(_ *cobra.Command, _ []string) error {
	// The /api/orgs endpoint requires session auth, which the CLI doesn't support.
	// We surface a clear error rather than sending a request that will get a 303 redirect.
	format := resolveFormat()
	if format == output.FormatJSON {
		return output.PrintRawJSON(os.Stdout, map[string]any{
			"error": "org list requires session auth, which is not yet supported in the CLI",
			"hint":  "Use the web dashboard at your WireLog host to manage organizations",
		})
	}
	fmt.Fprintln(os.Stderr, "Org management requires session auth, which is not yet supported in the CLI.")
	fmt.Fprintln(os.Stderr, "Use the web dashboard to manage organizations.")
	fmt.Fprintln(os.Stderr, "Project management is available via 'wl project' with an admin key (ak_).")
	return nil
}
