package dashboard

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// ResolveDynamicVariables fills select variable options from WireLog queries.
func ResolveDynamicVariables(ctx context.Context, qc QueryClient, d *Dashboard) error {
	if d == nil || qc == nil {
		return Validate(d)
	}
	for name, variable := range d.Variables {
		if strings.TrimSpace(variable.Query) == "" {
			continue
		}
		rendered, err := RenderQueryTemplate(variable.Query, d, nil)
		if err != nil {
			return fmt.Errorf("resolve variable %q query template: %w", name, err)
		}
		resp, err := qc.QueryJSON(ctx, rendered, 1000, 0)
		if err != nil {
			return fmt.Errorf("resolve variable %q options: %w", name, err)
		}
		options, err := variableOptionsFromResponse(variable, fromClientResponse(resp))
		if err != nil {
			return fmt.Errorf("resolve variable %q options: %w", name, err)
		}
		variable.Options = options
		d.Variables[name] = variable
	}
	return Validate(d)
}

func variableOptionsFromResponse(variable Variable, resp *QueryResponse) ([]VariableOption, error) {
	options := append([]VariableOption(nil), variable.Options...)
	seen := make(map[string]struct{}, len(options))
	for _, opt := range options {
		seen[opt.Value] = struct{}{}
	}
	if resp == nil || len(resp.Columns) == 0 {
		return options, nil
	}
	valueColumn := variable.ValueColumn
	if valueColumn == "" {
		valueColumn = resp.Columns[0]
	} else {
		valueColumn = responseColumn(resp.Columns, valueColumn)
	}
	labelColumn := variable.LabelColumn
	if labelColumn == "" {
		labelColumn = valueColumn
	} else {
		labelColumn = responseColumn(resp.Columns, labelColumn)
	}
	dynamicOptions := make([]VariableOption, 0, len(resp.Rows))
	for _, row := range resp.Rows {
		value, ok := stringCell(row, valueColumn)
		if !ok || value == "" {
			continue
		}
		if !optionValueRE.MatchString(value) {
			return nil, fmt.Errorf("value %q is not safe for a variable option", value)
		}
		if _, ok = seen[value]; ok {
			continue
		}
		label, ok := stringCell(row, labelColumn)
		if !ok || label == "" {
			label = value
		}
		dynamicOptions = append(dynamicOptions, VariableOption{
			Label:    label,
			Value:    value,
			Fragment: renderVariableFragment(variable.FragmentTemplate, value),
		})
		seen[value] = struct{}{}
	}
	sort.SliceStable(dynamicOptions, func(i, j int) bool {
		if dynamicOptions[i].Label == dynamicOptions[j].Label {
			return dynamicOptions[i].Value < dynamicOptions[j].Value
		}
		return dynamicOptions[i].Label < dynamicOptions[j].Label
	})
	return append(options, dynamicOptions...), nil
}

func responseColumn(columns []string, wanted string) string {
	for _, col := range columns {
		if col == wanted {
			return col
		}
	}
	for _, col := range columns {
		if displayResponseColumn(col) == wanted {
			return col
		}
	}
	return wanted
}

func displayResponseColumn(column string) string {
	if strings.HasPrefix(column, "arrayElement(event_properties, '") && strings.HasSuffix(column, "')") {
		return strings.TrimSuffix(strings.TrimPrefix(column, "arrayElement(event_properties, '"), "')")
	}
	if strings.HasPrefix(column, "event_properties_") {
		return strings.TrimPrefix(column, "event_properties_")
	}
	return column
}

func stringCell(row map[string]any, column string) (string, bool) {
	if row == nil {
		return "", false
	}
	value, ok := row[column]
	if !ok || value == nil {
		return "", false
	}
	return strings.TrimSpace(fmt.Sprint(value)), true
}

func renderVariableFragment(template, value string) string {
	if template == "" {
		return ""
	}
	out := strings.ReplaceAll(template, "{{raw_value}}", value)
	out = strings.ReplaceAll(out, "{{value}}", escapeQueryString(value))
	return out
}

func escapeQueryString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}
