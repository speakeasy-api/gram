package presidiofp

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realLookingCard is a Luhn-valid Visa number that is not a published test
// card and has no placeholder shape, so it stands in for a real customer card.
const realLookingCard = "4539578763621486"

func TestNonCardReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		match  string
		expect bool
	}{
		{name: "real-looking visa", match: realLookingCard, expect: false},
		{name: "real-looking visa spaced", match: "4539 5787 6362 1486", expect: false},
		{name: "real-looking visa hyphenated", match: "4539-5787-6362-1486", expect: false},
		{name: "real-looking mastercard", match: "5534129876004319", expect: false},
		{name: "real-looking amex", match: "371882450931763", expect: false},

		{name: "visa test card", match: "4111111111111111", expect: true},
		{name: "visa test card spaced", match: "4111 1111 1111 1111", expect: true},
		{name: "stripe visa", match: "4242-4242-4242-4242", expect: true},
		{name: "widely circulated example visa", match: "4539 1488 0343 6467", expect: true},
		{name: "mastercard test card", match: "5555555555554444", expect: true},
		{name: "amex test card", match: "378282246310005", expect: true},
		{name: "discover test card", match: "6011111111111117", expect: true},
		{name: "diners test card", match: "30569309025904", expect: true},
		{name: "jcb test card", match: "3530111333300000", expect: true},
		{name: "unionpay test card", match: "6243030000000001", expect: true},

		{name: "two distinct digits", match: "4444 5555 5555 5555", expect: true},
		{name: "ascending digits", match: "1234567890123452", expect: true},
		{name: "descending digits", match: "6543210987654326", expect: true},
		{name: "consecutive ids", match: "6651 6652 6653 6654", expect: true},
		{name: "descending ids with gaps", match: "6789 6786 6782 6777", expect: true},

		// Out of scope: the recognizer never emits these shapes.
		{name: "too short", match: "453957876362", expect: false},
		{name: "too long", match: "45395787636214860000", expect: false},
		{name: "not digits", match: "4111.1111.1111.1111", expect: false},
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

func TestCardIDListing(t *testing.T) {
	t.Parallel()

	assert.True(t, cardIDListing("6651665266536654"), "consecutive ascending")
	assert.True(t, cardIDListing("6789678667826777"), "descending with gaps")
	assert.True(t, cardIDListing("1000100110501100"), "span exactly at the limit")

	assert.False(t, cardIDListing("1000100110501101"), "span one past the limit")
	assert.False(t, cardIDListing("6784678167776683"), "groups too far apart")
	assert.False(t, cardIDListing("6651665366526654"), "not monotonic")
	assert.False(t, cardIDListing("6651665166526653"), "repeated group")
	assert.False(t, cardIDListing("665166526653665"), "not sixteen digits")
}

// TestCreditCardRegressions replays the traffic shapes that made a financial
// data policy warn on coding-agent sessions (AIS-720).
func TestCreditCardRegressions(t *testing.T) {
	t.Parallel()

	// A fixture-bearing file re-shipped by a coding agent's PostToolUse hook.
	// It talks about cards, so only the value can clear it.
	fixture := `f.match = "4539 1488 0343 6467"
if i%4 == 0 {
	f.match = "4111 1111 1111 1111"
}
f.content = "Customer card " + f.match + ", exp 11/27."`
	for _, pan := range []string{"4539 1488 0343 6467", "4111 1111 1111 1111"} {
		assert.NotEmptyf(t, ReasonInContext(EntityTypeCreditCard, pan, fixture), "fixture card %q must not flag", pan)
	}

	// A Bash tool request listing four consecutive PR numbers.
	assert.NotEmpty(t, ReasonInContext(EntityTypeCreditCard, "6651 6652 6653 6654",
		"gh pr view 6651 6652 6653 6654 --json title"))

	// A real-looking card with card context still flags, in a user message and
	// in a tool response.
	assert.Empty(t, ReasonInContext(EntityTypeCreditCard, realLookingCard,
		"Please refund my order, my credit card is "+realLookingCard))
	assert.Empty(t, ReasonInContext(EntityTypeCreditCard, realLookingCard,
		`{"customer": {"card": {"number": "`+realLookingCard+`", "exp": "11/27"}}}`))
}

// TestCreditCardKeepsRandomCards guards the expensive direction: the value
// checks must not drop plausible card numbers. Every Luhn-valid run in a
// deterministic corpus must survive.
func TestCreditCardKeepsRandomCards(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(720, 0))
	prefixes := []int{3, 4, 5, 6}

	checked := 0
	for checked < 200_000 {
		pan := fmt.Sprintf("%d%015d", prefixes[rng.IntN(len(prefixes))], rng.Int64N(1_000_000_000_000_000))
		if !luhnValid(pan) {
			continue
		}
		checked++
		if knownTestPANs[pan] {
			continue
		}
		require.Emptyf(t, nonCardReason(pan), "plausible card %s must not be suppressed", pan)
	}
}

// TestKnownTestPANsAreWellFormed keeps every catalog entry bare digits,
// Luhn-valid and inside the PAN length window, so a mistyped digit fails here
// instead of silently never matching.
func TestKnownTestPANsAreWellFormed(t *testing.T) {
	t.Parallel()

	require.NotEmpty(t, knownTestPANs)
	for pan := range knownTestPANs {
		assert.Equalf(t, pan, cardDigits(pan), "%s must be stored as bare digits", pan)
		assert.GreaterOrEqualf(t, len(pan), minCardDigits, "%s is too short", pan)
		assert.LessOrEqualf(t, len(pan), maxCardDigits, "%s is too long", pan)
		assert.Truef(t, luhnValid(pan), "%s must pass the Luhn checksum", pan)
	}
}

// luhnValid runs the mod-10 check Presidio's CreditCardRecognizer applies
// before it reports a match.
func luhnValid(digits string) bool {
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
	return digits != "" && sum%10 == 0
}
