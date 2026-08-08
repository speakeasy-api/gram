package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/stretchr/testify/require"
)

func TestLoadTrialReturnsNilAfterLookupError(t *testing.T) {
	t.Parallel()

	lookupErr := errors.New("trial lookup unavailable")
	logger := testenv.NewLogger(t)

	trial := loadTrial(
		t.Context(),
		"<ORG_ID>",
		func(context.Context, string) (trialsRepo.GetSessionTrialRow, error) {
			return trialsRepo.GetSessionTrialRow{}, lookupErr
		},
		logger,
	)

	require.Nil(t, trial)
}
