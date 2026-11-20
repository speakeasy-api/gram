package presidiofp

import "strings"

// Bounds of a primary account number (PAN) per ISO/IEC 7812. Presidio's
// CreditCardRecognizer only emits runs inside this window.
const (
	minCardDigits = 13
	maxCardDigits = 19
)

// cardIDListMaxSpan bounds how far apart the four groups of an id listing may
// be. Four sorted ids within a hundred of each other is the shape of a `gh pr
// list` or similar CLI output; a random card number lands there roughly once
// in three million draws.
const cardIDListMaxSpan = 100

// nonCardReason returns a short, human-readable reason why a CREDIT_CARD match
// is a false positive, or "" when the value could be a real card number.
//
// Presidio's CreditCardRecognizer matches any 13-17 digit run starting with 1,
// 3, 4, 5[0-5] or 6 and, when the Luhn checksum passes, PatternRecognizer pins
// the score at 1.0 without consulting its context words. Roughly one in ten
// card-shaped runs passes Luhn, so published test cards in fixtures and lists
// of adjacent ids in CLI output are reported as cardholder data at maximum
// confidence.
//
// This layer judges the value alone and only rules out values that cannot be a
// real customer's card, so it never needs the surrounding text:
//
//  1. Published test PANs. Payment networks and processors document sandbox
//     card numbers that can never be charged. They live in fixtures, seed data
//     and docs, and a coding agent's PostToolUse hook re-ships the whole file on
//     every edit, so one fixture re-reports the same numbers indefinitely.
//  2. Placeholder shapes: a run built from at most two distinct digits
//     (4444 5555 5555 5555), or one unbroken ascending or descending digit
//     sequence (1234 5678 9012 3452).
//  3. Id listings: four sorted four-digit groups close together, such as four
//     PR numbers printed side by side (6651 6652 6653 6654).
//
// Deliberately NOT filtered: a plausible PAN with no card vocabulary around it.
// Neither scanner knows which surface (user message, tool request, tool
// response) a payload came from, and dropping every PAN in a payload without
// payment words would also drop real card numbers in opaque JSON or logs.
func nonCardReason(match string) string {
	digits := cardDigits(match)
	if len(digits) < minCardDigits || len(digits) > maxCardDigits {
		return ""
	}
	if knownTestPANs[digits] {
		return "published test card number"
	}
	if distinctDigits(digits) <= 2 {
		return "placeholder card number built from at most two distinct digits"
	}
	if sequentialDigits(digits) {
		return "placeholder card number made of sequential digits"
	}
	if cardIDListing(digits) {
		return "list of nearby four-digit ids, not a card number"
	}
	return ""
}

// cardDigits strips the separators the credit-card recognizer's own grammar
// allows (spaces and hyphens, per its replacement_pairs) and returns the
// remaining characters only when every one of them is a digit. Anything else
// returns "" so callers treat the value as out of scope rather than
// mis-measuring it.
func cardDigits(match string) string {
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

func distinctDigits(digits string) int {
	var seen [10]bool
	n := 0
	for i := range len(digits) {
		d := digits[i] - '0'
		if !seen[d] {
			seen[d] = true
			n++
		}
	}
	return n
}

// sequentialDigits reports whether every digit but the last steps by one (mod
// ten, in a single direction) from the digit before it. The last digit is
// ignored because a Luhn check digit is whatever the checksum demands.
func sequentialDigits(digits string) bool {
	body := digits[:len(digits)-1]
	step := (int(body[1]) - int(body[0]) + 10) % 10
	if step != 1 && step != 9 {
		return false
	}
	for i := 2; i < len(body); i++ {
		if (int(body[i])-int(body[i-1])+10)%10 != step {
			return false
		}
	}
	return true
}

// cardIDListing reports whether a 16-digit run splits into four strictly
// ascending or strictly descending four-digit groups that all fall within
// cardIDListMaxSpan of each other.
func cardIDListing(digits string) bool {
	if len(digits) != 16 {
		return false
	}
	var groups [4]int
	for i := range groups {
		for _, c := range digits[i*4 : i*4+4] {
			groups[i] = groups[i]*10 + int(c-'0')
		}
	}
	ascending, descending := true, true
	for i := 1; i < len(groups); i++ {
		ascending = ascending && groups[i] > groups[i-1]
		descending = descending && groups[i] < groups[i-1]
	}
	if !ascending && !descending {
		return false
	}
	span := groups[3] - groups[0]
	if span < 0 {
		span = -span
	}
	return span <= cardIDListMaxSpan
}

// knownTestPANs is the set of published, non-chargeable sandbox card numbers
// from the payment networks' and processors' documentation (Visa, Mastercard,
// American Express, Discover, Diners Club, JCB, UnionPay, Maestro; Stripe,
// Adyen, Braintree, PayPal, Worldpay), plus a few example numbers that
// circulate widely in docs and fake-data generators. Stored as bare digits;
// cardDigits strips separators before the lookup.
//
// Keep in sync with KNOWN_TEST_PANS in
// pystreams/src/pystreams/risk/presidiofp/creditcard.py.
var knownTestPANs = map[string]bool{
	// Visa.
	"4111111111111111": true,
	"4242424242424242": true,
	"4012888888881881": true,
	"4222222222222":    true,
	"4000056655665556": true,
	"4005519200000004": true,
	"4009348888881881": true,
	"4012000033330026": true,
	"4012000077777777": true,
	"4217651111111119": true,
	"4444333322221111": true,
	"4462030000000000": true,
	"4484070000000000": true,
	"4500600000000061": true,
	"4543474002249996": true,
	"4646464646464644": true,
	"4917300800000000": true,
	"4917610000000000": true,
	"4988438843884305": true,
	"4539148803436467": true,
	// Visa, Stripe's documented decline and authentication triggers.
	"4000000000000002": true,
	"4000000000000069": true,
	"4000000000000077": true,
	"4000000000000119": true,
	"4000000000000127": true,
	"4000000000000341": true,
	"4000000000003055": true,
	"4000000000003063": true,
	"4000000000003220": true,
	"4000000000009979": true,
	"4000000000009987": true,
	"4000000000009995": true,
	"4000002500001001": true,
	"4000002500003155": true,
	"4000002760003184": true,
	"4000008400001629": true,

	// Mastercard.
	"5555555555554444": true,
	"5200828282828210": true,
	"5105105105105100": true,
	"5454545454545454": true,
	"5431111111111111": true,
	"5500000000000004": true,
	"5555341244441115": true,
	"5555444433331111": true,
	"5577000055770004": true,
	"2223003122003222": true,
	"2222400010000008": true,
	"2223000048400011": true,

	// American Express.
	"378282246310005": true,
	"371449635398431": true,
	"378734493671000": true,
	"340000000000009": true,
	"370000000000002": true,
	"374245455400126": true,
	"343434343434343": true,

	// Discover.
	"6011111111111117": true,
	"6011000990139424": true,
	"6011000400000000": true,
	"6011601160116611": true,
	"6011981111111113": true,
	"6445644564456445": true,

	// Diners Club.
	"30569309025904":   true,
	"38520000023237":   true,
	"36006666333344":   true,
	"36148900647913":   true,
	"36227206271667":   true,
	"3056930009020004": true,

	// JCB.
	"3530111333300000": true,
	"3566002020360505": true,
	"3566111111111113": true,
	"3569990010030400": true,
	"3528000700000000": true,

	// UnionPay.
	"6200000000000005":    true,
	"6205500000000000004": true,
	"6243030000000001":    true,
	"6250941006528599":    true,

	// Maestro.
	"6759649826438453": true,
	"6304000000000000": true,
}
