package presidiofp

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// syntheticVisa is a Luhn-valid, correctly-prefixed PAN that is not published
// in any processor's test-card documentation, so it stands in for "a real card"
// in the tests below. Used only as the value that MUST survive the catalog.
const syntheticVisa = "4539172846305125"

// TestNonCardReason covers the value-only layer: what a card-shaped digit run
// says about itself before any surrounding text is consulted.
func TestNonCardReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		match  string
		expect bool // true when the value alone proves it is not a card number
	}{
		// Real-shaped PANs: this layer must let them through.
		{name: "synthetic visa", match: syntheticVisa, expect: false},
		{name: "synthetic visa spaced", match: "4539 1728 4630 5125", expect: false},
		{name: "synthetic visa hyphenated", match: "4539-1728-4630-5125", expect: false},
		{name: "synthetic mastercard", match: "5534129876004319", expect: false},
		{name: "synthetic amex", match: "371882450931763", expect: false},

		// Published sandbox PANs from processor documentation.
		{name: "visa universal test pan", match: "4111111111111111", expect: true},
		{name: "visa universal test pan spaced", match: "4111 1111 1111 1111", expect: true},
		{name: "stripe visa", match: "4242424242424242", expect: true},
		{name: "mastercard test pan", match: "5555555555554444", expect: true},
		{name: "amex test pan", match: "378282246310005", expect: true},
		{name: "discover test pan", match: "6011111111111117", expect: true},
		{name: "diners test pan", match: "30569309025904", expect: true},
		{name: "jcb test pan", match: "3530111333300000", expect: true},

		// Placeholder shapes.
		{name: "two distinct digits", match: "4141414141414141", expect: true},
		{name: "consecutive four-digit groups", match: "4009401040114012", expect: true},
		{name: "consecutive digit run", match: "1234567890123452", expect: true},

		// Prefixes no network issues from.
		{name: "unissued 66 series", match: "6651665266536654", expect: true},
		{name: "unissued 61 series", match: "6135802974216083", expect: true},

		// Presidio only reports checksum-valid runs, so a failing one is noise
		// by construction (and the offline sweep re-checks stored values).
		{name: "luhn fails", match: "4539172846305126", expect: true},

		// Out of this catalog's scope: the recognizer never emits these shapes,
		// so they are left for another lane.
		{name: "too short", match: "453917284630", expect: false},
		{name: "too long", match: "45391728463051250000", expect: false},
		{name: "not digits", match: "4539-1728-4630-51AB", expect: false},
		{name: "empty", match: "", expect: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reason := nonCardReason(tt.match)
			if tt.expect {
				assert.NotEmpty(t, reason)
				return
			}
			assert.Empty(t, reason)
		})
	}
}

// TestCardContextReason covers the layer that reads the payload a match came
// from. Presidio's recognizer ships these context words but only lets them
// raise a score, so this is where they become a requirement.
func TestCardContextReason(t *testing.T) {
	t.Parallel()

	kept := []string{
		"Customer credit card " + syntheticVisa,
		"Card: " + syntheticVisa,
		`{"card": {"number": "` + syntheticVisa + `"}}`,
		`{"card_number": "` + syntheticVisa + `"}`,
		`{"cardNumber":"` + syntheticVisa + `","cvv":"123"}`,
		"CARD_NUMBER=" + syntheticVisa,
		"charge the Visa ending 5125",
		"stripe.paymentMethods.create({ number: '" + syntheticVisa + "' })",
		"cardholder data must never be logged",
		"PCI DSS scope review",
	}
	for _, text := range kept {
		assert.Emptyf(t, cardContextReason(text), "should keep: %q", text)
	}

	suppressed := []string{
		"gh pr view 6651 6652 6653 6654",
		`{"trace_id": "` + syntheticVisa + `", "level": "info"}`,
		"order " + syntheticVisa + " shipped",
		// The words Presidio's own CONTEXT list would have matched, in the
		// ordinary code senses that make substring matching unusable as a gate.
		"discard the wildcard entry and recompute cardinality",
		"the account was credited last night",
		"scorecard rendered by DiscoveryPanel",
	}
	for _, text := range suppressed {
		assert.NotEmptyf(t, cardContextReason(text), "should suppress: %q", text)
	}

	// Unknown context is not evidence of anything.
	assert.Empty(t, cardContextReason(""))
}

