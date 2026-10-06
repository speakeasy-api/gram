package policycatalog

import (
	"sort"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/risk/categories"
)

// SourceDescriptions explains each detector source in product language so a
// caller choosing from the catalog knows what it is turning on.
var SourceDescriptions = map[string]string{
	"gitleaks":         "Secrets: API keys, tokens, private keys and other credentials, matched by pattern.",
	"presidio":         "Personal data: emails, phone numbers, card and bank numbers, government IDs and healthcare identifiers. Pick the entities to detect with presidio_entities.",
	"prompt_injection": "Prompt injection: jailbreaks, instruction overrides and hidden instructions in prompts or tool outputs, judged semantically.",
	"shadow_mcp":       "Shadow MCP: tool calls reaching MCP servers the organization has not issued or approved. Needs Speakeasy hooks on the agent.",
	"destructive_tool": "Destructive tools: MCP tool calls whose tool definition is annotated destructive. Flag only.",
	"cli_destructive":  "Destructive CLI commands: rm -rf, git push --force, DROP TABLE, kubectl delete and similar in tool arguments. Flag only.",
	"account_identity": "Non-corporate accounts: sessions signed in with a personal AI account or an email domain outside approved_email_domains. Flag only.",
}

// ActionDescriptions explains what each enforcement action does at runtime.
var ActionDescriptions = map[string]string{
	"flag":  "Log the finding for review without interrupting the session.",
	"warn":  "Warn the user and require acknowledgement before the action proceeds; falls back to blocking where confirmation is not possible.",
	"block": "Deny the prompt or tool call that matched.",
}

// PolicyTypeDescriptions explains when to pick each policy type.
var PolicyTypeDescriptions = map[string]string{
	"standard":     "Built-in detectors chosen with sources and presidio_entities. Deterministic and cheap; use when the risk is a recognizable pattern such as a credential or a card number.",
	"prompt_based": "A plain-language instruction judged by the policy model on each in-scope message. Use when the risk is about intent or context, such as destructive changes to production.",
}

//nolint:gosec // Plain-language names of entity types, not credentials.
var presidioEntityDescriptions = map[string]string{
	"AU_TFN":                        "Australian tax file number",
	"CREDIT_CARD":                   "Payment card number",
	"CRYPTO":                        "Cryptocurrency wallet address",
	"EMAIL_ADDRESS":                 "Email address",
	"ES_NIF":                        "Spanish tax identifier",
	"IBAN_CODE":                     "International bank account number",
	"IN_AADHAAR":                    "Indian Aadhaar number",
	"IN_PAN":                        "Indian permanent account number",
	"IP_ADDRESS":                    "IPv4 or IPv6 address",
	"IT_FISCAL_CODE":                "Italian fiscal code",
	"MAC_ADDRESS":                   "Hardware MAC address",
	"MEDICAL_BIOLOGICAL_ATTRIBUTE":  "Biological attribute in a medical context",
	"MEDICAL_CLINICAL_EVENT":        "Clinical event or procedure mention",
	"MEDICAL_DISEASE_DISORDER":      "Disease or disorder mention",
	"MEDICAL_FAMILY_HISTORY":        "Family medical history mention",
	"MEDICAL_LICENSE":               "Medical license number",
	"MEDICAL_MEDICATION":            "Medication mention",
	"MEDICAL_THERAPEUTIC_PROCEDURE": "Therapeutic procedure mention",
	"PHONE_NUMBER":                  "Phone number",
	"SG_NRIC_FIN":                   "Singapore NRIC or FIN",
	"UK_NHS":                        "UK NHS number",
	"UK_NINO":                       "UK national insurance number",
	"UK_PASSPORT":                   "UK passport number",
	"US_BANK_NUMBER":                "US bank account number",
	"US_ITIN":                       "US individual taxpayer identification number",
	"US_MBI":                        "US Medicare beneficiary identifier",
	"US_NPI":                        "US national provider identifier",
	"US_PASSPORT":                   "US passport number",
	"US_SSN":                        "US social security number",
}

// PresidioEntityDescription returns the plain-language meaning of one entity.
func PresidioEntityDescription(entity string) string {
	return presidioEntityDescriptions[entity]
}

// CategoryDescriptions maps each detection scope category to its dashboard
// description, taken from the canonical category definitions.
func CategoryDescriptions() map[string]string {
	out := make(map[string]string, len(categories.Definitions))
	for _, def := range categories.Definitions {
		if _, seen := out[string(def.Category)]; seen {
			continue
		}
		out[string(def.Category)] = def.Description
	}
	return out
}

// DescribeValues renders "value: meaning" lines for an enum so the meaning
// travels with the schema. Values without a description are listed bare.
func DescribeValues(values []string, describe func(string) string) string {
	sorted := make([]string, len(values))
	copy(sorted, values)
	sort.Strings(sorted)
	lines := make([]string, 0, len(sorted))
	for _, value := range sorted {
		if text := strings.TrimSpace(describe(value)); text != "" {
			lines = append(lines, value+": "+text)
			continue
		}
		lines = append(lines, value)
	}
	return strings.Join(lines, "\n")
}
