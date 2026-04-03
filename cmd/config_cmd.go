package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wirelogai/wirelog-cli/internal/config"
	"github.com/wirelogai/wirelog-cli/internal/output"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage CLI configuration",
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Interactive setup",
	Long: `Set up the CLI with your API key and host.

In non-TTY mode, use flags or environment variables:
  WIRELOG_API_KEY=sk_xxx wl config init
  wl config init --api-key sk_xxx --host https://api.wirelog.ai`,
	RunE: runConfigInit,
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a config value",
	Long: `Set a configuration value. Valid keys: api-key, host.

Examples:
  wl config set api-key sk_xxx
  wl config set host https://api.wirelog.ai`,
	Args: cobra.ExactArgs(2),
	RunE: runConfigSet,
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a config value",
	Long: `Get a configuration value. Valid keys: api-key, host.

Secrets are masked by default.`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigGet,
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show all configuration",
	RunE:  runConfigList,
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print config file path",
	RunE:  runConfigPath,
}

func init() {
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configListCmd)
	configCmd.AddCommand(configPathCmd)
	rootCmd.AddCommand(configCmd)
}

func runConfigInit(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		cfg = &config.Config{}
	}

	apiKey := flagAPIKey
	if apiKey == "" {
		apiKey = os.Getenv("WIRELOG_API_KEY")
	}
	host := flagHost
	if host == "" {
		host = os.Getenv("WIRELOG_HOST")
	}

	// Interactive prompts for TTY
	reader := bufio.NewReader(os.Stdin)
	if apiKey == "" {
		existing := ""
		if cfg.APIKey != "" {
			existing = " [" + config.MaskKey(cfg.APIKey) + "]"
		}
		fmt.Fprintf(os.Stderr, "API key%s: ", existing)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input != "" {
			apiKey = input
		} else {
			apiKey = cfg.APIKey
		}
	}

	if host == "" {
		existing := config.DefaultHost
		if cfg.Host != "" {
			existing = cfg.Host
		}
		fmt.Fprintf(os.Stderr, "Host [%s]: ", existing)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input != "" {
			host = input
		} else {
			host = existing
		}
	}

	if apiKey == "" {
		return fmt.Errorf("API key is required. Get one from your WireLog dashboard")
	}

	cfg.APIKey = apiKey
	cfg.Host = host

	err = config.Save(cfg)
	if err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	// Test connection
	stderr("Config saved to %s", config.GlobalPath())
	stderr("Testing connection...")

	testClient := newClientNoAuth()
	testClient.BaseURL = strings.TrimRight(host, "/")
	testClient.APIKey = apiKey

	ctx, cancel := cmdContext()
	defer cancel()

	status, err := testClient.Health(ctx)
	if err != nil || !status.Healthy {
		stderr("Warning: could not reach %s. Check your host setting.", host)
	} else {
		stderr("Connected to %s", host)
	}

	stderr("")
	stderr("Next steps:")
	stderr("  wl inspect                              # discover events")
	stderr("  wl query \"* | last 7d | count\"           # run a query")
	stderr("  wl track page_view --user-id u1          # send an event")

	return nil
}

func runConfigSet(_ *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		cfg = &config.Config{}
	}

	key, value := args[0], args[1]

	switch key {
	case "api-key":
		cfg.APIKey = value
	case "host":
		cfg.Host = value
	default:
		return fmt.Errorf("unknown config key %q. Valid keys: api-key, host", key)
	}

	err = config.Save(cfg)
	if err != nil {
		return err
	}

	stderr("Set %s", key)
	return nil
}

func runConfigGet(_ *cobra.Command, args []string) error {
	cfg := config.Resolve(flagAPIKey, flagHost)

	switch args[0] {
	case "api-key":
		fmt.Println(config.MaskKey(cfg.APIKey))
	case "host":
		fmt.Println(cfg.Host)
	default:
		return fmt.Errorf("unknown config key %q. Valid keys: api-key, host", args[0])
	}
	return nil
}

func runConfigList(_ *cobra.Command, _ []string) error {
	cfg := config.Resolve(flagAPIKey, flagHost)
	format := resolveFormat()

	if format == output.FormatJSON {
		return output.PrintRawJSON(os.Stdout, map[string]string{
			"api_key": config.MaskKey(cfg.APIKey),
			"host":    cfg.Host,
			"source":  cfg.Source,
		})
	}

	_, _ = fmt.Fprintf(os.Stdout, "  api-key:  %s (%s)\n", config.MaskKey(cfg.APIKey), cfg.Source)
	_, _ = fmt.Fprintf(os.Stdout, "  host:     %s\n", cfg.Host)
	_, _ = fmt.Fprintf(os.Stdout, "  config:   %s\n", config.GlobalPath())
	return nil
}

func runConfigPath(_ *cobra.Command, _ []string) error {
	fmt.Println(config.GlobalPath())
	return nil
}
