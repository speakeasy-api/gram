package idjag_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/idjag"
)

func TestUnverifiedResourceRejectsMalformedAssertion(t *testing.T) {
	t.Parallel()

	_, ok, err := idjag.UnverifiedResource("not.a.jwt")
	require.False(t, ok)
	require.Equal(t, idjag.ReasonMalformed, idjag.ReasonOf(err))
}
