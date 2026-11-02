package chatredact

import (
	"regexp"
	"strings"
)

// The secret scanner only finds credentials. These patterns mask the personal,
// financial, government and healthcare identifiers that most often appear in
// support chats. They favour precision so order numbers and timestamps
// survive: phone numbers need separators, card numbers must start with an
// issuer digit and pass the Luhn check, SSNs must use issuable area and group
// numbers (or a label when undashed) and medical record numbers need a label
// and a digit.
var (
	piiEmail = regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	piiPhone = regexp.MustCompile(`(?:\+?1[\s.\-]?)?(?:\(\d{3}\)\s?|\b\d{3}[\s.\-])\d{3}[\s.\-]\d{4}\b|\+\d{1,3}[\s.\-]?\(\d{1,4}\)(?:[\s.\-]?\d){5,11}\b|\+\d{1,3}(?:[\s.\-]?\d){7,13}\b`)
	piiCard  = regexp.MustCompile(`\b[2-6](?:[ \-]?\d){12,18}\b`)
	piiSSN   = regexp.MustCompile(`\b(\d{3})-(\d{2})-(\d{4})\b`)
	// piiLabelledSSN catches SSNs written without dashes, which are too
	// ambiguous to mask unlabelled. It keeps the label.
	piiLabelledSSN = regexp.MustCompile(`(?i)\b(SSN|social security(?: number| no\.?| #)?)([\s:#\-]*)\d{3}[\s\-]?\d{2}[\s\-]?\d{4}\b`)
	// piiMedicalRecord keeps the label and masks the identifier after it.
	piiMedicalRecord = regexp.MustCompile(`(?i)\b(MRN|medical record(?: number| no\.?| #)?)([\s:#\-]*)[a-z]{0,4}-?\d[a-z0-9\-]{2,}`)
)

// maskPII masks personal identifiers in plain text. changed reports that
// something was masked.
func maskPII(text string) (string, bool) {
	out := piiMedicalRecord.ReplaceAllString(text, "${1}${2}"+redactedMarker)
	out = piiLabelledSSN.ReplaceAllString(out, "${1}${2}"+redactedMarker)
	out = piiEmail.ReplaceAllString(out, redactedMarker)
	out = piiSSN.ReplaceAllStringFunc(out, func(match string) string {
		parts := piiSSN.FindStringSubmatch(match)
		if validSSN(parts[1], parts[2], parts[3]) {
			return redactedMarker
		}
		return match
	})
	out = piiCard.ReplaceAllStringFunc(out, func(match string) string {
		if luhnValid(match) {
			return redactedMarker
		}
		return match
	})
	out = piiPhone.ReplaceAllString(out, redactedMarker)
	return out, out != text
}

const redactedMarker = "[REDACTED]"

// validSSN rejects numbers the SSA never issues: area 000, 666 or 900-999,
// group 00 and serial 0000.
func validSSN(area, group, serial string) bool {
	return area != "000" && area != "666" && area[0] != '9' && group != "00" && serial != "0000"
}

// luhnValid reports whether the digits in s, ignoring spaces and dashes,
// form a 13 to 19 digit number that passes the Luhn checksum.
func luhnValid(s string) bool {
	digits := strings.NewReplacer(" ", "", "-", "").Replace(s)
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	sum := 0
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if (len(digits)-i)%2 == 0 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}
