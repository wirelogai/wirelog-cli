package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wirelogai/wirelog-cli/internal/client"
	"github.com/wirelogai/wirelog-cli/internal/output"
)

var (
	trackUserID    string
	trackDeviceID  string
	trackSessionID string
	trackProps     []string
	trackUserProps []string
	trackPropJSON  string
	trackStdin     bool
	trackBatchSize int
	trackDryRun    bool
)

var trackCmd = &cobra.Command{
	Use:   "track <event_type>",
	Short: "Send tracking events",
	Long: `Send one or more tracking events to WireLog.

Requires a public key (pk_), secret key (sk_), or access token with track scope.

Examples:
  wl track page_view --user-id u123 --prop path=/home --prop referrer=google
  wl track signup --user-id u456 --prop-json '{"plan":"pro","seats":5}'
  cat events.jsonl | wl track --stdin
  echo '{"event_type":"click","user_id":"u1"}' | wl track --stdin`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTrack,
}

func init() {
	f := trackCmd.Flags()
	f.StringVar(&trackUserID, "user-id", "", "User ID")
	f.StringVar(&trackDeviceID, "device-id", "", "Device ID")
	f.StringVar(&trackSessionID, "session-id", "", "Session ID")
	f.StringArrayVar(&trackProps, "prop", nil, "Event property as key=value (repeatable)")
	f.StringArrayVar(&trackUserProps, "user-prop", nil, "User property as key=value (repeatable)")
	f.StringVar(&trackPropJSON, "prop-json", "", "Event properties as JSON object")
	f.BoolVar(&trackStdin, "stdin", false, "Read events as JSONL from stdin")
	f.IntVar(&trackBatchSize, "batch-size", 100, "Batch size for stdin mode")
	f.BoolVar(&trackDryRun, "dry-run", false, "Print request body without sending")
	rootCmd.AddCommand(trackCmd)
}

func runTrack(_ *cobra.Command, args []string) error {
	if trackStdin {
		return runTrackStdin()
	}

	if len(args) == 0 {
		return fmt.Errorf("event_type is required (or use --stdin for JSONL input)")
	}

	event := client.TrackEvent{
		EventType: args[0],
		UserID:    trackUserID,
		DeviceID:  trackDeviceID,
		SessionID: trackSessionID,
		Origin:    "server",
	}

	// Parse --prop flags
	props, err := parseKeyValueFlags(trackProps)
	if err != nil {
		return fmt.Errorf("invalid --prop: %w", err)
	}

	// Parse --prop-json
	if trackPropJSON != "" {
		var jsonProps map[string]any
		err = json.Unmarshal([]byte(trackPropJSON), &jsonProps)
		if err != nil {
			return fmt.Errorf("invalid --prop-json: %w", err)
		}
		// JSON props win on conflict
		for k, v := range jsonProps {
			props[k] = v
		}
	}
	if len(props) > 0 {
		event.EventProperties = props
	}

	// Parse --user-prop flags
	userProps, err := parseKeyValueFlags(trackUserProps)
	if err != nil {
		return fmt.Errorf("invalid --user-prop: %w", err)
	}
	if len(userProps) > 0 {
		event.UserProperties = userProps
	}

	events := []client.TrackEvent{event}

	if trackDryRun {
		return output.PrintRawJSON(os.Stdout, map[string]any{"events": events})
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	ctx, cancel := cmdContext()
	defer cancel()

	resp, err := c.Track(ctx, events)
	if err != nil {
		handleAPIError(err, "track")
		return err
	}

	format := resolveFormat()
	if format == output.FormatJSON {
		return output.PrintRawJSON(os.Stdout, resp)
	}
	stderr("Accepted: %d event(s)", resp.Accepted)
	return nil
}

func runTrackStdin() error {
	c, clientErr := newClient()
	if clientErr != nil && !trackDryRun {
		return clientErr
	}

	scanner := bufio.NewScanner(os.Stdin)
	// Increase buffer for large lines
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	var batch []client.TrackEvent
	totalAccepted := 0

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}

		if trackDryRun {
			return output.PrintRawJSON(os.Stdout, map[string]any{"events": batch})
		}

		ctx, cancel := cmdContext()
		defer cancel()

		resp, err := c.Track(ctx, batch)
		if err != nil {
			return err
		}
		totalAccepted += resp.Accepted
		batch = batch[:0]
		return nil
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event client.TrackEvent
		err := json.Unmarshal([]byte(line), &event)
		if err != nil {
			stderr("Skipping invalid line: %v", err)
			continue
		}
		if event.EventType == "" {
			stderr("Skipping event with no event_type")
			continue
		}

		batch = append(batch, event)
		if len(batch) >= trackBatchSize {
			err = flush()
			if err != nil {
				handleAPIError(err, "track")
				return err
			}
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return fmt.Errorf("read stdin: %w", scanErr)
	}

	// Flush remaining
	err := flush()
	if err != nil {
		handleAPIError(err, "track")
		return err
	}

	if !trackDryRun {
		format := resolveFormat()
		if format == output.FormatJSON {
			return output.PrintRawJSON(os.Stdout, map[string]any{"accepted": totalAccepted})
		}
		stderr("Accepted: %d event(s)", totalAccepted)
	}
	return nil
}

func parseKeyValueFlags(flags []string) (map[string]any, error) {
	result := make(map[string]any, len(flags))
	for _, kv := range flags {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("expected key=value, got %q", kv)
		}
		result[parts[0]] = parts[1]
	}
	return result, nil
}
