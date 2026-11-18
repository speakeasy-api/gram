package oops_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestInnermost(t *testing.T) {
	t.Parallel()

	require.Nil(t, oops.Innermost(nil))
	require.Nil(t, oops.Innermost(errors.New("plain")))

	inner := oops.E(oops.CodeInvalid, errors.New("dial: no url"), "no server URL provided for tool")
	require.Same(t, inner, oops.Innermost(inner))

	outer := oops.E(oops.CodeBadRequest, fmt.Errorf("wrapped: %w", inner), "tool call was rejected as invalid or not permitted")
	require.Same(t, inner, oops.Innermost(outer))
	require.Same(t, inner, oops.Innermost(fmt.Errorf("context: %w", outer)))

	joined := oops.E(oops.CodeUnexpected, errors.Join(errors.New("plain"), outer), "tool call failed")
	require.Same(t, inner, oops.Innermost(joined), "a ShareableError under errors.Join is found")
	require.Same(t, inner, oops.Innermost(errors.Join(errors.New("plain"), outer)))
}

// synthesizingError has an As hook that answers every *ShareableError target
// with a new ShareableError wrapping itself, so following causes never ends.
type synthesizingError struct{}

func (synthesizingError) Error() string { return "synthesizing" }

func (c synthesizingError) As(target any) bool {
	shareable, ok := target.(**oops.ShareableError)
	if !ok {
		return false
	}
	*shareable = oops.E(oops.CodeUnexpected, c, "synthesized")
	return true
}

// cyclicError answers every *ShareableError target with the same error.
type cyclicError struct{ self **oops.ShareableError }

func (cyclicError) Error() string { return "cyclic" }

func (c cyclicError) As(target any) bool {
	shareable, ok := target.(**oops.ShareableError)
	if !ok {
		return false
	}
	*shareable = *c.self
	return true
}

// A cause whose As hook keeps producing ShareableErrors ends the walk instead
// of looping forever.
func TestInnermost_StopsOnACauseThatNeverEnds(t *testing.T) {
	t.Parallel()

	require.NotNil(t, oops.Innermost(synthesizingError{}))

	var self *oops.ShareableError
	self = oops.E(oops.CodeUnexpected, cyclicError{self: &self}, "cyclic")
	require.Same(t, self, oops.Innermost(self))
}