// TestContainsWord pins the token matcher the context layer is built on: the
// generic words the recognizer ships are only safe as a gate because they are
// matched as whole tokens, with punctuation and separators counting as
// boundaries.
func TestContainsWord(t *testing.T) {
	t.Parallel()

	for _, text := range []string{"card", "card: 1", `{"card":1}`, "card_number", "card-number", "a card here", "CARD"} {
		assert.Truef(t, containsWord(strings.ToLower(text), "card"), "should match: %q", text)
	}
	for _, text := range []string{"discard", "wildcard", "cardinality", "scorecard", "cards2", "", "car"} {
		assert.Falsef(t, containsWord(strings.ToLower(text), "card"), "should not match: %q", text)
	}
}

// TestCreditCardRegressions is the regression set this catalog exists for
// (AIS-720): the exact traffic shapes a financial-data policy was warning on.
func TestCreditCardRegressions(t *testing.T) {
	t.Parallel()

	// A fixture-bearing file re-shipped by a coding agent's PostToolUse hook.
	// It talks about cards all over, so only the test-PAN layer can clear it.
	seedFile := `INSERT INTO risk_results (match, rule_id) VALUES
	('4111 1111 1111 1111', 'pii.credit_card'),
	('4242424242424242', 'pii.credit_card');`
	for _, pan := range []string{"4111 1111 1111 1111", "4242424242424242"} {
		assert.NotEmptyf(t, ReasonInContext(EntityTypeCreditCard, pan, seedFile),
			"test fixture PAN %q must not flag", pan)
	}

	// A Bash tool request listing four consecutive PR numbers.
	ghCommand := "gh pr view 6651 6652 6653 6654 --json title"
	assert.NotEmpty(t, ReasonInContext(EntityTypeCreditCard, "6651 6652 6653 6654", ghCommand),
		"consecutive PR numbers must not flag")

	// This very issue: the description quotes a test PAN alongside the words
	// "credit card", which is exactly the payload that got held.
	issueBody := "The Presidio CREDIT_CARD recognizer flags benign content, e.g. 4111111111111111."
	assert.NotEmpty(t, ReasonInContext(EntityTypeCreditCard, "4111111111111111", issueBody))

	// A genuine-looking card in a payload that names it still flags.
	assert.Empty(t, ReasonInContext(EntityTypeCreditCard, syntheticVisa,
		"Customer's credit card on file is "+syntheticVisa))
	assert.Empty(t, ReasonInContext(EntityTypeCreditCard, syntheticVisa,
		`{"payment_method": {"card": {"number": "`+syntheticVisa+`"}}}`))
}

// TestCreditCardSuppressesOpaqueIdentifiers quantifies the noise class: about
// one in ten card-shaped digit runs passes Luhn, and Presidio reports every one
// of those at maximum confidence. Sweeping a deterministic corpus of tool-output
// shapes proves none of them survives the catalog.
func TestCreditCardSuppressesOpaqueIdentifiers(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(720, 2))

	var checked int
	for range 20000 {
		// A 16-digit run whose leading group Presidio's regex accepts.
		id := fmt.Sprintf("%04d%012d", rng.IntN(3000)+4000, rng.Int64N(1_000_000_000_000))
		if !luhnValid(id) {
			continue // Presidio would not have reported it in the first place.
		}
		checked++
		text := "build artifact sha stream offset " + id + " written to disk"
		assert.NotEmptyf(t, ReasonInContext(EntityTypeCreditCard, id, text),
			"opaque identifier %s must not read as a card number", id)
	}
	require.Positive(t, checked, "corpus produced no checksum-valid runs")
}

// TestKnownTestPANsAreWellFormed keeps the published-test-card map honest. Every
// entry must be bare digits, Luhn-valid, and inside the recognizer's length
// window — a mistyped digit would otherwise sit in the map matching nothing.
func TestKnownTestPANsAreWellFormed(t *testing.T) {
	t.Parallel()

	require.NotEmpty(t, knownTestPANs)
	for pan := range knownTestPANs {
		assert.Equalf(t, pan, cardDigits(pan), "%s must be stored as bare digits", pan)
		assert.GreaterOrEqualf(t, len(pan), minCardDigits, "%s is shorter than the recognizer's window", pan)
		assert.LessOrEqualf(t, len(pan), maxCardDigits, "%s is longer than the recognizer's window", pan)
		assert.Truef(t, luhnValid(pan), "%s must pass the Luhn checksum", pan)
		assert.Truef(t, issuedCardPrefix(pan), "%s must start with an issued network prefix", pan)
	}
}

