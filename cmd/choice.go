package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wirelogai/wirelog-cli/internal/output"
)

var (
	choiceConversion string
	choiceWindow     string
	choiceUnit       string
)

var choiceCmd = &cobra.Command{
	Use:   "choice",
	Short: "Analyze client-side choice() experiments",
}

var choiceResultsCmd = &cobra.Command{
	Use:   "results <choice_key>",
	Short: "Show conversion results for a choice() key",
	Args:  cobra.ExactArgs(1),
	RunE:  runChoiceResults,
}

func init() {
	choiceResultsCmd.Flags().StringVar(&choiceConversion, "conversion", "", "Conversion event, for example signup")
	choiceResultsCmd.Flags().StringVar(&choiceWindow, "window", "7d", "Conversion window, for example 7d")
	choiceResultsCmd.Flags().StringVar(&choiceUnit, "unit", "user_id", "Assignment unit: user_id, device_id, session_id, or any")
	choiceCmd.AddCommand(choiceResultsCmd)
	rootCmd.AddCommand(choiceCmd)
}

func runChoiceResults(_ *cobra.Command, args []string) error {
	if choiceConversion == "" {
		return fmt.Errorf("--conversion is required")
	}
	unit := strings.TrimSpace(choiceUnit)
	if unit == "any" {
		unit = ""
	}
	q := "choice " + dslToken(args[0]) + " | results " + dslToken(choiceConversion)
	if choiceWindow != "" {
		q += " | window " + choiceWindow
	}
	if unit != "" {
		q += " | unit " + dslToken(unit)
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
		handleAPIError(err, "choice results")
		return err
	}
	return output.PrintQueryResult(os.Stdout, format, data, flagNoColor)
}

func dslToken(value string) string {
	if value == "" {
		return strconv.Quote(value)
	}
	for _, r := range value {
		if r == '_' || r == '-' || r == ':' || r == '.' || r == '/' ||
			(r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') {
			continue
		}
		return strconv.Quote(value)
	}
	return value
}
