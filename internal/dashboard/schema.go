// Package dashboard loads, validates, renders, and serves WireLog dashboards.
package dashboard

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Dashboard is the root YAML dashboard document.
type Dashboard struct {
	Version   int                 `yaml:"version" json:"version"`
	Title     string              `yaml:"title" json:"title"`
	Order     int                 `yaml:"order,omitempty" json:"order,omitempty"`
	Refresh   string              `yaml:"refresh,omitempty" json:"refresh,omitempty"`
	Timezone  string              `yaml:"timezone,omitempty" json:"timezone,omitempty"`
	Variables map[string]Variable `yaml:"variables,omitempty" json:"variables,omitempty"`
	Sections  []Section           `yaml:"sections" json:"sections"`
}

// Section groups cards under a dashboard heading.
type Section struct {
	Title string `yaml:"title" json:"title"`
	Cards []Card `yaml:"cards" json:"cards"`
}

// Variable is a dashboard-level anchor variable.
type Variable struct {
	Label            string                   `yaml:"label,omitempty" json:"label,omitempty"`
	Type             VariableType             `yaml:"type" json:"type"`
	Default          string                   `yaml:"default,omitempty" json:"default,omitempty"`
	Options          []VariableOption         `yaml:"options,omitempty" json:"options,omitempty"`
	Query            string                   `yaml:"query,omitempty" json:"query,omitempty"`
	ValueColumn      string                   `yaml:"value_column,omitempty" json:"value_column,omitempty"`
	LabelColumn      string                   `yaml:"label_column,omitempty" json:"label_column,omitempty"`
	FragmentTemplate string                   `yaml:"fragment_template,omitempty" json:"fragment_template,omitempty"`
	Input            string                   `yaml:"input,omitempty" json:"input,omitempty"`
	Placeholder      string                   `yaml:"placeholder,omitempty" json:"placeholder,omitempty"`
	Submit           bool                     `yaml:"submit,omitempty" json:"submit,omitempty"`
	Required         bool                     `yaml:"required,omitempty" json:"required,omitempty"`
	AllowDomain      bool                     `yaml:"allow_domain_wildcard,omitempty" json:"allow_domain_wildcard,omitempty"`
	AllowGlob        bool                     `yaml:"allow_glob,omitempty" json:"allow_glob,omitempty"`
	Fragments        map[string]InputFragment `yaml:"fragments,omitempty" json:"fragments,omitempty"`
}

// VariableType is the supported variable control type.
type VariableType string

const (
	// VariableSelect is a whitelisted dropdown/select variable.
	VariableSelect VariableType = "select"
	// VariableInput is a submitted text input with safe fragment generation.
	VariableInput VariableType = "input"
)

// VariableOption is one allowed value for a variable.
type VariableOption struct {
	Label    string `yaml:"label" json:"label"`
	Value    string `yaml:"value" json:"value"`
	Fragment string `yaml:"fragment,omitempty" json:"fragment,omitempty"`
}

// InputFragment maps an input value to a source-specific safe where fragment.
type InputFragment struct {
	ExactField  string `yaml:"exact_field" json:"exact_field"`
	DomainField string `yaml:"domain_field,omitempty" json:"domain_field,omitempty"`
}

// CardKind identifies the semantic card type.
type CardKind string

const (
	// CardChart renders one or more query results as a chart.
	CardChart CardKind = "chart"
	// CardMetric renders a scalar metric.
	CardMetric CardKind = "metric"
	// CardTable renders rows and columns.
	CardTable CardKind = "table"
	// CardEvents renders event/list rows.
	CardEvents CardKind = "events"
	// CardFunnel renders funnel query output.
	CardFunnel CardKind = "funnel"
	// CardRetention renders retention query output.
	CardRetention CardKind = "retention"
	// CardJourney renders path/journey query output.
	CardJourney CardKind = "journey"
	// CardSessions renders session query output.
	CardSessions CardKind = "sessions"
	// CardLifecycle renders lifecycle query output.
	CardLifecycle CardKind = "lifecycle"
	// CardStickiness renders stickiness query output.
	CardStickiness CardKind = "stickiness"
	// CardUsers renders users/user query output.
	CardUsers CardKind = "users"
	// CardInspect renders inspect/fields query output.
	CardInspect CardKind = "inspect"
	// CardMarkdown renders a note panel.
	CardMarkdown CardKind = "markdown"
)

// VizKind identifies the visual renderer.
type VizKind string

