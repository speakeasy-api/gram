// Package presidiofp classifies Presidio PII findings as false positives. It
// holds the per-entity false-positive catalogs (reserved/placeholder IPs and
// emails, cloud/CDN ASN attribution) and the dispatch over them. It is a leaf
// domain package with no Temporal or activity dependencies, so it can be reused
// from the scanner, from offline tools, or anywhere a stored finding needs to
// be re-evaluated.
package presidiofp

import "strings"

// EntityType is Presidio's UPPER_SNAKE entity name (e.g. "IP_ADDRESS").
type EntityType = string

const (
	EntityTypeEmailAddress EntityType = "EMAIL_ADDRESS"
	EntityTypeIPAddress    EntityType = "IP_ADDRESS"
)

// rulePrefix is the canonical rule_id prefix for Presidio PII findings. It
// mirrors risk_analysis.CanonicalPresidioRuleID's grammar ("pii." + lowercased
// entity); keep the two in sync.
const rulePrefix = "pii."

// Reason returns the catalog reason a Presidio match of the given entity type
// is treated as noise, or "" when it is a real finding. Only IP_ADDRESS and
// EMAIL_ADDRESS have catalogs today; other entity types always return "".
func Reason(entityType, match string) string {
	switch entityType {
	case EntityTypeIPAddress:
		return nonPIIIPReason(strings.TrimSpace(match))
	case EntityTypeEmailAddress:
		return nonPIIEmailReason(match)
	default:
		return ""
	}
}

// ReasonByRuleID is Reason keyed by a stored finding's canonical rule_id
// (e.g. "pii.ip_address"), for re-evaluating persisted findings. Rule ids
// outside the catalogs always return "".
func ReasonByRuleID(ruleID, match string) string {
	return Reason(entityTypeForRuleID(ruleID), match)
}

// RuleIDs returns the canonical rule ids whose entity types have a catalog.
// Callers re-scanning stored findings can use it to read only rows that could
// possibly be reclassified. Keep in sync with the switch in Reason.
func RuleIDs() []string {
	return []string{
		ruleIDForEntity(EntityTypeIPAddress),
		ruleIDForEntity(EntityTypeEmailAddress),
	}
}

// ruleIDForEntity maps a Presidio entity type to its canonical rule_id.
func ruleIDForEntity(entity EntityType) string {
	return rulePrefix + strings.ToLower(entity)
}

// entityTypeForRuleID inverts ruleIDForEntity: "pii.ip_address" -> "IP_ADDRESS".
// Returns "" for non-PII rule ids.
func entityTypeForRuleID(ruleID string) string {
	rest, ok := strings.CutPrefix(ruleID, rulePrefix)
	if !ok {
		return ""
	}
	return strings.ToUpper(rest)
}
