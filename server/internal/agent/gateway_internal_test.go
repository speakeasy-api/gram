package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestOptionalOrganizationGatewayURLFailsOpen(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	called := false
	got := optionalOrganizationGatewayURL(context.Background(), logger, func() (*string, error) {
		called = true
		return nil, errors.New("gateway lookup failed")
	})

	require.True(t, called)
	require.Nil(t, got)
}
