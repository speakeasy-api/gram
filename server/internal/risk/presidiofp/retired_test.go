package presidiofp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRetiredRecognizers covers the blanket classification: every finding from
// a retired recognizer is noise regardless of its value, which is what lets the
// offline sweep clear the rows stored before the live scanners started dropping
// them.
func TestRetiredRecognizers(t *testing.T) {
	t.Parallel()

	// The shapes AIS-494 reported: Figma file and node ids read as a driver
	// license number to the upstream recognizer.
	for _, match := range []string{"N1234567", "K9182736450", "X12345678", ""} {
		assert.NotEmptyf(t, Reason(EntityTypeUSDriverLicense, match),
			"every US_DRIVER_LICENSE finding is retired noise, including %q", match)
	}

	// Retirement is keyed on the entity, not the value: the same string under a
	// live recognizer is judged on its merits.
	assert.Empty(t, Reason(EntityTypeUSDriverLicense+"_OTHER", "N1234567"))
	assert.Equal(t, []EntityType{EntityTypeUSDriverLicense}, RetiredEntityTypes())
}
