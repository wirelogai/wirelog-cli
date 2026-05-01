package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/wirelogai/wirelog-cli/internal/dashboard"
	"github.com/wirelogai/wirelog-cli/internal/output"
)

var (
	dashboardFile       string
	dashboardOutput     string
	dashboardMode       string
	dashboardForce      bool
	dashboardOpen       bool
	dashboardPort       int
	dashboardTokenEnv   string
	dashboardTokenStdin bool
	dashboardJSON       bool
	dashboardVars       []string
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Create, view, and export YAML dashboards",
}

var dashboardInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Write an agent-friendly starter dashboard",
	RunE:  runDashboardInit,
}

var dashboardSchemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Print the dashboard JSON Schema",
	RunE:  runDashboardSchema,
}

var dashboardValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate a dashboard YAML file",
	RunE:  runDashboardValidate,
}

var dashboardSaveCmd = &cobra.Command{
	Use:   "save",
	Short: "Export a dashboard as HTML",
	RunE:  runDashboardSave,
}

var dashboardRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run every query in a dashboard YAML file",
	RunE:  runDashboardRun,
}

var dashboardViewCmd = &cobra.Command{
	Use:   "view",
	Short: "Serve an editable local dashboard",
	RunE:  runDashboardView,
}

func init() {
	dashboardInitCmd.Flags().StringVarP(&dashboardOutput, "output", "o", "dashboard.yaml", "Output file, or - for stdout")
	dashboardInitCmd.Flags().BoolVar(&dashboardForce, "force", false, "Overwrite an existing file")

	dashboardSchemaCmd.Flags().StringVarP(&dashboardOutput, "output", "o", "-", "Output file, or - for stdout")
	dashboardSchemaCmd.Flags().BoolVar(&dashboardForce, "force", false, "Overwrite an existing file")

	dashboardValidateCmd.Flags().StringVarP(&dashboardFile, "file", "f", "dashboard.yaml", "Dashboard YAML file, or - for stdin")
	dashboardValidateCmd.Flags().BoolVar(&dashboardJSON, "json", false, "Print JSON validation result")

	dashboardSaveCmd.Flags().StringVarP(&dashboardFile, "file", "f", "dashboard.yaml", "Dashboard YAML file")
	dashboardSaveCmd.Flags().StringVarP(&dashboardOutput, "output", "o", "index.html", "Output HTML file, or - for stdout")
	dashboardSaveCmd.Flags().StringVar(&dashboardMode, "mode", "report", "Export mode: report or interactive")
	dashboardSaveCmd.Flags().StringVar(&dashboardTokenEnv, "token-env", "WIRELOG_DASHBOARD_TOKEN", "Environment variable containing interactive aat_ token")
	dashboardSaveCmd.Flags().BoolVar(&dashboardTokenStdin, "token-stdin", false, "Read interactive token from stdin")

	dashboardRunCmd.Flags().StringVarP(&dashboardFile, "file", "f", "dashboard.yaml", "Dashboard YAML file")
	dashboardRunCmd.Flags().StringArrayVar(&dashboardVars, "var", nil, "Dashboard variable override as name=value (repeatable)")

	dashboardViewCmd.Flags().StringVarP(&dashboardFile, "file", "f", "dashboard.yaml", "Dashboard YAML file or directory")
	dashboardViewCmd.Flags().IntVar(&dashboardPort, "port", 0, "Local port, or 0 for a random port")
	dashboardViewCmd.Flags().BoolVar(&dashboardOpen, "open", true, "Open the dashboard in a browser")

	dashboardCmd.AddCommand(dashboardInitCmd, dashboardSchemaCmd, dashboardValidateCmd, dashboardSaveCmd, dashboardRunCmd, dashboardViewCmd)
	rootCmd.AddCommand(dashboardCmd)
}

func runDashboardInit(_ *cobra.Command, _ []string) error {
	return dashboard.WriteNewFile(dashboardOutput, []byte(dashboard.StarterYAML), dashboardForce)
}

func runDashboardSchema(_ *cobra.Command, _ []string) error {
	return dashboard.WriteNewFile(dashboardOutput, []byte(dashboard.SchemaJSON+"\n"), dashboardForce)
}

func runDashboardValidate(_ *cobra.Command, _ []string) error {
	d, _, err := loadDashboardInput(dashboardFile)
	if err != nil {
		return err
	}
	err = dashboard.Validate(d)
	if dashboardJSON {
		if err != nil {
			_, writeErr := fmt.Fprintf(os.Stdout, `{"ok":false,"error":%q}`+"\n", err.Error())
			if writeErr != nil {
				return fmt.Errorf("write validation result: %w", writeErr)
			}
			return err
		}
		_, writeErr := fmt.Fprintln(os.Stdout, `{"ok":true}`)
		if writeErr != nil {
			return fmt.Errorf("write validation result: %w", writeErr)
		}
		return nil
	}
	if err != nil {
		return err
	}
	stderr("dashboard ok")
	return nil
}

