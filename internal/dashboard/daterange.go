package dashboard

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	defaultDateRangeName  = "range"
	defaultDateRangeValue = "30d"
	customDateRangeValue  = "custom"
)

var (
	dateRangeDurationRE = regexp.MustCompile(`^[1-9][0-9]*[hdwmy]$`)
	customDateRangeRE   = regexp.MustCompile(`^custom:(\d{4}-\d{2}-\d{2})\.\.(\d{4}-\d{2}-\d{2})$`)
	legacyRangeLastRE   = regexp.MustCompile(`\|\s*last\s+(today|yesterday|this_week|this_month|this_quarter|this_year|last_month|custom:\d{4}-\d{2}-\d{2}\.\.\d{4}-\d{2}-\d{2})`)
)

var defaultDateRangeOptions = []VariableOption{
	{Label: "7 days", Value: "7d"},
	{Label: "30 days", Value: "30d"},
	{Label: "90 days", Value: "90d"},
	{Label: "This month", Value: "this_month"},
	{Label: "Last month", Value: "last_month"},
	{Label: "Custom", Value: customDateRangeValue},
}

func normalizeDashboard(d *Dashboard) {
	if d == nil {
		return
	}
	if d.Variables == nil {
		d.Variables = map[string]Variable{}
	}
	if variable, ok := d.Variables[defaultDateRangeName]; !ok {
		d.Variables[defaultDateRangeName] = defaultDateRangeVariable()
	} else if shouldUpgradeDateRangeVariable(variable) {
		d.Variables[defaultDateRangeName] = normalizeDateRangeVariable(variable)
	}
	for name, variable := range d.Variables {
		if variable.Type == VariableDateRange {
			d.Variables[name] = normalizeDateRangeVariable(variable)
		}
	}
}

func defaultDateRangeVariable() Variable {
	return normalizeDateRangeVariable(Variable{
		Label:   "Range",
		Type:    VariableDateRange,
		Default: defaultDateRangeValue,
	})
}

func normalizeDateRangeVariable(variable Variable) Variable {
	variable.Type = VariableDateRange
	if variable.Label == "" {
		variable.Label = "Range"
	}
	if variable.Default == "" {
		variable.Default = defaultDateRangeValue
	}
	variable.Options = mergeDateRangeOptions(variable.Options)
	return variable
}

func shouldUpgradeDateRangeVariable(variable Variable) bool {
	if variable.Type != VariableSelect || strings.TrimSpace(variable.Query) != "" || variable.FragmentTemplate != "" || variable.Input != "" || len(variable.Fragments) > 0 {
		return false
	}
	for _, opt := range variable.Options {
		if opt.Fragment != "" || !validDateRangeOptionValue(opt.Value) {
			return false
		}
	}
	return true
}

func mergeDateRangeOptions(options []VariableOption) []VariableOption {
	merged := append([]VariableOption(nil), options...)
	seen := make(map[string]struct{}, len(merged))
	for _, opt := range merged {
		seen[opt.Value] = struct{}{}
	}
	for _, opt := range defaultDateRangeOptions {
		if _, ok := seen[opt.Value]; ok {
			continue
		}
		merged = append(merged, opt)
	}
	return merged
}

func validDateRangeOptionValue(value string) bool {
	if value == customDateRangeValue {
		return true
	}
	_, err := dateRangeStage(value, time.Now())
	return err == nil
}

func dateRangeStage(value string, now time.Time) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultDateRangeValue
	}
	if dateRangeDurationRE.MatchString(value) {
		return "| last " + value, nil
	}
	switch value {
	case "today":
		return "| today", nil
	case "yesterday":
		return "| yesterday", nil
	case "this_week":
		return "| this week", nil
	case "this_month":
		return "| this month", nil
	case "this_quarter":
		return "| this quarter", nil
	case "this_year":
		return "| this year", nil
	case "last_month":
		start, end := lastMonthRange(now)
		return fmt.Sprintf("| from %s to %s", formatDateRangeDate(start), formatDateRangeDate(end)), nil
	}
	if match := customDateRangeRE.FindStringSubmatch(value); match != nil {
		start, err := parseDateRangeDate(match[1])
		if err != nil {
			return "", err
		}
		end, err := parseDateRangeDate(match[2])
		if err != nil {
			return "", err
		}
		if !end.After(start) {
			return "", fmt.Errorf("custom date range end must be after start")
		}
		return fmt.Sprintf("| from %s to %s", match[1], match[2]), nil
	}
	return "", fmt.Errorf("unsupported date range %q", value)
}

func normalizeLegacyDateRangeLastStages(query string) (string, error) {
	var renderErr error
	out := legacyRangeLastRE.ReplaceAllStringFunc(query, func(match string) string {
		if renderErr != nil {
			return match
		}
		parts := strings.Fields(match)
		if len(parts) != 3 {
			return match
		}
		stage, err := dateRangeStage(parts[2], time.Now())
		if err != nil {
			renderErr = err
			return match
		}
		return stage
	})
	if renderErr != nil {
		return "", renderErr
	}
	return out, nil
}

func lastMonthRange(now time.Time) (time.Time, time.Time) {
	local := now.In(now.Location())
	year, month, _ := local.Date()
	end := time.Date(year, month, 1, 0, 0, 0, 0, local.Location())
	start := end.AddDate(0, -1, 0)
	return start, end
}

func parseDateRangeDate(value string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q", value)
	}
	return parsed, nil
}

func formatDateRangeDate(value time.Time) string {
	return value.Format("2006-01-02")
}
