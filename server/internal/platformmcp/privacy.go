package platformmcp

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// SubjectSuppressionThreshold is the smallest subject count a diagnostic will
// state exactly. Below it the count itself identifies: "one active user in this
// project last hour" names a person to anyone who knows the team.
const SubjectSuppressionThreshold = 5

// subjectSuppressedLabel is what a suppressed count reports instead of a
// number. It is a stable token, not prose, so a caller can branch on it.
const subjectSuppressedLabel = "less_than_5"

// SubjectCount is a count of people — users, members, operators. It serializes
// as a number when it is zero or at least SubjectSuppressionThreshold, and as
// "less_than_5" in between.
//
// Zero is reported exactly because it identifies nobody: it is the difference
// between "no one used this" and "someone did", which a diagnostic caller needs
// and which reveals no subject. Counts of events — calls, failures — are not
// subject counts and are reported exactly; they are plain integers, not this
// type.
type SubjectCount struct {
	value int64
}

// NewSubjectCount wraps a raw subject count for suppression at the boundary.
func NewSubjectCount(value int64) SubjectCount {
	if value < 0 {
		value = 0
	}
	return SubjectCount{value: value}
}

// Suppressed reports whether this count will be withheld.
func (c SubjectCount) Suppressed() bool {
	return c.value > 0 && c.value < SubjectSuppressionThreshold
}

func (c SubjectCount) MarshalJSON() ([]byte, error) {
	if c.Suppressed() {
		return []byte(strconv.Quote(subjectSuppressedLabel)), nil
	}
	return []byte(strconv.FormatInt(c.value, 10)), nil
}

// UnmarshalJSON exists so a result round-trips through JSON in tests and in any
// caller that decodes its own output. A suppressed value decodes back to the
// suppression floor rather than to a real count, because the real count was
// never transmitted.
func (c *SubjectCount) UnmarshalJSON(data []byte) error {
	var number int64
	if err := json.Unmarshal(data, &number); err == nil {
		c.value = number
		return nil
	}
	var label string
	if err := json.Unmarshal(data, &label); err != nil {
		return fmt.Errorf("decode subject count: %w", err)
	}
	if label != subjectSuppressedLabel {
		c.value = 0
		return nil
	}
	c.value = SubjectSuppressionThreshold - 1
	return nil
}
