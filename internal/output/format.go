// Package output handles rendering CLI results in multiple formats.
package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// Format represents an output format.
type Format string

const (
	FormatAuto     Format = "auto"
	FormatTable    Format = "table"
	FormatJSON     Format = "json"
	FormatCSV      Format = "csv"
	FormatMarkdown Format = "markdown"
)

// Detect returns table if stdout is a TTY, json otherwise.
func Detect() Format {
	if term.IsTerminal(int(os.Stdout.Fd())) {
		return FormatTable
	}
	return FormatJSON
}

// ResolveFormat resolves "auto" to a concrete format.
func ResolveFormat(f Format) Format {
	if f == FormatAuto || f == "" {
		return Detect()
	}
	return f
}

// ServerFormat maps a CLI output format to the server's format parameter.
func ServerFormat(f Format) string {
	switch f {
	case FormatMarkdown:
		return "llm"
	case FormatCSV:
		return "csv"
	default:
		return "json"
	}
}

// PrintQueryResult renders query results to the writer.
// For table format, it parses the JSON and renders a styled table.
// For json/csv/markdown, it writes the raw server response.
func PrintQueryResult(w io.Writer, format Format, data []byte, noColor bool) error {
	switch format {
	case FormatTable:
		return printQueryTable(w, data, noColor)
	case FormatJSON:
		// Pretty-print the JSON
		var raw json.RawMessage
		err := json.Unmarshal(data, &raw)
		if err != nil {
			// Not valid JSON, write as-is
			_, writeErr := w.Write(data)
			return writeErr
		}
		pretty, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			_, writeErr := w.Write(data)
			return writeErr
		}
		_, err = fmt.Fprintln(w, string(pretty))
		return err
	case FormatCSV, FormatMarkdown:
		// Pass through raw server response
		_, err := w.Write(data)
		if err != nil {
			return err
		}
		// Ensure trailing newline
		if len(data) > 0 && data[len(data)-1] != '\n' {
			_, err = fmt.Fprintln(w)
		}
		return err
	default:
		_, err := w.Write(data)
		return err
	}
}

// PrintStructured renders non-query data (projects, orgs, health, etc.) in the given format.
func PrintStructured(w io.Writer, format Format, headers []string, rows [][]string, noColor bool) error {
	switch format {
	case FormatTable:
		return printTable(w, headers, rows, noColor)
	case FormatJSON:
		return printStructuredJSON(w, headers, rows)
	case FormatCSV:
		return printCSV(w, headers, rows)
	default:
		return printTable(w, headers, rows, noColor)
	}
}

// PrintSingle renders a single object as JSON or key-value pairs.
func PrintSingle(w io.Writer, format Format, data map[string]string, noColor bool) error {
	switch format {
	case FormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	default:
		return printKeyValue(w, data, noColor)
	}
}

// PrintRawJSON prints a pre-formed JSON value with indentation.
func PrintRawJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func printStructuredJSON(w io.Writer, headers []string, rows [][]string) error {
	result := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		obj := make(map[string]string, len(headers))
		for i, h := range headers {
			if i < len(row) {
				obj[h] = row[i]
			}
		}
		result = append(result, obj)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func printCSV(w io.Writer, headers []string, rows [][]string) error {
	cw := csv.NewWriter(w)
	err := cw.Write(headers)
	if err != nil {
		return err
	}
	for _, row := range rows {
		err = cw.Write(row)
		if err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func printQueryTable(w io.Writer, data []byte, noColor bool) error {
	var resp struct {
		Columns   []string         `json:"columns"`
		Rows      []map[string]any `json:"rows"`
		Total     int              `json:"total"`
		ElapsedMs int64            `json:"elapsed_ms"`
		Mode      string           `json:"mode"`
	}
	err := json.Unmarshal(data, &resp)
	if err != nil {
		// Fall back to raw output
		_, writeErr := w.Write(data)
		return writeErr
	}

	if len(resp.Rows) == 0 {
		_, err = fmt.Fprintln(w, dim("No results.", noColor))
		return err
	}

	headers := resp.Columns
	if len(headers) == 0 && len(resp.Rows) > 0 {
		for k := range resp.Rows[0] {
			headers = append(headers, k)
		}
	}

	rows := make([][]string, 0, len(resp.Rows))
	for _, row := range resp.Rows {
		r := make([]string, len(headers))
		for i, h := range headers {
			r[i] = formatAny(row[h])
		}
		rows = append(rows, r)
	}

	err = printTable(w, headers, rows, noColor)
	if err != nil {
		return err
	}

	// Footer
	footer := fmt.Sprintf("%d rows", len(resp.Rows))
	if resp.Total > len(resp.Rows) {
		footer += fmt.Sprintf(" of %d total", resp.Total)
	}
	if resp.ElapsedMs > 0 {
		footer += fmt.Sprintf(" · %dms", resp.ElapsedMs)
	}
	_, err = fmt.Fprintln(w, dim(footer, noColor))
	return err
}

func formatAny(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%.2f", val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case json.Number:
		return val.String()
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}

func printKeyValue(w io.Writer, data map[string]string, noColor bool) error {
	maxKeyLen := 0
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
		if len(k) > maxKeyLen {
			maxKeyLen = len(k)
		}
	}

	for _, k := range keys {
		label := green(k+":", noColor)
		padding := strings.Repeat(" ", maxKeyLen-len(k)+1)
		_, err := fmt.Fprintf(w, "  %s%s%s\n", label, padding, data[k])
		if err != nil {
			return err
		}
	}
	return nil
}
