package presidiofp

import "strings"

// Bounds of the digit runs Presidio's CreditCardRecognizer can emit. Its
// pattern is a 4-digit leading group followed by three more groups of 3-5
// digits, so the shortest match is 13 digits and the longest 19 — which also
// happens to be the ISO/IEC 7812 PAN length window.
const (
	minCardDigits = 13
	maxCardDigits = 19
)

// nonCardReason returns a short, human-readable reason why a CREDIT_CARD match
// is a false positive, or "" when the value could be a real primary account
// number (PAN).
//
// This layer sees only the matched value. The noise class it cannot reach — a
// perfectly plausible PAN that is really an order id or a hash fragment — is
// handled by cardContextReason instead.
//
// Why the value-only layer matters: Presidio's CreditCardRecognizer matches
// `\b(?!1\d{12}(?!\d))((4\d{3})|(5[0-5]\d{2})|(6\d{3})|(1\d{3})|(3\d{3}))[- ]?(\d{3,4})[- ]?(\d{3,4})[- ]?(\d{3,5})\b`
// and, when the Luhn checksum passes, PatternRecognizer raises the score to 1.0
// with no context requirement. Roughly one in ten 16-digit runs beginning with
// 1, 3, 4, 5 or 6 clears that bar, so consecutive PR numbers printed by `gh`,
// concatenated ids, and any other digit soup in tool output is reported as
// cardholder data at maximum confidence (AIS-720).
//
// Four checks, in order:
//
//  1. Shape. Anything outside the 13-19 digit window (after the separators the
//     recognizer's own grammar allows are stripped) is left alone: it did not
//     come from this recognizer, so this catalog has nothing to say about it.
//  2. Luhn. The recognizer only reports checksum-valid runs, so a failing value
//     never was a card. The check is reimplemented here so the offline sweep can
//     re-judge a stored finding without calling the analyzer.
//  3. Published test PANs. Payment processors publish sandbox card numbers that
//     can never be charged, and they are everywhere in fixtures, seed data and
//     docs — including this repository's own demo seed. Claude Code's PostToolUse
//     hook ships the whole original file with every `Edit`, so one fixture-bearing
//     file re-reports the same PANs on every edit.
//  4. Placeholder shapes and unissued prefixes. A run built from two distinct
//     digits, or whose four-digit groups count up one by one, is a pattern rather
//     than an account; and a PAN always begins inside an issuer identification
//     range that ISO/IEC 7812 actually allocates, which Presidio's leading-group
//     alternation is far wider than.
func nonCardReason(match string) string {
	digits := cardDigits(match)
	if len(digits) < minCardDigits || len(digits) > maxCardDigits {
		return ""
	}
	if !luhnValid(digits) {
		return "fails the Luhn checksum"
	}
	if knownTestPANs[digits] {
		return "published payment-processor test card number"
	}
	if reason := placeholderCardShapeReason(digits); reason != "" {
		return reason
	}
	if !issuedCardPrefix(digits) {
		return "does not begin with an issued card-network prefix"
	}
	return ""
}

// cardContextReason reports a CREDIT_CARD match as noise when the text it was
// found in never mentions payment cards.
//
// The recognizer ships CONTEXT words ("credit", "card", "visa", ...) but
// Presidio only uses them to *raise* a score, never to gate a match, and a
// passing Luhn check pins the score at 1.0 before any context is consulted.
// This inverts that: a card-shaped digit run in a payload that never talks about
// payments is treated as an opaque identifier rather than cardholder data.
//
// The trade-off is deliberate and matches the UK NHS catalog above it. A real
// PAN pasted with no payment vocabulary anywhere in the payload is missed; in
// exchange, coding-agent traffic — where card-shaped runs are overwhelmingly
// ids, offsets and CLI output — stops producing warn challenges on a financial
// policy. The vocabulary below is deliberately generous (any network name, any
// processor name, any "card"-labelled field keeps the finding) because the
// value-only layer above already removes the two dominant noise families, and
// keeping a finding is the cheap direction.
//
// text is the whole scanned payload (or, for the offline sweep, the whole
// message the finding was anchored to), not a window around the match, for the
// same reason as the NHS layer: the two callers cannot agree on offsets, and
// scanning everything only ever keeps more findings.
//
// An empty text means "context unknown", and no finding is suppressed on that
// basis.
func cardContextReason(text string) string {
	if text == "" {
		return ""
	}
	lower := strings.ToLower(text)
	for _, word := range cardContextWords {
		if containsWord(lower, word) {
			return ""
		}
	}
	for _, term := range cardContextTerms {
		if strings.Contains(lower, term) {
			return ""
		}
	}
	return "card-shaped digit run with no payment-card context in the surrounding text"
}

