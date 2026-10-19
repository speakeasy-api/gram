package agent

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOptionalOrganizationGatewayURLFailsOpen(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	called := false
	got := optionalOrganizationGatewayURL(context.Background(), logger, func() (*string, error) {
		called = true
		return nil, errors.New("gateway lookup failed")
	})

	require.True(t, called)
	require.Nil(t, got)
}
