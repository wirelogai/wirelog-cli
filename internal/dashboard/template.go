package dashboard

import (
	"fmt"
	"regexp"
	"strings"
)

var templateRefRE = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)(?:\.([A-Za-z_][A-Za-z0-9_]*))?\s*\}\}`)
var exactEmailRE = regexp.MustCompile(`^[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}$`)
var domainWildcardRE = regexp.MustCompile(`^\*@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}$`)

type resolvedVariable struct {
	Value     string
	Fragment  string
	Fragments map[string]string
}

type inputValue struct {
	Kind   string
	Value  string
	Domain string
}

// ResolveVariables returns selected values, falling back to defaults.
func ResolveVariables(d *Dashboard, selected map[string]string) (map[string]resolvedVariable, error) {
	resolved := make(map[string]resolvedVariable, len(d.Variables))
	for name, variable := range d.Variables {
		value := variable.Default
		if selected != nil {
			if selectedValue, ok := selected[name]; ok {
				value = selectedValue
			}
		}
		switch variable.Type {
		case VariableSelect:
			if value == "" {
				value = variable.Default
			}
			opt, ok := optionByValue(variable, value)
			if !ok {
				return nil, fmt.Errorf("variable %q value %q is not allowed", name, value)
			}
			resolved[name] = resolvedVariable{Value: opt.Value, Fragment: opt.Fragment}
		case VariableInput:
			input, err := parseEmailInput(value, variable.AllowDomain)
			if err != nil {
				return nil, fmt.Errorf("variable %q value %q is not allowed: %w", name, value, err)
			}
			if variable.Required && input.Kind == "empty" {
				return nil, fmt.Errorf("variable %q is required", name)
			}
			fragments := make(map[string]string, len(variable.Fragments))
			for fragmentName, fragment := range variable.Fragments {
				fragments[fragmentName] = renderInputFragment(input, fragment)
			}
			resolved[name] = resolvedVariable{Value: input.Value, Fragments: fragments}
		default:
			return nil, fmt.Errorf("variable %q has unsupported type %q", name, variable.Type)
		}
	}
	return resolved, nil
}

// RenderQueryTemplate replaces whitelisted variable references in a query.
func RenderQueryTemplate(query string, d *Dashboard, selected map[string]string) (string, error) {
	resolved, err := ResolveVariables(d, selected)
	if err != nil {
		return "", err
	}
	var renderErr error
	out := templateRefRE.ReplaceAllStringFunc(query, func(token string) string {
		if renderErr != nil {
			return token
		}
		match := templateRefRE.FindStringSubmatch(token)
		name := match[1]
		attr := match[2]
		opt, ok := resolved[name]
		if !ok {
			renderErr = fmt.Errorf("unknown variable %q", name)
			return token
		}
		switch attr {
		case "":
			return opt.Value
		case "fragment":
			return opt.Fragment
		default:
			if strings.HasSuffix(attr, "_fragment") {
				fragmentName := strings.TrimSuffix(attr, "_fragment")
				if fragment, ok := opt.Fragments[fragmentName]; ok {
					return fragment
				}
			}
			renderErr = fmt.Errorf("unsupported variable attribute %q in %q", attr, token)
			return token
		}
	})
	if renderErr != nil {
		return "", renderErr
	}
	return strings.Join(strings.Fields(out), " "), nil
}

func validateTemplateRefs(loc, query string, variables map[string]Variable, errs *[]string) {
	matches := templateRefRE.FindAllStringSubmatch(query, -1)
	for _, match := range matches {
		name := match[1]
		attr := match[2]
		variable, ok := variables[name]
		if !ok {
			*errs = append(*errs, fmt.Sprintf("%s references unknown variable %q", loc, name))
			continue
		}
		if attr == "" || attr == "fragment" {
			if attr == "fragment" && variable.Type != VariableSelect {
				*errs = append(*errs, fmt.Sprintf("%s references unsupported variable attribute %q", loc, attr))
			}
			continue
		}
		fragmentName, ok := strings.CutSuffix(attr, "_fragment")
		if !ok || variable.Type != VariableInput {
			*errs = append(*errs, fmt.Sprintf("%s references unsupported variable attribute %q", loc, attr))
			continue
		}
		if _, ok = variable.Fragments[fragmentName]; !ok {
			*errs = append(*errs, fmt.Sprintf("%s references unsupported variable attribute %q", loc, attr))
		}
	}
}

func optionByValue(variable Variable, value string) (VariableOption, bool) {
	for _, opt := range variable.Options {
		if opt.Value == value {
			return opt, true
		}
	}
	return VariableOption{}, false
}

func parseEmailInput(value string, allowDomain bool) (inputValue, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return inputValue{Kind: "empty"}, nil
	}
	if exactEmailRE.MatchString(value) {
		_, domain, _ := strings.Cut(value, "@")
		return inputValue{Kind: "exact", Value: value, Domain: domain}, nil
	}
	if allowDomain && domainWildcardRE.MatchString(value) {
		domain := strings.TrimPrefix(value, "*@")
		return inputValue{Kind: "domain", Value: value, Domain: domain}, nil
	}
	if strings.Contains(value, "*") {
		return inputValue{}, fmt.Errorf("only *@domain.com wildcards are supported")
	}
	return inputValue{}, fmt.Errorf("expected email or *@domain.com")
}

func renderInputFragment(input inputValue, fragment InputFragment) string {
	switch input.Kind {
	case "exact":
		return fmt.Sprintf(`| where %s = "%s"`, fragment.ExactField, escapeQueryString(input.Value))
	case "domain":
		return fmt.Sprintf(`| where %s = "%s"`, fragment.DomainField, escapeQueryString(input.Domain))
	default:
		return ""
	}
}
