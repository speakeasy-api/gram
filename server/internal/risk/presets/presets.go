// Package presets owns the use-case bundles an administrator starts a risk
// policy from. A preset resolves to the same create payload the dashboard
// wizard and the Platform MCP already accept, so both surfaces can offer
// "protect customer data" instead of "pick detectors, then a scope, then an
// action".
package presets

import (
	"slices"
	"sort"
	"strings"
	"unicode"
)

const (
	PolicyTypeStandard    = "standard"
	PolicyTypePromptBased = "prompt_based"
)

// Preset is one use-case bundle. Sources and PresidioEntities are catalog
// values; the recommended detection scopes for each category apply at scan
// time, so a preset never carries scope CEL.
type Preset struct {
	ID          string
	Label       string
	Description string
	PolicyType  string

	Sources          []string
	PresidioEntities []string
	Action           string
	Score            float64
	Prompt           string
	UserMessage      string

	// RequiresApprovedEmailDomains marks presets that are inert until the
	// administrator supplies the organization's approved email domains.
	RequiresApprovedEmailDomains bool

	// Keywords drive the deterministic suggestion path. Multi-word entries
	// weigh more than single words because they are less ambiguous.
	Keywords []string
}

var registry = []Preset{
	{
		ID:                           "secrets_and_credentials",
		Label:                        "Secrets and credentials",
		Description:                  "Block API keys, tokens, private keys and passwords from entering prompts, tool arguments or tool results.",
		PolicyType:                   PolicyTypeStandard,
		Sources:                      []string{"gitleaks"},
		PresidioEntities:             nil,
		Action:                       "block",
		Score:                        8,
		Prompt:                       "",
		UserMessage:                  "%{match} looks like a credential. Remove it from the request before continuing.",
		RequiresApprovedEmailDomains: false,
		Keywords:                     []string{"secret", "credential", "api key", "access key", "token", "password", "private key", "aws key", "leak"},
	},
	{
		ID:          "customer_pii_egress",
		Label:       "Customer personal data",
		Description: "Flag personal, financial and government identifiers such as emails, phone numbers, card numbers, bank accounts, national IDs and passports wherever agents handle them.",
		PolicyType:  PolicyTypeStandard,
		Sources:     []string{"presidio"},
		PresidioEntities: []string{
			"AU_TFN", "CREDIT_CARD", "EMAIL_ADDRESS", "ES_NIF", "IBAN_CODE", "IN_AADHAAR", "IN_PAN",
			"IP_ADDRESS", "IT_FISCAL_CODE", "PHONE_NUMBER", "SG_NRIC_FIN", "UK_NHS", "UK_NINO",
			"UK_PASSPORT", "US_BANK_NUMBER", "US_ITIN", "US_PASSPORT", "US_SSN",
		},
		Action:                       "flag",
		Score:                        6,
		Prompt:                       "",
		UserMessage:                  "",
		RequiresApprovedEmailDomains: false,
		Keywords:                     []string{"pii", "personal data", "personal information", "customer data", "personally identifiable", "email address", "phone number", "ssn", "social security", "credit card", "card number", "bank account", "passport", "national id", "gdpr", "privacy"},
	},
	{
		ID:               "destructive_production_actions",
		Label:            "Destructive actions in production",
		Description:      "Block tool calls that delete, drop, truncate, force-push or otherwise irreversibly change production systems. Judged by the policy model, so it understands intent rather than matching command names.",
		PolicyType:       PolicyTypePromptBased,
		Sources:          nil,
		PresidioEntities: nil,
		Action:           "block",
		Score:            9,
		Prompt: "Flag any tool call that performs a destructive or irreversible operation against a production system. " +
			"This includes deleting or dropping databases, tables, records or files; truncating data; force-pushing or deleting branches; " +
			"terminating, deleting or resizing cloud resources; and disabling monitoring, backups or access controls. " +
			"Treat local, test and staging targets as out of scope unless the request clearly says production.",
		UserMessage:                  "This looks like a destructive change to a production system. Confirm the target is not production, or ask an administrator to run it.",
		RequiresApprovedEmailDomains: false,
		Keywords:                     []string{"destructive", "delete", "deleting", "drop table", "drop database", "truncate", "production", "prod ", "rm -rf", "force push", "force-push", "irreversible", "wipe", "terminate"},
	},
	{
		ID:                           "unapproved_mcp_servers",
		Label:                        "Unapproved MCP servers",
		Description:                  "Flag tool calls that reach MCP servers your organization has not issued or approved, so shadow integrations show up in Watchdog. Blocking with a default posture is configured from the dashboard.",
		PolicyType:                   PolicyTypeStandard,
		Sources:                      []string{"shadow_mcp"},
		PresidioEntities:             nil,
		Action:                       "flag",
		Score:                        8,
		Prompt:                       "",
		UserMessage:                  "",
		RequiresApprovedEmailDomains: false,
		Keywords:                     []string{"shadow mcp", "shadow", "unapproved", "unknown mcp", "unsanctioned", "third-party mcp", "third party mcp", "allowlist", "allow list", "mcp server", "mcp servers"},
	},
	{
		ID:                           "non_corporate_accounts",
		Label:                        "Non-corporate accounts",
		Description:                  "Flag agent sessions signed in with a personal AI account or an email domain outside the approved list. Needs the organization's approved email domains.",
		PolicyType:                   PolicyTypeStandard,
		Sources:                      []string{"account_identity"},
		PresidioEntities:             nil,
		Action:                       "flag",
		Score:                        5,
		Prompt:                       "",
		UserMessage:                  "",
		RequiresApprovedEmailDomains: true,
		Keywords:                     []string{"personal account", "non-corporate", "non corporate", "corporate account", "email domain", "gmail", "personal email", "byo", "company account", "work account"},
	},
	{
		ID:                           "prompt_injection",
		Label:                        "Prompt injection",
		Description:                  "Flag jailbreaks, instruction overrides and hidden instructions arriving through user prompts or tool outputs, judged semantically.",
		PolicyType:                   PolicyTypeStandard,
		Sources:                      []string{"prompt_injection"},
		PresidioEntities:             nil,
		Action:                       "flag",
		Score:                        7,
		Prompt:                       "",
		UserMessage:                  "",
		RequiresApprovedEmailDomains: false,
		Keywords:                     []string{"prompt injection", "injection", "jailbreak", "hidden instruction", "instruction override", "ignore previous", "untrusted content", "untrusted input"},
	},
}