// containsWord reports whether term occurs in lower as a standalone token,
// bounded on both sides by something that is not an ASCII letter or digit.
//
// This is what makes the recognizer's own generic context words usable as a
// gate. Presidio matches "credit" and "card" as substrings, which fire inside
// "credited", "discard", "wildcard", "cardinality" and "scorecard" — all
// ordinary vocabulary in the traffic this catalog de-noises. As tokens they
// still match every shape that actually labels a card: `Card:`, `"card":`,
// `card_number`, `CARD-NUMBER`, `credit_card`.
//
// lower must already be lowercased; bytes outside [a-z0-9] (punctuation,
// whitespace, and the leading bytes of any multi-byte rune) all count as
// boundaries.
func containsWord(lower, term string) bool {
	for offset := 0; offset+len(term) <= len(lower); {
		i := strings.Index(lower[offset:], term)
		if i < 0 {
			return false
		}
		start := offset + i
		end := start + len(term)
		if !isWordByte(lower, start-1) && !isWordByte(lower, end) {
			return true
		}
		offset = start + 1
	}
	return false
}

func isWordByte(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}
	c := s[i]
	return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
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

// luhnValid runs the mod-10 check every payment card carries: doubling every
// second digit from the right, casting out nines, and requiring the total to be
// divisible by ten. Same validation as Presidio's CreditCardRecognizer.
func luhnValid(digits string) bool {
	if digits == "" {
		return false
	}
	sum := 0
	double := false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}

// placeholderCardShapeReason reports the digit-pattern families that are
// card-shaped by construction rather than by being an account number. Each is
// something a person or a program typed as a series, so a passing Luhn check is
// coincidence.
func placeholderCardShapeReason(digits string) string {
	if distinctDigits(digits) <= 2 {
		return "built from at most two distinct digits"
	}
	if consecutiveGroups(digits) {
		return "a run of consecutive four-digit numbers, not one card"
	}
	if consecutiveRun(digits) {
		return "an unbroken ascending or descending digit run"
	}
	return ""
}

// distinctDigits counts how many of the ten digits appear in the run. Two or
// fewer is the 4111-1111-1111-1111 / 4242-4242-4242-4242 family: a random
// 16-digit number has a ~1-in-10^10 chance of being that repetitive, so this
// never costs a real card.
func distinctDigits(digits string) int {
	var seen uint16
	for i := range len(digits) {
		seen |= 1 << (digits[i] - '0')
	}
	n := 0
	for seen != 0 {
		n += int(seen & 1)
		seen >>= 1
	}
	return n
}

// consecutiveGroups reports whether the run splits into four-digit groups that
// step by exactly one, in either direction — the shape a `gh pr list` (or any
// other listing of adjacent ids) takes when four of them land side by side.
// Requires at least three groups so a 8-digit coincidence cannot trip it; the
// recognizer's own window means this is really the 16-digit case.
func consecutiveGroups(digits string) bool {
	const group = 4
	if len(digits)%group != 0 || len(digits)/group < 3 {
		return false
	}
	var prev, step int
	for i := 0; i < len(digits); i += group {
		n := 0
		for _, c := range digits[i : i+group] {
			n = n*10 + int(c-'0')
		}
		switch {
		case i == 0:
			prev = n
		case i == group:
			step = n - prev
			if step != 1 && step != -1 {
				return false
			}
			prev = n
		default:
			if n-prev != step {
				return false
			}
			prev = n
		}
	}
	return true
}

// consecutiveRun reports whether every digit but the last steps by one (mod
// ten) from the digit before it — 1234-5678-9012-345X and its descending twin.
// The final digit is excluded because a Luhn check digit is whatever the
// checksum demands, so it breaks the run in all but one case in ten.
func consecutiveRun(digits string) bool {
	if len(digits) < 5 {
		return false
	}
	body := digits[:len(digits)-1]
	step := int(body[1]-'0') - int(body[0]-'0')
	if step != 1 && step != -1 {
		// Normalize the single wrap (9->0 ascending, 0->9 descending) so a run
		// that happens to straddle it is still recognized.
		switch {
		case body[0] == '9' && body[1] == '0':
			step = 1
		case body[0] == '0' && body[1] == '9':
			step = -1
		default:
			return false
		}
	}
	for i := 1; i < len(body); i++ {
		want := (int(body[i-1]-'0') + step + 10) % 10
		if int(body[i]-'0') != want {
			return false
		}
	}
	return true
}

