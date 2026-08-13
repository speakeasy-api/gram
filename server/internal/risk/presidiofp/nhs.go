package presidiofp

import "strings"

// nonNHSReason returns a short, human-readable reason why a UK_NHS match is a
// false positive, or "" when the value could be a real NHS number.
//
// This layer sees only the matched value, so it can rule out digit runs that
// are not valid NHS numbers at all. The much larger noise class — a perfectly
// valid-looking 10-digit run that is really a Confluence page id, a Unix
// timestamp, or an order number — is indistinguishable by value alone and is
// handled by nhsContextReason instead.
//
// Why the value-only layer matters: Presidio's NhsRecognizer matches
// `\b(\d{3})[- ]?(\d{3})[- ]?(\d{4})\b` and, when its mod-11 checksum passes,
// PatternRecognizer raises the score to 1.0 with no context requirement. Any
// 10-digit identifier therefore has a ~1-in-11 chance of being reported as a
// UK National Health Service number at maximum confidence.
//
// Three checks, in order:
//
//  1. Shape. Anything that is not exactly ten digits after separators are
//     stripped is left alone: it did not come from the NHS recognizer's own
//     grammar, so this catalog has nothing to say about it.
//  2. Check digit. NHS numbers carry a mod-11 check digit (the same validation
//     the recognizer runs). A run that fails it is not an NHS number.
//  3. Allocation range. NHS/CHI/H&C/IHI numbers are only issued from the
//     ranges recorded in nhsAllocatedRanges. A checksum-valid run outside them
//     was never issued to a patient.
//
// Deliberately NOT filtered here: repeated or sequential digit runs
// (0000000000, 1234567890). They are placeholder-shaped, but they also fail
// the check digit in almost every case, so a dedicated rule would be dead
// weight, and the ones that do pass are cleared by the context layer.
func nonNHSReason(match string) string {
	digits := nhsDigits(match)
	if len(digits) != 10 {
		return ""
	}
	if !nhsCheckDigitValid(digits) {
		return "fails the NHS number check digit"
	}
	if !nhsAllocated(digits) {
		return "outside the allocated NHS number ranges"
	}
	return ""
}

// nhsContextReason reports a UK_NHS match as noise when the text it was found
// in carries no NHS signal at all.
//
// The recognizer already ships CONTEXT words ("nhs", "national health
// service", ...) but Presidio only uses them to *raise* a score, never to gate
// a match, and a passing checksum pins the score at 1.0 before any context is
// consulted. This inverts that: a ten-digit run in a payload that never
// mentions health care anywhere is treated as an opaque identifier rather than
// a patient number.
//
// text is the whole scanned payload (or, for the offline sweep, the whole
// message the finding was anchored to), not a window around the match. A
// window would be tighter, but the two callers cannot agree on offsets — the
// sweep re-locates a stored match inside re-fetched text — and scanning
// everything only ever keeps more findings, which is the safe direction.
//
// An empty text means "context unknown", and no finding is suppressed on that
// basis.
func nhsContextReason(text string) string {
	if text == "" {
		return ""
	}
	lower := strings.ToLower(text)
	for _, term := range nhsContextTerms {
		if strings.Contains(lower, term) {
			return ""
		}
	}
	return "ten-digit identifier with no NHS context in the surrounding text"
}

// nhsDigits strips the separators the NHS recognizer's own grammar allows
// (spaces and hyphens, per its replacement_pairs) and returns the remaining
// characters only when every one of them is a digit. Anything else returns ""
// so callers treat the value as out of scope rather than mis-measuring it.
func nhsDigits(match string) string {
	var b strings.Builder
	b.Grow(len(match))
	for _, r := range strings.TrimSpace(match) {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-':
			continue
		default:
			return ""
		}
	}
	return b.String()
}

// nhsCheckDigitValid runs the NHS Digital mod-11 check: each of the ten digits
// is weighted 10 down to 1 and the weighted sum must be divisible by 11. This
// is the same validation Presidio's NhsRecognizer applies, reimplemented here
// so a stored finding can be re-checked offline without calling the analyzer.
func nhsCheckDigitValid(digits string) bool {
	total := 0
	for i, r := range digits {
		total += int(r-'0') * (10 - i)
	}
	return total%11 == 0
}

// nhsRange is an inclusive range over the first nine digits of an NHS number
// (the tenth is the check digit).
type nhsRange struct {
	low  int
	high int
}

// nhsAllocatedRanges is the set of nine-digit prefixes that identify a real
// person somewhere in the UK/Ireland numbering scheme. Everything outside them
// has never been issued, so a checksum-valid run there is an unrelated
// identifier that happens to validate.
//
// Sourced from the NHS Data Dictionary (HEALTH AND CARE NUMBER), NHS England's
// NHS-number guidance and the UK FCI NHS number range table:
//
//	010 000 000 - 319 999 999  Scotland (CHI numbers, DDMMYY-prefixed)
//	320 000 000 - 399 999 999  Northern Ireland (Health & Care numbers)
//	400 000 000 - 499 999 999  England, Wales, Isle of Man
//	600 000 000 - 799 999 999  England, Wales, Isle of Man
//	800 000 000 - 859 999 999  Republic of Ireland (IHI)
//
// Two gaps are deliberate. 000 000 000 - 009 999 999 is unissued, and it is
// where zero-padded internal ids land. 860 000 000 - 999 999 999 is unissued
// too; it contains NHS England's 999-prefixed test range, which is valid by
// checksum but reserved so it can never belong to a patient.
var nhsAllocatedRanges = []nhsRange{
	{low: 10000000, high: 319999999},
	{low: 320000000, high: 399999999},
	{low: 400000000, high: 499999999},
	{low: 600000000, high: 799999999},
	{low: 800000000, high: 859999999},
}

// nhsAllocated reports whether the ten-digit run's leading nine digits fall in
// an issued range.
func nhsAllocated(digits string) bool {
	prefix := 0
	for _, r := range digits[:9] {
		prefix = prefix*10 + int(r-'0')
	}
	for _, rng := range nhsAllocatedRanges {
		if prefix >= rng.low && prefix <= rng.high {
			return true
		}
	}
	return false
}

// nhsContextTerms are the lowercased substrings that count as NHS signal in the
// surrounding text. The list starts from the recognizer's own CONTEXT words and
// adds the sibling schemes that share the ten-digit format, so a Scottish CHI
// or Northern Irish Health & Care number is not suppressed for lacking the
// letters "nhs".
//
// Substring matching is intentional: "nhs" also fires inside "nhsNumber" and
// "NHS_NUMBER", the shapes these values take in JSON payloads and column names,
// which a word-boundary match would miss.
var nhsContextTerms = []string{
	"nhs",
	"national health service",
	"health service",
	"health authority",
	"health and care number",
	"health & care number",
	"chi number",
	"patient",
	"medical record",
	"hospital number",
}
