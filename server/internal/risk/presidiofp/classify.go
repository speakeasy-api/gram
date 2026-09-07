// Package presidiofp classifies Presidio PII findings as false positives. It
// holds the per-entity false-positive catalogs (reserved/placeholder IPs and
// emails, cloud/CDN ASN attribution, NHS number validity, retired recognizers)
// and the dispatch over them. It is a leaf domain package with no Temporal or
// activity dependencies, so it can be reused from the scanner, from offline
// tools, or anywhere a stored finding needs to be re-evaluated.
package presidiofp

import "strings"

// EntityType is Presidio's UPPER_SNAKE entity name (e.g. "IP_ADDRESS").
type EntityType = string

const (
	EntityTypeEmailAddress    EntityType = "EMAIL_ADDRESS"
	EntityTypeIPAddress       EntityType = "IP_ADDRESS"
	EntityTypeUKNHS           EntityType = "UK_NHS"
	EntityTypeUSDriverLicense EntityType = "US_DRIVER_LICENSE"
)

// rulePrefix is the canonical rule_id prefix for Presidio PII findings. It
// mirrors risk_analysis.CanonicalPresidioRuleID's grammar ("pii." + lowercased
// entity); keep the two in sync.
const rulePrefix = "pii."

// Reason returns the catalog reason a Presidio match of the given entity type
// is treated as noise, or "" when it is a real finding. Entity types without a
// catalog always return "".
//
// This is the value-only view: it judges the matched text on its own. Callers
// that hold the text the match was found in should prefer ReasonInContext,
// which additionally applies the catalogs that need surrounding text.
func Reason(entityType, match string) string {
	return ReasonInContext(entityType, match, "")
}

// ReasonInContext is Reason with the payload the match was found in. Passing an
// empty text is equivalent to calling Reason: no finding is ever suppressed for
// missing context, only for context that is present and carries no signal.
func ReasonInContext(entityType, match, text string) string {
	if reason := retiredReason(entityType); reason != "" {
		return reason
	}
	switch entityType {
	case EntityTypeIPAddress:
		return nonPIIIPReason(strings.TrimSpace(match))
	case EntityTypeEmailAddress:
		return nonPIIEmailReason(match)
	case EntityTypeUKNHS:
		if reason := nonNHSReason(match); reason != "" {
			return reason
		}
		return nhsContextReason(text)
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

// ReasonByRuleIDInContext is ReasonInContext keyed by a stored finding's
// canonical rule_id, for the offline sweep that re-reads the message a finding
// was anchored to.
func ReasonByRuleIDInContext(ruleID, match, text string) string {
	return ReasonInContext(entityTypeForRuleID(ruleID), match, text)
}

// RuleIDs returns the canonical rule ids whose entity types have a catalog.
// Callers re-scanning stored findings can use it to read only rows that could
// possibly be reclassified. Keep in sync with the switch in ReasonInContext.
func RuleIDs() []string {
	ids := []string{
		ruleIDForEntity(EntityTypeIPAddress),
		ruleIDForEntity(EntityTypeEmailAddress),
		ruleIDForEntity(EntityTypeUKNHS),
	}
	for _, entity := range RetiredEntityTypes() {
		ids = append(ids, ruleIDForEntity(entity))
	}
	return ids
}

// ContextRuleIDs returns the subset of RuleIDs whose classification can change
// once the surrounding text is supplied. The sweep uses it to decide which rows
// are worth re-reading a message for.
func ContextRuleIDs() []string {
	return []string{ruleIDForEntity(EntityTypeUKNHS)}
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