func runDashboardSave(_ *cobra.Command, _ []string) error {
	d, _, err := loadDashboardInput(dashboardFile)
	if err != nil {
		return err
	}
	if err = dashboard.Validate(d); err != nil {
		return err
	}

	mode := dashboard.ExportMode(dashboardMode)
	opts := dashboard.ExportOptions{Mode: mode}
	c := newClientNoAuth()
	opts.Host = c.BaseURL
	if mode == dashboard.ExportInteractive {
		token, tokenErr := resolveDashboardToken()
		if tokenErr != nil {
			return tokenErr
		}
		opts.Token = token
		err = dashboard.SaveHTML(context.Background(), nil, d, dashboardOutput, opts)
		if err != nil {
			return err
		}
		if dashboardOutput != "-" {
			stderr("warning: interactive dashboard embeds an aat_ token; wrote %s with 0600 permissions", dashboardOutput)
		}
		return nil
	}
	if mode != dashboard.ExportReport {
		return fmt.Errorf("invalid dashboard mode %q", dashboardMode)
	}
	c, err = newClient()
	if err != nil {
		return err
	}
	ctx, cancel := cmdContext()
	defer cancel()
	return dashboard.SaveHTML(ctx, c, d, dashboardOutput, opts)
}

func runDashboardRun(_ *cobra.Command, _ []string) error {
	d, _, err := loadDashboardInput(dashboardFile)
	if err != nil {
		return err
	}
	if err = dashboard.Validate(d); err != nil {
		return err
	}
	vars, err := parseDashboardVars(dashboardVars)
	if err != nil {
		return err
	}
	c, err := newClient()
	if err != nil {
		return err
	}
	ctx, cancel := cmdContext()
	defer cancel()
	if err = dashboard.ResolveDynamicVariables(ctx, c, d); err != nil {
		return err
	}
	results, err := dashboard.RunAll(ctx, c, d, dashboard.RunOptions{Variables: vars})
	if err != nil {
		return err
	}
	format := resolveFormat()
	if format == output.FormatCSV {
		return fmt.Errorf("dashboard run does not support csv output")
	}
	if format == output.FormatMarkdown {
		_, err = fmt.Fprint(os.Stdout, renderDashboardRunMarkdown(d.Title, results))
		return err
	}
	return output.PrintRawJSON(os.Stdout, map[string]any{
		"dashboard": d.Title,
		"variables": vars,
		"results":   results,
	})
}

func runDashboardView(_ *cobra.Command, _ []string) error {
	if dashboardFile == "-" {
		return fmt.Errorf("dashboard view requires a file or directory path")
	}
	c, err := newClient()
	if err != nil {
		return err
	}
	addr, err := dashboardAddr(dashboardPort)
	if err != nil {
		return err
	}
	server, err := dashboard.NewServer(dashboardFile, c.BaseURL, c)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	url := "http://" + addr
	stderr("dashboard serving %s", url)
	if dashboardOpen {
		openBrowser(url)
	}
	err = server.ListenAndServe(ctx, addr)
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func parseDashboardVars(items []string) (map[string]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	vars := make(map[string]string, len(items))
	for _, item := range items {
		name, value, ok := strings.Cut(item, "=")
		if !ok || name == "" {
			return nil, fmt.Errorf("invalid --var %q, expected name=value", item)
		}
		vars[name] = value
	}
	return vars, nil
}

func renderDashboardRunMarkdown(title string, results []dashboard.CardResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	for _, card := range results {
		if card.Skipped {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n", card.Title)
		for _, series := range card.Series {
			if len(card.Series) > 1 {
				fmt.Fprintf(&b, "### %s\n\n", series.Name)
			}
			fmt.Fprintf(&b, "`%s`\n\n", series.Query)
			if series.Error != "" {
				fmt.Fprintf(&b, "Error: %s\n\n", series.Error)
				continue
			}
			if series.Response == nil {
				b.WriteString("No response.\n\n")
				continue
			}
			b.WriteString(markdownRows(series.Response.Columns, series.Response.Rows))
			b.WriteString("\n")
		}
	}
	return b.String()
}

func markdownRows(columns []string, rows []map[string]any) string {
	if len(columns) == 0 {
		return "No rows.\n"
	}
	var b strings.Builder
	for _, col := range columns {
		fmt.Fprintf(&b, "| %s ", col)
	}
	b.WriteString("|\n")
	for range columns {
		b.WriteString("| --- ")
	}
	b.WriteString("|\n")
	for _, row := range rows {
		for _, col := range columns {
			fmt.Fprintf(&b, "| %v ", row[col])
		}
		b.WriteString("|\n")
	}
	return b.String()
}

func loadDashboardInput(path string) (*dashboard.Dashboard, []byte, error) {
	if path == "-" {
		return dashboard.Load(os.Stdin)
	}
	return dashboard.LoadFile(path)
}

func dashboardAddr(port int) (string, error) {
	if port == 0 {
		return dashboard.FirstFreeAddr()
	}
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("invalid port %d", port)
	}
	return "127.0.0.1:" + strconv.Itoa(port), nil
}

func resolveDashboardToken() (string, error) {
	var token string
	if dashboardTokenStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read token from stdin: %w", err)
		}
		token = strings.TrimSpace(string(data))
	} else {
		token = strings.TrimSpace(os.Getenv(dashboardTokenEnv))
	}
	if token == "" {
		return "", fmt.Errorf("interactive export requires an aat_ token via --token-env or --token-stdin")
	}
	return token, nil
}

func openBrowser(rawURL string) {
	if _, err := url.ParseRequestURI(rawURL); err != nil {
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	_ = cmd.Start()
}