const (
	// VizLine renders a line chart.
	VizLine VizKind = "line"
	// VizArea renders an area chart.
	VizArea VizKind = "area"
	// VizBar renders a bar chart.
	VizBar VizKind = "bar"
	// VizPie renders a pie chart.
	VizPie VizKind = "pie"
	// VizNumber renders a numeric value.
	VizNumber VizKind = "number"
	// VizTable renders a table.
	VizTable VizKind = "table"
	// VizEventStream renders event rows.
	VizEventStream VizKind = "event-stream"
	// VizFunnel renders a funnel.
	VizFunnel VizKind = "funnel"
	// VizRetentionGrid renders a retention grid.
	VizRetentionGrid VizKind = "retention-grid"
	// VizPath renders path/journey output.
	VizPath VizKind = "path"
	// VizLifecycle renders lifecycle output.
	VizLifecycle VizKind = "lifecycle"
	// VizHistogram renders a histogram.
	VizHistogram VizKind = "histogram"
	// VizJSON renders raw JSON.
	VizJSON VizKind = "json"
	// VizMarkdown renders Markdown notes.
	VizMarkdown VizKind = "markdown"
)

// Layout controls card dimensions in a 12-column section flow.
type Layout struct {
	W int `yaml:"w,omitempty" json:"w,omitempty"`
	H int `yaml:"h,omitempty" json:"h,omitempty"`
}

// NamedQuery is one query series in a card.
type NamedQuery struct {
	Name  string `yaml:"name" json:"name"`
	Query string `yaml:"query" json:"query"`
}

// Card is one dashboard panel.
type Card struct {
	ID       string         `yaml:"id" json:"id"`
	Title    string         `yaml:"title" json:"title"`
	Kind     CardKind       `yaml:"kind" json:"kind"`
	Viz      VizKind        `yaml:"viz" json:"viz"`
	Query    string         `yaml:"query,omitempty" json:"query,omitempty"`
	Queries  []NamedQuery   `yaml:"queries,omitempty" json:"queries,omitempty"`
	Markdown string         `yaml:"markdown,omitempty" json:"markdown,omitempty"`
	Layout   Layout         `yaml:"layout,omitempty" json:"layout,omitempty"`
	Options  map[string]any `yaml:"options,omitempty" json:"options,omitempty"`
}

var (
	errEmptyDashboard = errors.New("empty dashboard")
	nameRE            = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	fieldNameRE       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)
	optionValueRE     = regexp.MustCompile(`^[A-Za-z0-9_./:@+-]+$`)
	cardIDRE          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]*$`)
)

// Load reads dashboard YAML from r.
func Load(r io.Reader) (*Dashboard, []byte, error) {
	src, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, fmt.Errorf("read dashboard: %w", err)
	}
	if len(bytes.TrimSpace(src)) == 0 {
		return nil, nil, errEmptyDashboard
	}
	var d Dashboard
	err = yaml.Unmarshal(src, &d)
	if err != nil {
		return nil, nil, fmt.Errorf("parse dashboard YAML: %w", err)
	}
	return &d, src, nil
}

// LoadFile reads dashboard YAML from path.
func LoadFile(path string) (*Dashboard, []byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open dashboard: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()
	return Load(f)
}

// Marshal returns a stable YAML representation of d.
func Marshal(d *Dashboard) ([]byte, error) {
	out, err := yaml.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("marshal dashboard YAML: %w", err)
	}
	return out, nil
}

// Validate checks schema and semantic constraints.
func Validate(d *Dashboard) error {
	var errs []string
	if d == nil {
		return fmt.Errorf("dashboard is nil")
	}
	if d.Version != 1 {
		errs = append(errs, "version must be 1")
	}
	if strings.TrimSpace(d.Title) == "" {
		errs = append(errs, "title is required")
	}
	validateVariables(d, &errs)
	validateSections(d, &errs)
	if len(errs) > 0 {
		return fmt.Errorf("invalid dashboard:\n- %s", strings.Join(errs, "\n- "))
	}
	return nil
}

func validateVariables(d *Dashboard, errs *[]string) {
	for name, variable := range d.Variables {
		if !nameRE.MatchString(name) {
			*errs = append(*errs, fmt.Sprintf("variable %q has invalid name", name))
		}
		switch variable.Type {
		case VariableSelect:
			validateSelectVariable(name, variable, d, errs)
		case VariableInput:
			validateInputVariable(name, variable, errs)
		default:
			*errs = append(*errs, fmt.Sprintf("variable %q type must be select or input", name))
		}
	}
}

func validateSelectVariable(name string, variable Variable, d *Dashboard, errs *[]string) {
	hasDynamicOptions := strings.TrimSpace(variable.Query) != ""
	if len(variable.Options) == 0 && !hasDynamicOptions {
		*errs = append(*errs, fmt.Sprintf("variable %q must define options or query", name))
		return
	}
	if hasDynamicOptions {
		validateTemplateRefs(fmt.Sprintf("variable %q query", name), variable.Query, d.Variables, errs)
	}
	if strings.ContainsAny(variable.FragmentTemplate, "\n\r`") {
		*errs = append(*errs, fmt.Sprintf("variable %q has unsafe fragment_template", name))
	}
	foundDefault := false
	for _, opt := range variable.Options {
		if opt.Value == "" {
			*errs = append(*errs, fmt.Sprintf("variable %q has option with empty value", name))
		}
		if opt.Value != "" && !optionValueRE.MatchString(opt.Value) {
			*errs = append(*errs, fmt.Sprintf("variable %q option %q has unsafe value", name, opt.Value))
		}
		if strings.ContainsAny(opt.Fragment, "\n\r`") {
			*errs = append(*errs, fmt.Sprintf("variable %q option %q has unsafe fragment", name, opt.Value))
		}
		if opt.Value == variable.Default {
			foundDefault = true
		}
	}
	if variable.Default == "" {
		*errs = append(*errs, fmt.Sprintf("variable %q default is required", name))
	} else if !foundDefault && !hasDynamicOptions {
		*errs = append(*errs, fmt.Sprintf("variable %q default %q is not an option", name, variable.Default))
	}
}

