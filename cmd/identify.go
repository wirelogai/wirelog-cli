package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wirelogai/wirelog-cli/internal/client"
	"github.com/wirelogai/wirelog-cli/internal/output"
)

var (
	identifyUserID   string
	identifyDeviceID string
	identifyProps    []string
	identifySetOnce  []string
	identifyAdd      []string
	identifyUnset    []string
	identifyDryRun   bool
)

var identifyCmd = &cobra.Command{
	Use:   "identify",
	Short: "Identify a user and set profile properties",
	Long: `Bind a device to a user and update user profile properties.

Requires a public key (pk_), secret key (sk_), or access token with track scope.

Examples:
  wl identify --user-id u123 --prop name="Jane Doe" --prop plan=enterprise
  wl identify --user-id u123 --device-id d456
  wl identify --user-id u123 --set-once signup_source=ads --add login_count=1`,
	RunE: runIdentify,
}

func init() {
	f := identifyCmd.Flags()
	f.StringVar(&identifyUserID, "user-id", "", "User ID (required)")
	f.StringVar(&identifyDeviceID, "device-id", "", "Device ID")
	f.StringArrayVar(&identifyProps, "prop", nil, "User property to $set (key=value, repeatable)")
	f.StringArrayVar(&identifySetOnce, "set-once", nil, "User property to $set_once (key=value, repeatable)")
	f.StringArrayVar(&identifyAdd, "add", nil, "Numeric user property to $add (key=value, repeatable)")
	f.StringArrayVar(&identifyUnset, "unset", nil, "User property to $unset (repeatable)")
	f.BoolVar(&identifyDryRun, "dry-run", false, "Print request body without sending")
	_ = identifyCmd.MarkFlagRequired("user-id")
	rootCmd.AddCommand(identifyCmd)
}

func runIdentify(_ *cobra.Command, _ []string) error {
	params := client.IdentifyParams{
		UserID:   identifyUserID,
		DeviceID: identifyDeviceID,
	}

	// Build user_properties from --prop flags ($set semantics)
	if len(identifyProps) > 0 {
		props, err := parseKeyValueFlags(identifyProps)
		if err != nil {
			return fmt.Errorf("invalid --prop: %w", err)
		}
		params.UserProperties = props
	}

	// Build user_property_ops
	hasOps := len(identifySetOnce) > 0 || len(identifyAdd) > 0 || len(identifyUnset) > 0
	if hasOps {
		ops := &client.UserPropertyOps{}

		if len(identifySetOnce) > 0 {
			so, err := parseKeyValueFlags(identifySetOnce)
			if err != nil {
				return fmt.Errorf("invalid --set-once: %w", err)
			}
			ops.SetOnce = so
		}

		if len(identifyAdd) > 0 {
			add := make(map[string]float64, len(identifyAdd))
			for _, kv := range identifyAdd {
				parts := strings.SplitN(kv, "=", 2)
				if len(parts) != 2 {
					return fmt.Errorf("invalid --add: expected key=value, got %q", kv)
				}
				v, err := strconv.ParseFloat(parts[1], 64)
				if err != nil {
					return fmt.Errorf("invalid --add value for %q: must be a number", parts[0])
				}
				add[parts[0]] = v
			}
			ops.Add = add
		}

		if len(identifyUnset) > 0 {
			ops.Unset = identifyUnset
		}

		params.UserPropertyOps = ops
	}

	if identifyDryRun {
		return output.PrintRawJSON(os.Stdout, params)
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	ctx, cancel := cmdContext()
	defer cancel()

	err = c.Identify(ctx, params)
	if err != nil {
		handleAPIError(err, "identify")
		return err
	}

	format := resolveFormat()
	if format == output.FormatJSON {
		return output.PrintRawJSON(os.Stdout, map[string]any{"ok": true})
	}
	stderr("Identified user %s", identifyUserID)
	return nil
}
