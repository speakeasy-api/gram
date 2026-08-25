// Package policycatalog owns the release-pinned detector values that Platform
// policy administration may expose. Scanner packages remain authoritative for
// what can produce findings; this package projects their stable public values
// into one deterministic catalog without exposing scanner implementation types.
package policycatalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/scanners/accountidentity"
	"github.com/speakeasy-api/gram/server/internal/scanners/clidestructive"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

const SchemaV1 = "risk-policy-catalog-v1"

type Catalog struct {
	Schema                   string   `json:"schema"`
	PolicyTypes              []string `json:"policy_types"`
	Actions                  []string `json:"actions"`
	Sources                  []string `json:"sources"`
	FlagOnlySources          []string `json:"flag_only_sources"`
	PresidioEntities         []string `json:"presidio_entities"`
	PromptInjectionRules     []string `json:"prompt_injection_rules"`
	DisabledRules            []string `json:"disabled_rules"`
	DetectionScopeCategories []string `json:"detection_scope_categories"`
	PolicyMessageTypes       []string `json:"policy_message_types"`
}

func Build() (Catalog, error) {
	secretRules, err := gitleaks.ReportableRuleIDs()
	if err != nil {
		return Catalog{}, fmt.Errorf("list reportable gitleaks rules: %w", err)
	}

	disabledRules := make([]string, 0, len(secretRules)+len(presidioEntities)+2)
	disabledRules = append(disabledRules, secretRules...)
	for _, entity := range presidioEntities {
		disabledRules = append(disabledRules, CanonicalPresidioRuleID(entity))
	}
	disabledRules = append(disabledRules,
		accountidentity.RulePersonalAccount,
		accountidentity.RuleUnapprovedDomain,
	)

	catalog := Catalog{
		Schema:      SchemaV1,
		PolicyTypes: []string{"prompt_based", "standard"},
		Actions:     []string{"block", "flag", "warn"},
		Sources: []string{
			accountidentity.Source,
			clidestructive.Source,
			shadowmcp.SourceDestructiveTool,
			gitleaks.Source,
			PresidioSource,
			promptinjection.Source,
			shadowmcp.SourceShadowMCP,
		},
		FlagOnlySources: []string{
			accountidentity.Source,
			clidestructive.Source,
			shadowmcp.SourceDestructiveTool,
		},
		PresidioEntities:     slices.Clone(presidioEntities),
		PromptInjectionRules: []string{},
		DisabledRules:        disabledRules,
		// Account identity is deliberately absent: it is a session-scoped
		// detector, so per-message detection scopes do not apply.
		DetectionScopeCategories: []string{
			string(categories.CategoryCLIDestructive),
			string(categories.CategoryDestructiveTool),
			string(categories.CategoryFinancial),
			string(categories.CategoryGovernmentIDs),
			string(categories.CategoryHealthcare),
			string(categories.CategoryOffPolicy),
			string(categories.CategoryPII),
			string(categories.CategoryPromptInjection),
			string(categories.CategorySecrets),
			string(categories.CategoryShadowMCP),
		},
		// PromptAttachment is a valid backend message kind but is deliberately
		// excluded from D3 authoring v1. Existing policies remain readable; the
		// Platform schema cannot newly select this client-side context surface.
		PolicyMessageTypes: []string{
			message.Assistant,
			message.ToolRequest,
			message.ToolResponse,
			message.User,
		},
	}

	sortUnique := func(name string, values []string) ([]string, error) {
		slices.Sort(values)
		for i, value := range values {
			if value == "" {
				return nil, fmt.Errorf("%s contains an empty value", name)
			}
			if i > 0 && values[i-1] == value {
				return nil, fmt.Errorf("%s contains duplicate %q", name, value)
			}
		}
		return values, nil
	}

	for name, values := range map[string]*[]string{
		"policy_types":               &catalog.PolicyTypes,
		"actions":                    &catalog.Actions,
		"sources":                    &catalog.Sources,
		"flag_only_sources":          &catalog.FlagOnlySources,
		"presidio_entities":          &catalog.PresidioEntities,
		"disabled_rules":             &catalog.DisabledRules,
		"detection_scope_categories": &catalog.DetectionScopeCategories,
		"policy_message_types":       &catalog.PolicyMessageTypes,
		"prompt_injection_rules":     &catalog.PromptInjectionRules,
	} {
		sorted, err := sortUnique(name, *values)
		if err != nil {
			return Catalog{}, err
		}
		*values = sorted
	}

	return catalog, nil
}

func CanonicalJSON(catalog Catalog) ([]byte, error) {
	data, err := json.Marshal(catalog)
	if err != nil {
		return nil, fmt.Errorf("encode risk policy catalog: %w", err)
	}
	return data, nil
}

func Fingerprint(catalog Catalog) (string, error) {
	data, err := CanonicalJSON(catalog)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