// cardPrefixRange is an inclusive range over a PAN's leading digits. low and
// high always have the same length, so the comparison is a plain lexicographic
// one over equal-length digit strings.
type cardPrefixRange struct {
	low  string
	high string
}

// issuedCardPrefixes is the set of issuer identification number (IIN) ranges
// that payment networks actually issue from. Presidio's leading-group
// alternation accepts any run starting 1, 3, 4, 5(0-5) or 6, which is far wider
// than the allocated space — 66xx, for example, matches the recognizer but
// belongs to no network, so a Luhn-valid run there was never a card.
//
// Sourced from ISO/IEC 7812 major industry identifiers and each network's
// published BIN ranges (Visa, Mastercard, American Express, Discover, Diners
// Club, JCB, UnionPay, Maestro, Mir, RuPay, Elo/Hipercard). Ranges are kept at
// the granularity the networks publish rather than narrowed to the BINs seen in
// practice: an over-narrow table would silently drop real cards, which is the
// expensive direction for this catalog.
//
// Some entries (the 2-series and 8-series) cannot currently be produced by the
// recognizer's regex at all. They are listed anyway so the table reads as "what
// a card can start with" rather than "what this Presidio version emits", and so
// a future upstream widening does not quietly turn real cards into noise.
var issuedCardPrefixes = []cardPrefixRange{
	{low: "1", high: "1"},       // UATP (air travel), 15 digits
	{low: "2200", high: "2204"}, // Mir
	{low: "2221", high: "2720"}, // Mastercard 2-series
	{low: "300", high: "305"},   // Diners Club International
	{low: "3095", high: "3095"}, // Diners Club International
	{low: "34", high: "34"},     // American Express
	{low: "3528", high: "3589"}, // JCB
	{low: "36", high: "36"},     // Diners Club International
	{low: "37", high: "37"},     // American Express
	{low: "38", high: "39"},     // Diners Club (UK/Ireland, enRoute)
	{low: "4", high: "4"},       // Visa
	{low: "50", high: "50"},     // Maestro
	{low: "51", high: "55"},     // Mastercard
	{low: "56", high: "58"},     // Maestro
	{low: "60", high: "60"},     // Discover (6011), RuPay
	{low: "62", high: "62"},     // UnionPay, Discover co-brands
	{low: "6304", high: "6304"}, // Maestro
	{low: "636", high: "636"},   // Elo
	{low: "637", high: "639"},   // InstaPayment
	{low: "64", high: "65"},     // Discover
	{low: "67", high: "67"},     // Maestro, Laser
	{low: "81", high: "82"},     // UnionPay, RuPay
}

// issuedCardPrefix reports whether the run begins inside one of the allocated
// IIN ranges.
func issuedCardPrefix(digits string) bool {
	for _, r := range issuedCardPrefixes {
		n := len(r.low)
		if len(digits) < n {
			continue
		}
		if prefix := digits[:n]; prefix >= r.low && prefix <= r.high {
			return true
		}
	}
	return false
}

// cardContextWords are the lowercased tokens that count as payment-card signal.
// They are matched with containsWord, so each must appear as a whole word.
//
// The list starts from the recognizer's own CONTEXT words — kept generic
// precisely because token matching makes them safe — and adds the networks,
// processors and acquiring vocabulary that name cardholder data without ever
// saying "card".
var cardContextWords = []string{
	// The recognizer's own context vocabulary.
	"card",
	"cards",
	"credit",
	"credits",
	"debit",
	"debits",
	"visa",
	"mastercard",
	"amex",
	"discover",
	"jcb",
	"diners",
	"maestro",
	"instapayment",

	// Networks the recognizer omits.
	"unionpay",
	"rupay",
	"interac",
	"elo",
	"hipercard",

	// Field names and card data that travel with a PAN.
	"cardholder",
	"pan",
	"cvv",
	"cvv2",
	"cvc",
	"cvc2",
	"expiry",
	"expiration",

	// Processors and acquiring vocabulary: a payload that talks to one of these
	// is handling cardholder data even when it never says "card".
	"stripe",
	"braintree",
	"adyen",
	"worldpay",
	"chargeback",
	"chargebacks",
	"acquirer",
	"pci",
}