func validateInputVariable(name string, variable Variable, errs *[]string) {
	switch variable.Input {
	case "", "email":
	default:
		*errs = append(*errs, fmt.Sprintf("variable %q input must be email", name))
	}
	if variable.AllowGlob {
		*errs = append(*errs, fmt.Sprintf("variable %q allow_glob is not supported yet", name))
	}
	if variable.Default != "" {
		if _, err := parseEmailInput(variable.Default, variable.AllowDomain); err != nil {
			*errs = append(*errs, fmt.Sprintf("variable %q default is invalid: %s", name, err.Error()))
		}
	}
	if len(variable.Fragments) == 0 {
		*errs = append(*errs, fmt.Sprintf("variable %q input variables must define fragments", name))
	}
	for fragmentName, fragment := range variable.Fragments {
		if !nameRE.MatchString(fragmentName) {
			*errs = append(*errs, fmt.Sprintf("variable %q fragment %q has invalid name", name, fragmentName))
		}
		if !fieldNameRE.MatchString(fragment.ExactField) {
			*errs = append(*errs, fmt.Sprintf("variable %q fragment %q exact_field is required and must be a field name", name, fragmentName))
		}
		if variable.AllowDomain && !fieldNameRE.MatchString(fragment.DomainField) {
			*errs = append(*errs, fmt.Sprintf("variable %q fragment %q domain_field is required for domain wildcards", name, fragmentName))
		}
	}
}

func validateSections(d *Dashboard, errs *[]string) {
	ids := map[string]struct{}{}
	if len(d.Sections) == 0 {
		*errs = append(*errs, "sections are required")
		return
	}
	for si, section := range d.Sections {
		if strings.TrimSpace(section.Title) == "" {
			*errs = append(*errs, fmt.Sprintf("section %d title is required", si+1))
		}
		for ci, card := range section.Cards {
			loc := fmt.Sprintf("section %q card %d", section.Title, ci+1)
			validateCard(loc, card, ids, d.Variables, errs)
		}
	}
}

func validateCard(loc string, card Card, ids map[string]struct{}, variables map[string]Variable, errs *[]string) {
	if !cardIDRE.MatchString(card.ID) {
		*errs = append(*errs, fmt.Sprintf("%s id is required and must be slug-like", loc))
	} else if _, ok := ids[card.ID]; ok {
		*errs = append(*errs, fmt.Sprintf("card id %q is duplicated", card.ID))
	} else {
		ids[card.ID] = struct{}{}
	}
	if strings.TrimSpace(card.Title) == "" {
		*errs = append(*errs, fmt.Sprintf("%s title is required", loc))
	}
	if card.Kind == "" {
		*errs = append(*errs, fmt.Sprintf("%s kind is required", loc))
	}
	if card.Viz == "" {
		*errs = append(*errs, fmt.Sprintf("%s viz is required", loc))
	}
	if card.Layout.W < 0 || card.Layout.W > 12 {
		*errs = append(*errs, fmt.Sprintf("%s layout.w must be 1..12 when set", loc))
	}
	if card.Layout.H < 0 || card.Layout.H > 24 {
		*errs = append(*errs, fmt.Sprintf("%s layout.h must be 1..24 when set", loc))
	}
	hasQuery := strings.TrimSpace(card.Query) != ""
	hasQueries := len(card.Queries) > 0
	hasMarkdown := strings.TrimSpace(card.Markdown) != ""
	switch card.Kind {
	case CardMarkdown:
		if !hasMarkdown {
			*errs = append(*errs, fmt.Sprintf("%s markdown is required", loc))
		}
		if hasQuery || hasQueries {
			*errs = append(*errs, fmt.Sprintf("%s markdown cards cannot define query or queries", loc))
		}
	default:
		if hasQuery == hasQueries {
			*errs = append(*errs, fmt.Sprintf("%s must define exactly one of query or queries", loc))
		}
		if hasQuery {
			validateTemplateRefs(loc, card.Query, variables, errs)
		}
		for qi, q := range card.Queries {
			if strings.TrimSpace(q.Name) == "" {
				*errs = append(*errs, fmt.Sprintf("%s query %d name is required", loc, qi+1))
			}
			if strings.TrimSpace(q.Query) == "" {
				*errs = append(*errs, fmt.Sprintf("%s query %d query is required", loc, qi+1))
			}
			validateTemplateRefs(loc, q.Query, variables, errs)
		}
	}
}
