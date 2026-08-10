package presidiofp

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNonNHSReason covers the value-only layer: what a ten-digit run says about
// itself, before any surrounding text is consulted.
func TestNonNHSReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		match  string
		expect bool // true when the value alone proves it is not an NHS number
	}{
		// Issued ranges, valid check digit: this layer must let them through.
		{name: "england separated", match: "401 023 2137", expect: false},
		{name: "england hyphenated", match: "401-023-2137", expect: false},
		{name: "england bare", match: "4010232137", expect: false},
		{name: "wales range", match: "6543210982", expect: false},
		{name: "scotland chi range", match: "1706349017", expect: false},
		{name: "northern ireland range", match: "3201234567", expect: false},

		// Never issued to anyone.
		{name: "nhs england test range", match: "9999999999", expect: true},
		{name: "unallocated above 859", match: "9434765919", expect: true},
		{name: "zero padded internal id", match: "0000000000", expect: true},

		// Not an NHS number at all.
		{name: "check digit fails", match: "4010232138", expect: true},

		// Out of this catalog's scope: the recognizer only ever emits
		// ten-digit runs, so anything else is left for another lane.
		{name: "too short", match: "40102321", expect: false},
		{name: "too long", match: "40102321370", expect: false},
		{name: "not digits", match: "40102321AB", expect: false},
		{name: "empty", match: "", expect: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reason := nonNHSReason(tt.match)
			if tt.expect {
				assert.NotEmpty(t, reason)
				return
			}
			assert.Empty(t, reason)
		})
	}
}

// TestNHSContextReason covers the layer that reads the payload the match came
// from. Presidio's recognizer ships these context words but only lets them
// raise a score, so this is where they become a requirement.
func TestNHSContextReason(t *testing.T) {
	t.Parallel()

	kept := []string{
		"Patient NHS number 401 023 2137",
		`{"nhsNumber": "4010232137"}`,
		"NHS_NUMBER=4010232137",
		"the national health service record shows 4010232137",
		"CHI number 1706349017 for the Scottish record",
		"hospital number on file, id 4010232137",
	}
	for _, text := range kept {
		assert.Emptyf(t, nhsContextReason(text), "should keep: %q", text)
	}

	suppressed := []string{
		"https://acme.atlassian.net/wiki/spaces/ENG/pages/4010232137/Runbook",
		`{"ts": 1706349017, "level": "info"}`,
		"order 4010232137 shipped",
		"figma node 4010232137",
	}
	for _, text := range suppressed {
		assert.NotEmptyf(t, nhsContextReason(text), "should suppress: %q", text)
	}

	// Unknown context is not evidence of anything.
	assert.Empty(t, nhsContextReason(""))
}

// TestNHSCheckDigitMatchesPresidio locks our reimplementation of the mod-11
// check to the one Presidio's NhsRecognizer runs (weights ten down to one, sum
// divisible by eleven). Vectors are the ones exercised elsewhere in this
// package, verified against the analyzer itself.
func TestNHSCheckDigitMatchesPresidio(t *testing.T) {
	t.Parallel()

	valid := []string{"4010232137", "9434765919", "1706349017", "2481160193", "9999999999"}
	for _, digits := range valid {
		assert.Truef(t, nhsCheckDigitValid(digits), "%s should pass", digits)
	}
	invalid := []string{"4010232138", "0001234567", "6543210989", "1234567890", "3201234561"}
	for _, digits := range invalid {
		assert.Falsef(t, nhsCheckDigitValid(digits), "%s should fail", digits)
	}
}

// TestNHSAllocatedRangesAreOrderedAndDisjoint keeps the range table honest: a
// low above its high, or a pair of overlapping ranges, would mean someone
// mistyped a boundary while editing it.
func TestNHSAllocatedRangesAreOrderedAndDisjoint(t *testing.T) {
	t.Parallel()

	for i, rng := range nhsAllocatedRanges {
		require.LessOrEqualf(t, rng.low, rng.high, "range %d is inverted", i)
		if i == 0 {
			continue
		}
		assert.Greaterf(t, rng.low, nhsAllocatedRanges[i-1].high,
			"range %d overlaps or backtracks on its predecessor", i)
	}
}

// TestNHSSuppressesOpaqueIdentifiers is the regression this catalog exists for
// (AIS-494). Presidio reports any checksum-valid ten-digit run as a UK NHS
// number at maximum confidence, so roughly one in eleven Confluence page ids,
// Unix timestamps and order numbers surfaces as a government/health identifier.
// Every one of them must now be classified as noise.
func TestNHSSuppressesOpaqueIdentifiers(t *testing.T) {
	t.Parallel()

	// Deterministic corpus, seeded so a failure is reproducible.
	rng := rand.New(rand.NewPCG(1, 2))

	var checked int
	for range 20000 {
		id := fmt.Sprintf("%010d", rng.IntN(9_000_000_000)+1_000_000_000)
		if !nhsCheckDigitValid(id) {
			continue // Presidio would not have reported it in the first place.
		}
		checked++
		text := "https://acme.atlassian.net/wiki/spaces/ENG/pages/" + id + "/Runbook"
		assert.NotEmptyf(t, ReasonInContext(EntityTypeUKNHS, id, text),
			"opaque identifier %s must not read as an NHS number", id)
	}
	require.Positive(t, checked, "corpus produced no checksum-valid ids")
}
