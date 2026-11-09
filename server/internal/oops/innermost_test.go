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