// cardContextTerms are lowercased substrings for the compound spellings that
// token matching cannot see: a camelCase or run-together identifier has no
// boundary between its parts, so "cardNumber" lowercases to a single token that
// neither "card" nor "number" matches as a word.
var cardContextTerms = []string{
	"creditcard",
	"debitcard",
	"cardnumber",
	"cardnum",
	"cardbrand",
	"cardtype",
	"cardexpiry",
	"paymentcard",
	"paymentmethod",
	"expmonth",
	"expyear",
	"american express",
	"authorize.net",
	"checkout.com",
	"primary account number",
	"issuing bank",
	"merchant account",
	"security code",
}

// knownTestPANs is the set of published, non-chargeable sandbox card numbers
// from the payment networks' and processors' own documentation (Visa,
// Mastercard, American Express, Discover, Diners Club, JCB, UnionPay, Maestro,
// Elo/Hipercard; Stripe, Adyen, Braintree, PayPal, Worldpay). They are stored
// as bare digits; cardDigits strips separators before the lookup, so spaced and
// hyphenated spellings match too.
//
// These dominate the false positives in practice. They live in fixtures, seed
// data, SDK examples and docs — including this repository's own demo seed — and
// a coding agent's PostToolUse hook re-ships the whole file on every edit, so
// one fixture re-reports the same PANs indefinitely.
//
// The Stripe 4000-00* family is enumerated rather than collapsed into a BIN
// range: each entry is a documented behaviour trigger, and carving out the
// whole 400000 BIN would suppress a wider slice of Visa space than the
// documentation actually reserves.
//
// TestKnownTestPANsAreWellFormed keeps every entry Luhn-valid and in the
// recognizer's length window, so a mistyped digit fails CI rather than sitting
// in the map never matching anything.
var knownTestPANs = map[string]bool{
	// Visa.
	"4111111111111111": true,
	"4012888888881881": true,
	"4222222222222":    true,
	"4242424242424242": true,
	"4000056655665556": true,
	"4917610000000000": true,
	"4005519200000004": true,
	"4009348888881881": true,
	"4012000033330026": true,
	"4012000077777777": true,
	"4217651111111119": true,
	"4500600000000061": true,
	"4444333322221111": true,
	"4462030000000000": true,
	"4484070000000000": true,
	"4988438843884305": true,
	"4977949494949497": true,
	"4646464646464644": true,
	"4543474002249996": true,
	// Visa, Stripe's documented behaviour triggers.
	"4000000000000002": true,
	"4000000000000010": true,
	"4000000000000028": true,
	"4000000000000036": true,
	"4000000000000044": true,
	"4000000000000069": true,
	"4000000000000077": true,
	"4000000000000093": true,
	"4000000000000101": true,
	"4000000000000119": true,
	"4000000000000127": true,
	"4000000000000259": true,
	"4000000000000341": true,
	"4000000000002685": true,
	"4000000000003055": true,
	"4000000000003063": true,
	"4000000000003220": true,
	"4000000000005423": true,
	"4000000000009979": true,
	"4000000000009987": true,
	"4000000000009995": true,
	"4000002500003155": true,
	"4000002760003184": true,
	"4000008400001629": true,

	// Mastercard.
	"5555555555554444": true,
	"5105105105105100": true,
	"5200828282828210": true,
	"5555341244441115": true,
	"5454545454545454": true,
	"5431111111111111": true,
	"5111005111051128": true,
	"5577000055770004": true,
	"5555444433331111": true,
	"5500000000000004": true,
	"5425233430109903": true,
	"5100060000000002": true,
	// Mastercard 2-series.
	"2223003122003222": true,
	"2223000048400011": true,
	"2223000048410010": true,
	"2222400010000008": true,
	"2222400030000004": true,

	// American Express.
	"378282246310005": true,
	"371449635398431": true,
	"378734493671000": true,
	"340000000000009": true,
	"374245455400126": true,
	"375987000000005": true,
	"343434343434343": true,

	// Discover.
	"6011111111111117": true,
	"6011000990139424": true,
	"6011000400000000": true,
	"6011601160116611": true,
	"6011981111111113": true,
	"6445644564456445": true,

	// Diners Club.
	"30569309025904": true,
	"38520000023237": true,
	"36006666333344": true,
	"36148900647913": true,

	// JCB.
	"3530111333300000": true,
	"3566002020360505": true,
	"3569990010030400": true,
	"3528000700000000": true,

	// UnionPay.
	"6212345678901232": true,
	"6250941006528599": true,
	"6243030000000001": true,

	// Maestro.
	"6759649826438453": true,
	"6304000000000000": true,
	"5641821111166669": true,

	// Elo / Hipercard.
	"5066991111111118": true,
	"6362970000457013": true,
	"6062826786276634": true,
	"6060704495764400": true,
}
