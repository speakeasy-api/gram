// Package catalog holds the platforms the Access Hub offers to trust without
// looking anything up, each with the guided setup that connects it.
//
// An entry carries what an operator cannot be expected to know about a platform
// (its issuer, where it publishes its keys, the shape of its subjects) and
// declares what they supply themselves as variables. Connecting from an entry
// writes ordinary issuer and admission rows, so nothing here is read when a
// workload signs in; this package must never be imported by workloadidentity.
//
// Entries are YAML files under platforms/, embedded in the binary and validated
// when loaded. Adding a platform is adding a file.
package catalog

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

//go:embed platforms/*.yaml
var platformFiles embed.FS

// Visibility is whether the operator sees a value an entry supplies.
type Visibility string

const (
	// VisibilityHidden values are set without being shown, because they are the
	// same for every customer.
	VisibilityHidden Visibility = "hidden"

	// VisibilityReadOnly values are shown, for operators who need to recognize
	// them in their own configuration, but cannot be changed.
	VisibilityReadOnly Visibility = "read-only"
)

// Tier is how often a variable is supplied.
type Tier string

const (
	// TierPlatform variables are supplied once per trusted platform, such as a
	// tenant ID that is part of the issuer URL.
	TierPlatform Tier = "platform"

	// TierRule variables are supplied once per access rule, such as the
	// organization ID in a subject.
	TierRule Tier = "rule"
)

// Phase is where a step sits relative to writing the rows.
type Phase string

const (
	// PhaseCollect steps gather what the trusted platform and its first access
	// rule need.
	PhaseCollect Phase = "collect"

	// PhaseCreate is the one step that writes the rows.
	PhaseCreate Phase = "create"

	// PhaseConnect steps describe the platform's side, and only make sense once
	// the rows exist.
	PhaseConnect Phase = "connect"
)

// BlockType is the kind of content a block renders.
type BlockType string

const (
	// BlockText is Markdown. Renderers must not interpret raw HTML in it.
	BlockText BlockType = "text"

	// BlockImage is a screenshot served from the dashboard's own origin.
	BlockImage BlockType = "image"

	// BlockLink points at the platform's console or documentation.
	BlockLink BlockType = "link"

	// BlockField collects one variable.
	BlockField BlockType = "field"

	// BlockSubjectRule previews the access rule the variables produce.
	BlockSubjectRule BlockType = "subject_rule"

	// BlockAgentPicker chooses the agent the access rule is assigned.
	BlockAgentPicker BlockType = "agent_picker"

	// BlockTags collects optional labels for the access rule.
	BlockTags BlockType = "tags"

	// BlockComputedStatus says why computed values cannot be shown, if they
	// cannot.
	BlockComputedStatus BlockType = "computed_status"

	// BlockComputed shows one value Gram derives, with a copy button.
	BlockComputed BlockType = "computed"
)

// Computed values a definition may place. Each is derived by the server, the
// same way its authorization server metadata derives it; a definition can never
// compute one itself.
const (
	ComputedTokenEndpoint = "token_endpoint"
	ComputedIssuerURL     = "issuer_url"
	ComputedMCPHost       = "mcp_host"
)

var computedValues = []string{ComputedTokenEndpoint, ComputedIssuerURL, ComputedMCPHost}

// Constant is a value the entry supplies.
type Constant struct {
	// Value is used as is.
	Value string `yaml:"value"`

	// Visibility is whether the operator sees Value.
	Visibility Visibility `yaml:"visibility"`
}

// Variable is something the operator supplies.
type Variable struct {
	// Key names the variable in templates, as {key}.
	Key string `yaml:"key"`

	// Tier is whether it is supplied per platform or per access rule.
	Tier Tier `yaml:"tier"`

	// Label is the field's label.
	Label string `yaml:"label"`

	// Help says where the operator finds the value.
	Help string `yaml:"help"`

	// Placeholder is shown in the empty field.
	Placeholder string `yaml:"placeholder"`

	// Pattern is a regular expression the whole value must match.
	Pattern string `yaml:"pattern"`

	// PatternMessage is shown when the value does not match Pattern.
	PatternMessage string `yaml:"pattern_message"`
}

