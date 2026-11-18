package inferenceprofiles

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/directory"
	"github.com/speakeasy-api/gram/server/internal/inferenceprofiles/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ProviderBedrock = "bedrock"
	ProviderVertex  = "vertex"
	ProviderGateway = "gateway"

	HarnessClaudeCode = "claude_code"
	HarnessCodex      = "codex"
)

// Harnesses are the harnesses the device agent has a tool_config writer for,
// in the order resolutions are reported.
var Harnesses = []string{HarnessClaudeCode, HarnessCodex}

// Models are the model IDs a profile pins, in the provider's own naming.
type Models struct {
	Default string `json:"default,omitempty"`
	Opus    string `json:"opus,omitempty"`
	Sonnet  string `json:"sonnet,omitempty"`
	Haiku   string `json:"haiku,omitempty"`
}

// Settings is the stored shape of inference_profiles.settings.
type Settings struct {
	Region       string `json:"region,omitempty"`
	AWSProfile   string `json:"aws_profile,omitempty"`
	GCPProjectID string `json:"gcp_project_id,omitempty"`
	BaseURL      string `json:"base_url,omitempty"`
	Models       Models `json:"models,omitzero"`
}

// Profile is a decoded inference profile.
type Profile struct {
	ID       uuid.UUID
	Name     string
	Provider string
	Settings Settings
}

// Rule is a decoded inference profile rule. An empty Harness applies to every
// harness that supports the profile's provider.
type Rule struct {
	ID           uuid.UUID
	ProfileID    uuid.UUID
	PrincipalURN string
	Harness      string
	Position     int
	CreatedAt    time.Time
}

// Candidate is a rule matching one of the caller's principals, with its
// profile.
type Candidate struct {
	Rule    Rule
	Profile Profile
}

// Resolution is what one harness resolves to. Matched is false when no rule
// applies, leaving the harness on its own default.
type Resolution struct {
	Harness string
	Matched bool
	Rule    Rule
	Profile Profile
}

// SupportsHarness reports whether the device agent can point harness at
// provider. Codex has no Vertex AI provider.
func SupportsHarness(provider, harness string) bool {
	switch harness {
	case HarnessClaudeCode:
		return provider == ProviderBedrock || provider == ProviderVertex || provider == ProviderGateway
	case HarnessCodex:
		return provider == ProviderBedrock || provider == ProviderGateway
	default:
		return false
	}
}

// Principal specificity levels; lower is more specific.
const (
	levelUser = iota
	levelEmail
	levelGroup
	levelEveryone
)

func principalLevel(principal string) int {
	switch {
	case principal == urn.PrincipalWildcard || principal == string(urn.PrincipalTypeUser)+":"+urn.AllUsersPrincipalID:
		return levelEveryone
	case strings.HasPrefix(principal, string(urn.PrincipalTypeUser)+":"):
		return levelUser
	case strings.HasPrefix(principal, string(urn.PrincipalTypeEmail)+":"):
		return levelEmail
	case strings.HasPrefix(principal, string(urn.PrincipalTypeRole)+":"), directory.IsGroupPrincipal(principal), directory.IsAttributePrincipal(principal):
		return levelGroup
	default:
		return levelEveryone
	}
}

// Resolve picks, for each harness, the most specific matching rule:
// user: > email: > role:/directory group or attribute > user:all; a
// harness-specific rule beats an all-harness rule at the same level; then the
// lower position, then the older rule. A rule whose profile's provider the
// harness cannot use never applies to it.
func Resolve(candidates []Candidate) []Resolution {
	resolutions := make([]Resolution, 0, len(Harnesses))
	for _, harness := range Harnesses {
		var applicable []Candidate
		for _, candidate := range candidates {
			if candidate.Rule.Harness != "" && candidate.Rule.Harness != harness {
				continue
			}
			if !SupportsHarness(candidate.Profile.Provider, harness) {
				continue
			}
			applicable = append(applicable, candidate)
		}
		if len(applicable) == 0 {
			var unmatched Resolution
			unmatched.Harness = harness
			resolutions = append(resolutions, unmatched)
			continue
		}
		best := slices.MinFunc(applicable, compareCandidates)
		resolutions = append(resolutions, Resolution{Harness: harness, Matched: true, Rule: best.Rule, Profile: best.Profile})
	}
	return resolutions
}

func compareCandidates(a, b Candidate) int {
	allHarness := func(c Candidate) int {
		if c.Rule.Harness == "" {
			return 1
		}
		return 0
	}
	return cmp.Or(
		cmp.Compare(principalLevel(a.Rule.PrincipalURN), principalLevel(b.Rule.PrincipalURN)),
		cmp.Compare(allHarness(a), allHarness(b)),
		cmp.Compare(a.Rule.Position, b.Rule.Position),
		a.Rule.CreatedAt.Compare(b.Rule.CreatedAt),
		strings.Compare(a.Rule.ID.String(), b.Rule.ID.String()),
	)
}