// TestIssuedCardPrefixesAreWellFormed locks the IIN table's shape: a range whose
// bounds differ in length, or whose low is above its high, would mean someone
// mistyped a boundary while editing it.
func TestIssuedCardPrefixesAreWellFormed(t *testing.T) {
	t.Parallel()

	for i, r := range issuedCardPrefixes {
		require.Lenf(t, r.high, len(r.low), "range %d has mismatched bound lengths", i)
		require.NotEmptyf(t, r.low, "range %d is empty", i)
		assert.LessOrEqualf(t, r.low, r.high, "range %d is inverted", i)
		assert.Equalf(t, r.low, cardDigits(r.low), "range %d low must be digits", i)
		assert.Equalf(t, r.high, cardDigits(r.high), "range %d high must be digits", i)
	}
}

// TestIssuedCardPrefixCoversTheNetworks checks the table against one PAN per
// network, so narrowing a range in the future fails here rather than silently
// suppressing a live card.
func TestIssuedCardPrefixCoversTheNetworks(t *testing.T) {
	t.Parallel()

	issued := map[string]string{
		"visa":                "4539172846305125",
		"mastercard":          "5534129876004319",
		"mastercard 2-series": "2223003122003222",
		"amex 34":             "340000000000009",
		"amex 37":             "371449635398431",
		"discover 6011":       "6011111111111117",
		"discover 65":         "6500000000000002",
		"diners 305":          "30569309025904",
		"diners 36":           "36006666333344",
		"jcb":                 "3530111333300000",
		"unionpay":            "6212345678901232",
		"maestro 67":          "6759649826438453",
		"uatp":                "135412345678911",
	}
	for name, pan := range issued {
		assert.Truef(t, issuedCardPrefix(pan), "%s (%s) must be recognized as issued", name, pan)
	}

	// Ranges the networks do not issue from, which Presidio's leading-group
	// alternation nonetheless accepts.
	for _, pan := range []string{"6651665266536654", "6135802974216083", "6900000000000008"} {
		assert.Falsef(t, issuedCardPrefix(pan), "%s must not be recognized as issued", pan)
	}
}

// TestLuhnValidMatchesPresidio locks the reimplemented mod-10 check to the one
// Presidio's CreditCardRecognizer runs.
func TestLuhnValidMatchesPresidio(t *testing.T) {
	t.Parallel()

	for _, digits := range []string{"4111111111111111", "378282246310005", "30569309025904", "4539172846305125"} {
		assert.Truef(t, luhnValid(digits), "%s should pass", digits)
	}
	for _, digits := range []string{"4111111111111112", "378282246310006", "30569309025905", "", "1234567890123456"} {
		assert.Falsef(t, luhnValid(digits), "%s should fail", digits)
	}
}

// TestPlaceholderCardShapes pins the three pattern families the shape layer
// recognizes, and the boundary cases that must stay out of them.
func TestPlaceholderCardShapes(t *testing.T) {
	t.Parallel()

	shaped := []string{
		"4111111111111111", // two distinct digits
		"4242424242424242", // two distinct digits
		"4009401040114012", // groups counting up
		"4048404740464045", // groups counting down
		"1234567890123452", // one ascending run, wrapping 9->0
	}
	for _, digits := range shaped {
		assert.NotEmptyf(t, placeholderCardShapeReason(digits), "%s should read as a pattern", digits)
	}

	real := []string{
		syntheticVisa,
		"5534129876004319",
		"378282246310005",
		"4009401040114013", // last group breaks the run
	}
	for _, digits := range real {
		assert.Emptyf(t, placeholderCardShapeReason(digits), "%s should not read as a pattern", digits)
	}
}

// TestCreditCardSeparatorsMatchRecognizerGrammar keeps cardDigits aligned with
// the recognizer's replacement_pairs: spaces and hyphens only. A value carrying
// any other punctuation did not come from this recognizer, so the catalog must
// leave it alone rather than guess at its digits.
func TestCreditCardSeparatorsMatchRecognizerGrammar(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "4111111111111111", cardDigits("4111 1111 1111 1111"))
	assert.Equal(t, "4111111111111111", cardDigits("4111-1111-1111-1111"))
	assert.Equal(t, "4111111111111111", cardDigits("  4111 1111-1111 1111  "))
	assert.Empty(t, cardDigits("4111.1111.1111.1111"))
	assert.Empty(t, cardDigits("4111_1111_1111_1111"))
	assert.Empty(t, strings.TrimSpace(cardDigits("not a card")))
}