// Subject is the access rule an entry produces.
type Subject struct {
	// Template is the subject, with {key} placeholders for rule-tier
	// variables. For a wildcard rule it is the stem, and must end on a
	// delimiter of the platform's subject format so it cannot run into a
	// neighboring value.
	Template string `yaml:"template"`

	// Wildcard makes the rule the filled template followed by "*".
	Wildcard bool `yaml:"wildcard"`
}

// Block is one piece of a step. Which fields apply depends on Type.
type Block struct {
	// Type is the kind of content.
	Type BlockType `yaml:"type"`

	// Markdown is a text block's content.
	Markdown string `yaml:"markdown"`

	// Src is an image's path on the dashboard's origin.
	Src string `yaml:"src"`

	// Alt is an image's alternative text.
	Alt string `yaml:"alt"`

	// Caption is shown under an image.
	Caption string `yaml:"caption"`

	// Href is a link's target.
	Href string `yaml:"href"`

	// Label is a link's or computed value's label.
	Label string `yaml:"label"`

	// Variable is the key a field collects.
	Variable string `yaml:"variable"`

	// Value is the computed value a computed block shows.
	Value string `yaml:"value"`

	// Help is shown under a computed value.
	Help string `yaml:"help"`
}

// Step is one screen of a guided setup.
type Step struct {
	// ID is stable within the entry, and appears in the dashboard URL.
	ID string `yaml:"id"`

	// Title heads the step.
	Title string `yaml:"title"`

	// Phase places the step relative to writing the rows.
	Phase Phase `yaml:"phase"`

	// Blocks are rendered in order.
	Blocks []Block `yaml:"blocks"`
}

// Setup is an entry's guided setup.
type Setup struct {
	// Steps run in order: collect steps, the create step, then connect steps.
	Steps []Step `yaml:"steps"`
}

// Platform is one catalog entry.
type Platform struct {
	// Key identifies the entry permanently: it is written onto the rows the
	// entry creates, so renaming it breaks their connected state.
	Key string `yaml:"key"`

	// DisplayName is what the operator sees.
	DisplayName string `yaml:"display_name"`

	// Description says what connecting the platform does.
	Description string `yaml:"description"`

	// Icon is the platform's logo, a path on the dashboard's origin. Empty
	// where the platform has none.
	Icon string `yaml:"icon"`

	// Enabled is false for an entry listed but not offered.
	Enabled bool `yaml:"enabled"`

	// Issuer is the issuer identifier its tokens carry.
	Issuer Constant `yaml:"issuer"`

	// JWKSURI is where it publishes its signing keys.
	JWKSURI Constant `yaml:"jwks_uri"`

	// Variables are what the operator supplies.
	Variables []Variable `yaml:"variables"`

	// Subject is the access rule the entry produces.
	Subject Subject `yaml:"subject"`

	// Setup is the guided setup; nil falls back to the registration form.
	Setup *Setup `yaml:"setup"`
}

// Source provides the catalog. Files are the only source today; a staff-edited
// store can replace them without changing callers.
type Source interface {
	Platforms(ctx context.Context) ([]Platform, error)
}

// Embedded returns the platforms shipped with the server.
func Embedded() Source {
	return embeddedSource{}
}

type embeddedSource struct{}

var loadEmbedded = sync.OnceValues(func() ([]Platform, error) {
	return Load(platformFiles, "platforms")
})

func (embeddedSource) Platforms(_ context.Context) ([]Platform, error) {
	return loadEmbedded()
}

// Load reads and validates every YAML file in dir, ordered by key.
func Load(fsys fs.FS, dir string) ([]Platform, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read platform directory: %w", err)
	}

	platforms := make([]Platform, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || path.Ext(entry.Name()) != ".yaml" {
			continue
		}
		raw, err := fs.ReadFile(fsys, path.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.Name(), err)
		}
		platform, err := Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		if strings.TrimSuffix(entry.Name(), ".yaml") != platform.Key {
			return nil, fmt.Errorf("%s: file name must be the key %q", entry.Name(), platform.Key)
		}
		platforms = append(platforms, platform)
	}

	slices.SortFunc(platforms, func(a, b Platform) int { return strings.Compare(a.Key, b.Key) })
	return platforms, nil
}