// ToolConfig renders every matched resolution into the device agent's
// tool_config, keyed by harness. It is empty when nothing matched.
func ToolConfig(resolutions []Resolution) map[string]any {
	config := make(map[string]any)
	for _, resolution := range resolutions {
		if !resolution.Matched {
			continue
		}
		if rendered := Render(resolution.Harness, resolution.Profile); rendered != nil {
			config[resolution.Harness] = rendered
		}
	}
	return config
}

// Render maps a profile onto harness-native settings. It returns nil for a
// pairing the harness cannot use. Nothing rendered is ever a credential.
func Render(harness string, profile Profile) map[string]any {
	settings := profile.Settings
	switch harness {
	case HarnessClaudeCode:
		env := map[string]any{}
		out := map[string]any{"env": env}
		switch profile.Provider {
		case ProviderBedrock:
			env["CLAUDE_CODE_USE_BEDROCK"] = "1"
			env["AWS_REGION"] = settings.Region
			setIf(env, "AWS_PROFILE", settings.AWSProfile)
			setIf(env, "ANTHROPIC_BEDROCK_BASE_URL", settings.BaseURL)
			if settings.AWSProfile != "" {
				// AWSProfile is restricted to a shell-safe pattern on write.
				out["awsAuthRefresh"] = "aws sso login --profile " + settings.AWSProfile
			}
		case ProviderVertex:
			env["CLAUDE_CODE_USE_VERTEX"] = "1"
			env["CLOUD_ML_REGION"] = settings.Region
			env["ANTHROPIC_VERTEX_PROJECT_ID"] = settings.GCPProjectID
			setIf(env, "ANTHROPIC_VERTEX_BASE_URL", settings.BaseURL)
			out["gcpAuthRefresh"] = "gcloud auth application-default login"
		case ProviderGateway:
			env["ANTHROPIC_BASE_URL"] = settings.BaseURL
		default:
			return nil
		}
		setIf(env, "ANTHROPIC_MODEL", settings.Models.Default)
		setIf(env, "ANTHROPIC_DEFAULT_OPUS_MODEL", settings.Models.Opus)
		setIf(env, "ANTHROPIC_DEFAULT_SONNET_MODEL", settings.Models.Sonnet)
		setIf(env, "ANTHROPIC_DEFAULT_HAIKU_MODEL", settings.Models.Haiku)
		return out
	case HarnessCodex:
		var out map[string]any
		switch profile.Provider {
		case ProviderBedrock:
			aws := map[string]any{"region": settings.Region}
			setIf(aws, "profile", settings.AWSProfile)
			out = map[string]any{
				"model_provider":  "amazon-bedrock",
				"model_providers": map[string]any{"amazon-bedrock": map[string]any{"aws": aws}},
			}
		case ProviderGateway:
			out = map[string]any{
				"model_provider": "speakeasy",
				"model_providers": map[string]any{"speakeasy": map[string]any{
					"name":     "Speakeasy gateway",
					"base_url": settings.BaseURL,
					"wire_api": "responses",
				}},
			}
		default:
			return nil
		}
		setIf(out, "model", settings.Models.Default)
		return out
	default:
		return nil
	}
}

func setIf(m map[string]any, key, value string) {
	if value != "" {
		m[key] = value
	}
}

// DecodeProfile converts a stored profile row.
func DecodeProfile(row repo.InferenceProfile) (Profile, error) {
	var settings Settings
	if len(row.Settings) > 0 {
		if err := json.Unmarshal(row.Settings, &settings); err != nil {
			return Profile{}, fmt.Errorf("decode inference profile settings: %w", err)
		}
	}
	return Profile{ID: row.ID, Name: row.Name, Provider: row.Provider, Settings: settings}, nil
}

// DecodeRule converts a stored rule row.
func DecodeRule(row repo.InferenceProfileRule) Rule {
	return Rule{
		ID:           row.ID,
		ProfileID:    row.ProfileID,
		PrincipalURN: row.PrincipalUrn,
		Harness:      row.Harness.String,
		Position:     int(row.Position),
		CreatedAt:    row.CreatedAt.Time,
	}
}

// ResolveForPrincipals loads the organization's rules matching principals
// (as plugins.ResolveDeliveryPrincipals returns them) and resolves each
// harness.
func ResolveForPrincipals(ctx context.Context, db database.DBTX, organizationID string, principals []string) ([]Resolution, error) {
	rows, err := repo.New(db).ListMatchingInferenceProfileRules(ctx, repo.ListMatchingInferenceProfileRulesParams{
		OrganizationID: organizationID,
		PrincipalUrns:  principals,
	})
	if err != nil {
		return nil, fmt.Errorf("list matching inference profile rules: %w", err)
	}
	candidates := make([]Candidate, 0, len(rows))
	for _, row := range rows {
		profile, err := DecodeProfile(row.InferenceProfile)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, Candidate{Rule: DecodeRule(row.InferenceProfileRule), Profile: profile})
	}
	return Resolve(candidates), nil
}
