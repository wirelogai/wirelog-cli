package cmd

import (
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/wirelogai/wirelog-cli/internal/output"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	RunE:  runVersion,
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

func runVersion(_ *cobra.Command, _ []string) error {
	format := resolveFormat()

	if format == output.FormatJSON {
		return output.PrintRawJSON(os.Stdout, map[string]string{
			"version": version,
			"commit":  commit,
			"date":    date,
			"os":      runtime.GOOS,
			"arch":    runtime.GOARCH,
		})
	}

	fmt.Printf("wl/%s (%s/%s) built %s commit %s\n", version, runtime.GOOS, runtime.GOARCH, date, commit)
	return nil
}