// Parse decodes and validates one platform file. Unknown fields are refused, so
// a misspelled key fails instead of silently dropping its value.
func Parse(raw []byte) (Platform, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)

	var platform Platform
	if err := decoder.Decode(&platform); err != nil {
		return Platform{}, fmt.Errorf("decode: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Platform{}, errors.New("a platform file holds exactly one document")
	}
	if err := validate(platform); err != nil {
		return Platform{}, err
	}
	return platform, nil
}

var (
	keyPattern         = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	variableKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	placeholderPattern = regexp.MustCompile(`\{([^{}]*)\}`)
)

// stemDelimiters are the characters a wildcard stem may end on. Ending
// anywhere else lets the stem match a longer neighboring value: "repo:acme/app"
// would also admit "repo:acme/app-evil".
const stemDelimiters = "/:"

func validate(p Platform) error {
	if !keyPattern.MatchString(p.Key) {
		return fmt.Errorf("key %q must be lowercase words joined by hyphens", p.Key)
	}
	if strings.TrimSpace(p.DisplayName) == "" {
		return errors.New("display_name is required")
	}
	if p.Icon != "" {
		if err := requireOriginPath(p.Icon, "icon"); err != nil {
			return err
		}
	}
	if err := validateConstant("issuer", p.Issuer); err != nil {
		return err
	}
	if err := validateConstant("jwks_uri", p.JWKSURI); err != nil {
		return err
	}

	variables := make(map[string]Variable, len(p.Variables))
	for _, variable := range p.Variables {
		if err := validateVariable(variable); err != nil {
			return err
		}
		if _, ok := variables[variable.Key]; ok {
			return fmt.Errorf("variable %q is declared twice", variable.Key)
		}
		variables[variable.Key] = variable
	}

	if err := validateTemplate("issuer", p.Issuer.Value, variables, TierPlatform); err != nil {
		return err
	}
	if err := validateTemplate("jwks_uri", p.JWKSURI.Value, variables, TierPlatform); err != nil {
		return err
	}
	if err := validateSubject(p.Subject, variables); err != nil {
		return err
	}

	if p.Setup != nil {
		if err := validateSetup(*p.Setup, variables); err != nil {
			return err
		}
	}
	return nil
}

func validateConstant(field string, c Constant) error {
	if c.Visibility != VisibilityHidden && c.Visibility != VisibilityReadOnly {
		return fmt.Errorf("%s.visibility must be %q or %q", field, VisibilityHidden, VisibilityReadOnly)
	}
	if err := requireHTTPS(placeholderPattern.ReplaceAllString(c.Value, "x"), field); err != nil {
		return err
	}
	return nil
}

func validateVariable(v Variable) error {
	if !variableKeyPattern.MatchString(v.Key) {
		return fmt.Errorf("variable key %q must be lowercase snake_case", v.Key)
	}
	if v.Tier != TierPlatform && v.Tier != TierRule {
		return fmt.Errorf("variable %q: tier must be %q or %q", v.Key, TierPlatform, TierRule)
	}
	if strings.TrimSpace(v.Label) == "" {
		return fmt.Errorf("variable %q: label is required", v.Key)
	}
	if v.Pattern == "" {
		return fmt.Errorf("variable %q: pattern is required, so a value cannot reshape the template it fills", v.Key)
	}
	if _, err := regexp.Compile("^(?:" + v.Pattern + ")$"); err != nil {
		return fmt.Errorf("variable %q: pattern: %w", v.Key, err)
	}
	return nil
}

// validateTemplate checks that every placeholder names a declared variable of
// the expected tier: an issuer can only vary per platform, and a subject only
// per rule.
func validateTemplate(field, template string, variables map[string]Variable, tier Tier) error {
	for _, match := range placeholderPattern.FindAllStringSubmatch(template, -1) {
		variable, ok := variables[match[1]]
		if !ok {
			return fmt.Errorf("%s uses undeclared variable {%s}", field, match[1])
		}
		if variable.Tier != tier {
			return fmt.Errorf("%s uses {%s}, which must be a %s-tier variable", field, match[1], tier)
		}
	}
	return nil
}

