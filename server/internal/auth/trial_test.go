package auth

import (
	"context"
	"errors"
	"testing"

	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestLoadActiveTrialReturnsNilAfterLookupError(t *testing.T) {
	t.Parallel()

	lookupErr := errors.New("trial lookup unavailable")
	logger := testenv.NewLogger(t)

	trial := loadActiveTrial(
		context.Background(),
		"<ORG_ID>",
		func(context.Context, string) (orgRepo.GetActiveTrialRow, error) {
			return orgRepo.GetActiveTrialRow{}, lookupErr
		},
		logger,
	)

	require.Nil(t, trial)
}
