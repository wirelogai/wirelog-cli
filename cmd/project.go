package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wirelogai/wirelog-cli/internal/output"
)

var (
	projectUsageFrom string
	projectUsageTo   string
	projectYes       bool
)

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Manage projects (requires ak_ admin key)",
	Long: `Manage projects via the Admin API.

Requires an org admin key (ak_). Find it in your org settings or dashboard.`,
}

var projectListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all projects",
	RunE:  runProjectList,
}

var projectCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new project",
	Args:  cobra.ExactArgs(1),
	RunE:  runProjectCreate,
}

var projectGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get project details",
	Args:  cobra.ExactArgs(1),
	RunE:  runProjectGet,
}

var projectDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a project (destructive)",
	Args:  cobra.ExactArgs(1),
	RunE:  runProjectDelete,
}

var projectRotateCmd = &cobra.Command{
	Use:   "rotate-secret-key <id>",
	Short: "Rotate the project's secret key",
	Args:  cobra.ExactArgs(1),
	RunE:  runProjectRotate,
}

var projectUsageCmd = &cobra.Command{
	Use:   "usage <id>",
	Short: "Get project usage stats",
	Args:  cobra.ExactArgs(1),
	RunE:  runProjectUsage,
}

func init() {
	projectDeleteCmd.Flags().BoolVar(&projectYes, "yes", false, "Skip confirmation prompt")
	projectRotateCmd.Flags().BoolVar(&projectYes, "yes", false, "Skip confirmation prompt")
	projectUsageCmd.Flags().StringVar(&projectUsageFrom, "from", "", "Start date (YYYY-MM-DD)")
	projectUsageCmd.Flags().StringVar(&projectUsageTo, "to", "", "End date (YYYY-MM-DD)")

	projectCmd.AddCommand(projectListCmd)
	projectCmd.AddCommand(projectCreateCmd)
	projectCmd.AddCommand(projectGetCmd)
	projectCmd.AddCommand(projectDeleteCmd)
	projectCmd.AddCommand(projectRotateCmd)
	projectCmd.AddCommand(projectUsageCmd)
	rootCmd.AddCommand(projectCmd)
}

func runProjectList(_ *cobra.Command, _ []string) error {
	c, err := newClient()
	if err != nil {
		return err
	}

	ctx, cancel := cmdContext()
	defer cancel()

	projects, err := c.ListProjects(ctx)
	if err != nil {
		handleAPIError(err, "admin")
		return err
	}

	format := resolveFormat()

	headers := []string{"ID", "Name", "Public Key", "Created"}
	rows := make([][]string, 0, len(projects))
	for _, p := range projects {
		rows = append(rows, []string{p.ID, p.Name, p.PublicKey, p.CreatedAt})
	}

	return output.PrintStructured(os.Stdout, format, headers, rows, flagNoColor)
}

func runProjectCreate(_ *cobra.Command, args []string) error {
	c, err := newClient()
	if err != nil {
		return err
	}

	ctx, cancel := cmdContext()
	defer cancel()

	project, err := c.CreateProject(ctx, args[0])
	if err != nil {
		handleAPIError(err, "admin")
		return err
	}

	format := resolveFormat()
	if format == output.FormatJSON {
		return output.PrintRawJSON(os.Stdout, project)
	}

	data := map[string]string{
		"ID":         project.ID,
		"Name":       project.Name,
		"Public Key": project.PublicKey,
		"Secret Key": project.SecretKey,
		"Created":    project.CreatedAt,
	}
	stderr("Project created. Save the secret key — it won't be shown again.")
	return output.PrintSingle(os.Stdout, format, data, flagNoColor)
}

func runProjectGet(_ *cobra.Command, args []string) error {
	c, err := newClient()
	if err != nil {
		return err
	}

	ctx, cancel := cmdContext()
	defer cancel()

	project, err := c.GetProject(ctx, args[0])
	if err != nil {
		handleAPIError(err, "admin")
		return err
	}

	format := resolveFormat()
	if format == output.FormatJSON {
		return output.PrintRawJSON(os.Stdout, project)
	}

	data := map[string]string{
		"ID":         project.ID,
		"Name":       project.Name,
		"Public Key": project.PublicKey,
		"Secret Key": project.SecretKey,
		"Created":    project.CreatedAt,
	}
	return output.PrintSingle(os.Stdout, format, data, flagNoColor)
}

func runProjectDelete(_ *cobra.Command, args []string) error {
	c, err := newClient()
	if err != nil {
		return err
	}

	// Need the project name for confirmation
	ctx, cancel := cmdContext()
	defer cancel()

	project, err := c.GetProject(ctx, args[0])
	if err != nil {
		handleAPIError(err, "admin")
		return err
	}

	if !projectYes {
		fmt.Fprintf(os.Stderr, "Delete project %q (%s)? This is permanent.\nType the project name to confirm: ", project.Name, project.ID)
		reader := bufio.NewReader(os.Stdin)
		confirm, _ := reader.ReadString('\n')
		confirm = strings.TrimSpace(confirm)
		if confirm != project.Name {
			return fmt.Errorf("confirmation did not match, aborting")
		}
	}

	ctx2, cancel2 := cmdContext()
	defer cancel2()

	err = c.DeleteProject(ctx2, args[0], project.Name)
	if err != nil {
		handleAPIError(err, "admin")
		return err
	}

	format := resolveFormat()
	if format == output.FormatJSON {
		return output.PrintRawJSON(os.Stdout, map[string]any{"status": "deleted", "id": args[0]})
	}
	stderr("Deleted project %s", args[0])
	return nil
}

func runProjectRotate(_ *cobra.Command, args []string) error {
	if !projectYes {
		fmt.Fprintf(os.Stderr, "Rotate secret key for project %s? Existing key will stop working.\nType 'yes' to confirm: ", args[0])
		reader := bufio.NewReader(os.Stdin)
		confirm, _ := reader.ReadString('\n')
		if strings.TrimSpace(confirm) != "yes" {
			return fmt.Errorf("aborted")
		}
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	ctx, cancel := cmdContext()
	defer cancel()

	project, err := c.RotateSecretKey(ctx, args[0])
	if err != nil {
		handleAPIError(err, "admin")
		return err
	}

	format := resolveFormat()
	if format == output.FormatJSON {
		return output.PrintRawJSON(os.Stdout, project)
	}

	data := map[string]string{
		"ID":             project.ID,
		"Name":           project.Name,
		"Public Key":     project.PublicKey,
		"New Secret Key": project.SecretKey,
	}
	stderr("Secret key rotated. Save the new key — it won't be shown again.")
	return output.PrintSingle(os.Stdout, format, data, flagNoColor)
}

func runProjectUsage(_ *cobra.Command, args []string) error {
	c, err := newClient()
	if err != nil {
		return err
	}

	ctx, cancel := cmdContext()
	defer cancel()

	rows, err := c.GetProjectUsage(ctx, args[0], projectUsageFrom, projectUsageTo)
	if err != nil {
		handleAPIError(err, "admin")
		return err
	}

	format := resolveFormat()

	headers := []string{"Date", "Events Ingested", "Events Stored", "Queries Run"}
	tableRows := make([][]string, 0, len(rows))
	for _, r := range rows {
		tableRows = append(tableRows, []string{
			r.Date,
			fmt.Sprintf("%d", r.EventsIngested),
			fmt.Sprintf("%d", r.EventsStored),
			fmt.Sprintf("%d", r.QueriesRun),
		})
	}

	return output.PrintStructured(os.Stdout, format, headers, tableRows, flagNoColor)
}