// All returns the presets in display order.
func All() []Preset {
	out := make([]Preset, len(registry))
	for i, preset := range registry {
		out[i] = preset.clone()
	}
	return out
}

// IDs returns the preset identifiers in display order.
func IDs() []string {
	out := make([]string, len(registry))
	for i, preset := range registry {
		out[i] = preset.ID
	}
	return out
}

// ByID looks a preset up by identifier.
func ByID(id string) (Preset, bool) {
	for _, preset := range registry {
		if preset.ID == id {
			return preset.clone(), true
		}
	}
	return Preset{}, false
}

func (p Preset) clone() Preset {
	p.Sources = slices.Clone(p.Sources)
	p.PresidioEntities = slices.Clone(p.PresidioEntities)
	p.Keywords = slices.Clone(p.Keywords)
	return p
}

// Draft is the create payload a preset expands to, before project, name
// overrides and idempotency are applied by the caller.
type Draft struct {
	PresetID         string
	PolicyType       string
	Name             string
	Action           string
	Score            float64
	Sources          []string
	PresidioEntities []string
	Prompt           string
	UserMessage      string
}

// Draft expands the preset into a create payload.
func (p Preset) Draft() Draft {
	return Draft{
		PresetID:         p.ID,
		PolicyType:       p.PolicyType,
		Name:             p.Label,
		Action:           p.Action,
		Score:            p.Score,
		Sources:          slices.Clone(p.Sources),
		PresidioEntities: slices.Clone(p.PresidioEntities),
		Prompt:           p.Prompt,
		UserMessage:      p.UserMessage,
	}
}

// Match is one preset's fit for a description.
type Match struct {
	Preset     Preset
	Confidence float64
}

// Suggestion is the deterministic mapping from a description to a draft.
// Preset is nil when nothing matched and the draft is a bespoke prompt-based
// guardrail carrying the description as its instruction.
type Suggestion struct {
	Preset       *Preset
	Draft        Draft
	Confidence   float64
	Rationale    string
	Alternatives []Match
}

const (
	maxBespokeNameRunes = 60
	defaultBespokeScore = 5
)

// Suggest maps a plain-language description to the closest preset by keyword
// overlap. It is the fallback the dashboard uses when the model is
// unavailable and the whole path for the Platform MCP, whose caller is itself
// a model that only needs the catalog explained.
func Suggest(description string) Suggestion {
	normalized := " " + strings.ToLower(strings.Join(strings.Fields(description), " ")) + " "
	matches := make([]Match, 0, len(registry))
	for _, preset := range registry {
		score := 0.0
		hits := make([]string, 0, 3)
		for _, keyword := range preset.Keywords {
			if !strings.Contains(normalized, strings.ToLower(keyword)) {
				continue
			}
			weight := 1.0
			if strings.ContainsAny(keyword, " -") {
				weight = 2
			}
			score += weight
			hits = append(hits, strings.TrimSpace(keyword))
		}
		if score == 0 {
			continue
		}
		matches = append(matches, Match{Preset: preset.clone(), Confidence: score / (score + 2)})
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Confidence != matches[j].Confidence {
			return matches[i].Confidence > matches[j].Confidence
		}
		return matches[i].Preset.ID < matches[j].Preset.ID
	})

	if len(matches) == 0 {
		return Suggestion{
			Preset:       nil,
			Draft:        bespokeDraft(description),
			Confidence:   0,
			Rationale:    "No preset matched the description, so it becomes the instruction for a prompt-based guardrail judged by the policy model.",
			Alternatives: nil,
		}
	}

	best := matches[0]
	return Suggestion{
		Preset:       &best.Preset,
		Draft:        best.Preset.Draft(),
		Confidence:   best.Confidence,
		Rationale:    "Matched the " + best.Preset.Label + " preset: " + best.Preset.Description,
		Alternatives: matches[1:],
	}
}

func bespokeDraft(description string) Draft {
	instruction := strings.TrimSpace(description)
	return Draft{
		PresetID:         "",
		PolicyType:       PolicyTypePromptBased,
		Name:             bespokeName(instruction),
		Action:           "flag",
		Score:            defaultBespokeScore,
		Sources:          nil,
		PresidioEntities: nil,
		Prompt:           instruction,
		UserMessage:      "",
	}
}

func bespokeName(instruction string) string {
	runes := []rune(instruction)
	if len(runes) > maxBespokeNameRunes {
		cut := maxBespokeNameRunes
		for cut > 0 && !unicode.IsSpace(runes[cut]) {
			cut--
		}
		if cut == 0 {
			cut = maxBespokeNameRunes
		}
		runes = runes[:cut]
	}
	name := strings.TrimRight(string(runes), " .,;:")
	if name == "" {
		return "Custom guardrail"
	}
	return strings.ToUpper(name[:1]) + name[1:]
}
