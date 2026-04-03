package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wirelogai/wirelog-cli/internal/output"
)

var gdprYes bool

var gdprCmd = &cobra.Command{
	Use:   "gdpr",
	Short: "GDPR data export and deletion",
	Long:  "Export or delete user data for GDPR compliance.\n\nRequires a secret key (sk_) or access token with admin scope.",
}

var gdprExportCmd = &cobra.Command{
	Use:   "export <user-id>",
	Short: "Export all data for a user (NDJSON)",
	Long: `Stream all events for a user as NDJSON (one JSON object per line).

Examples:
  wl gdpr export user123
  wl gdpr export user123 > user_data.jsonl`,
	Args: cobra.ExactArgs(1),
	RunE: runGDPRExport,
}

var gdprDeleteCmd = &cobra.Command{
	Use:   "delete <user-id>",
	Short: "Delete all data for a user (destructive)",
	Long: `Permanently delete all events and profile data for a user.

Examples:
  wl gdpr delete user123
  wl gdpr delete user123 --yes`,
	Args: cobra.ExactArgs(1),
	RunE: runGDPRDelete,
}

func init() {
	gdprDeleteCmd.Flags().BoolVar(&gdprYes, "yes", false, "Skip confirmation prompt")
	gdprCmd.AddCommand(gdprExportCmd)
	gdprCmd.AddCommand(gdprDeleteCmd)
	rootCmd.AddCommand(gdprCmd)
}

func runGDPRExport(_ *cobra.Command, args []string) error {
	c, err := newClient()
	if err != nil {
		return err
	}

	ctx, cancel := cmdContext()
	defer cancel()

	body, err := c.GDPRExport(ctx, args[0])
	if err != nil {
		handleAPIError(err, "gdpr")
		return err
	}
	defer func() { _ = body.Close() }()

	// Stream directly to stdout without buffering the full response
	_, err = io.Copy(os.Stdout, body)
	return err
}

func runGDPRDelete(_ *cobra.Command, args []string) error {
	userID := args[0]

	if !gdprYes {
		fmt.Fprintf(os.Stderr, "Delete ALL data for user %q? This is permanent and cannot be undone.\nType the user ID to confirm: ", userID)
		reader := bufio.NewReader(os.Stdin)
		confirm, _ := reader.ReadString('\n')
		if strings.TrimSpace(confirm) != userID {
			return fmt.Errorf("confirmation did not match, aborting")
		}
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	ctx, cancel := cmdContext()
	defer cancel()

	err = c.GDPRDelete(ctx, userID)
	if err != nil {
		handleAPIError(err, "gdpr")
		return err
	}

	format := resolveFormat()
	if format == output.FormatJSON {
		return output.PrintRawJSON(os.Stdout, map[string]any{"status": "deleted", "user_id": userID})
	}
	stderr("Deleted all data for user %s", userID)
	return nil
}
