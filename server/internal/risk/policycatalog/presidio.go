package policycatalog

import (
	"slices"
	"strings"
)

const (
	PresidioSource         = "presidio"
	PresidioRulePrefix     = "pii."
	PresidioDeadLetterRule = PresidioRulePrefix + "dead_letter"
)

var presidioEntities = []string{
	"AU_TFN",
	"CREDIT_CARD",
	"CRYPTO",
	"EMAIL_ADDRESS",
	"ES_NIF",
	"HARMFUL_CONTENT_REQUEST",
	"IBAN_CODE",
	"IN_AADHAAR",
	"IN_PAN",
	"IP_ADDRESS",
	"IT_FISCAL_CODE",
	"MAC_ADDRESS",
	"MEDICAL_BIOLOGICAL_ATTRIBUTE",
	"MEDICAL_CLINICAL_EVENT",
	"MEDICAL_DISEASE_DISORDER",
	"MEDICAL_FAMILY_HISTORY",
	"MEDICAL_LICENSE",
	"MEDICAL_MEDICATION",
	"MEDICAL_THERAPEUTIC_PROCEDURE",
	"PHONE_NUMBER",
	"POLICY_VIOLATION",
	"SG_NRIC_FIN",
	"TOPIC_BOUNDARY_VIOLATION",
	"UK_NHS",
	"UK_NINO",
	"UK_PASSPORT",
	"UNAUTHORIZED_ACTION",
	"US_BANK_NUMBER",
	"US_ITIN",
	"US_MBI",
	"US_NPI",
	"US_PASSPORT",
	"US_SSN",
}

func CanonicalPresidioRuleID(entity string) string {
	return PresidioRulePrefix + strings.ToLower(entity)
}

func PresidioEntityForRuleID(ruleID string) (string, bool) {
	if !strings.HasPrefix(ruleID, PresidioRulePrefix) {
		return "", false
	}
	suffix := strings.TrimPrefix(ruleID, PresidioRulePrefix)
	if suffix == "" || suffix != strings.ToLower(suffix) {
		return "", false
	}
	entity := strings.ToUpper(suffix)
	if slices.Contains(presidioEntities, entity) {
		return entity, true
	}
	return "", false
}
