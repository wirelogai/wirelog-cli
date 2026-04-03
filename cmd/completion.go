package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completions",
	Long: `Generate shell completion scripts.

Bash:
  source <(wl completion bash)
  # Or permanently:
  wl completion bash > /etc/bash_completion.d/wl

Zsh:
  source <(wl completion zsh)
  # Or permanently:
  wl completion zsh > "${fpath[1]}/_wl"

Fish:
  wl completion fish | source
  # Or permanently:
  wl completion fish > ~/.config/fish/completions/wl.fish

PowerShell:
  wl completion powershell | Out-String | Invoke-Expression`,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
	RunE:      runCompletion,
}

func init() {
	rootCmd.AddCommand(completionCmd)
}

func runCompletion(_ *cobra.Command, args []string) error {
	switch args[0] {
	case "bash":
		return rootCmd.GenBashCompletion(os.Stdout)
	case "zsh":
		return rootCmd.GenZshCompletion(os.Stdout)
	case "fish":
		return rootCmd.GenFishCompletion(os.Stdout, true)
	case "powershell":
		return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
	default:
		return nil
	}
}