func validateSubject(s Subject, variables map[string]Variable) error {
	if strings.TrimSpace(s.Template) == "" {
		return errors.New("subject.template is required")
	}
	if strings.Contains(s.Template, "*") {
		return errors.New(`subject.template must not contain "*"; set wildcard instead`)
	}
	if err := validateTemplate("subject.template", s.Template, variables, TierRule); err != nil {
		return err
	}
	if s.Wildcard {
		if !strings.ContainsAny(s.Template[len(s.Template)-1:], stemDelimiters) {
			return fmt.Errorf("subject.template must end on one of %q to be a wildcard stem", stemDelimiters)
		}
		if strings.HasSuffix(s.Template, "}") {
			return errors.New("a wildcard subject.template must not end on a variable")
		}
	}
	return nil
}

func validateSetup(setup Setup, variables map[string]Variable) error {
	if len(setup.Steps) == 0 {
		return errors.New("setup has no steps")
	}

	order := map[Phase]int{PhaseCollect: 0, PhaseCreate: 1, PhaseConnect: 2}
	ids := make(map[string]bool, len(setup.Steps))
	creates := 0
	last := 0
	for _, step := range setup.Steps {
		rank, ok := order[step.Phase]
		if !ok {
			return fmt.Errorf("step %q: unknown phase %q", step.ID, step.Phase)
		}
		if rank < last {
			return fmt.Errorf("step %q: steps must run collect, then create, then connect", step.ID)
		}
		last = rank
		if step.Phase == PhaseCreate {
			creates++
		}
		if !keyPattern.MatchString(step.ID) {
			return fmt.Errorf("step id %q must be lowercase words joined by hyphens", step.ID)
		}
		if ids[step.ID] {
			return fmt.Errorf("step id %q is used twice", step.ID)
		}
		ids[step.ID] = true
		if strings.TrimSpace(step.Title) == "" {
			return fmt.Errorf("step %q: title is required", step.ID)
		}
		for i, block := range step.Blocks {
			if err := validateBlock(block, variables); err != nil {
				return fmt.Errorf("step %q block %d: %w", step.ID, i+1, err)
			}
		}
	}
	if creates != 1 {
		return fmt.Errorf("setup must have exactly one create step, found %d", creates)
	}
	return nil
}

func validateBlock(b Block, variables map[string]Variable) error {
	switch b.Type {
	case BlockText:
		if strings.TrimSpace(b.Markdown) == "" {
			return errors.New("text needs markdown")
		}
	case BlockImage:
		if err := requireOriginPath(b.Src, "image src"); err != nil {
			return err
		}
		if strings.TrimSpace(b.Alt) == "" {
			return errors.New("image needs alt text")
		}
	case BlockLink:
		if err := requireHTTPS(b.Href, "link href"); err != nil {
			return err
		}
		if strings.TrimSpace(b.Label) == "" {
			return errors.New("link needs a label")
		}
	case BlockField:
		if _, ok := variables[b.Variable]; !ok {
			return fmt.Errorf("field names undeclared variable %q", b.Variable)
		}
	case BlockComputed:
		if !slices.Contains(computedValues, b.Value) {
			return fmt.Errorf("computed value %q is not one Gram derives", b.Value)
		}
		if strings.TrimSpace(b.Label) == "" {
			return errors.New("computed value needs a label")
		}
	case BlockSubjectRule, BlockAgentPicker, BlockTags, BlockComputedStatus:
	default:
		return fmt.Errorf("unknown block type %q", b.Type)
	}
	return nil
}

// requireOriginPath refuses anything but a path on the dashboard's own origin.
// Images ship with the dashboard, and a definition must not be able to point
// the operator's browser at another host.
func requireOriginPath(raw, field string) error {
	if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.Contains(raw, "\\") {
		return fmt.Errorf("%s %q must be a path on the dashboard's origin", field, raw)
	}
	return nil
}

func requireHTTPS(raw, field string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("%s %q must be an https URL", field, raw)
	}
	return nil
}
