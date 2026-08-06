package auth

import (
	"context"
	"errors"
	"testing"

	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestLoadActiveEnterpriseTrialReturnsNilAfterLookupError(t *testing.T) {
	t.Parallel()

	lookupErr := errors.New("enterprise trial lookup unavailable")
	logger := testenv.NewLogger(t)

	trial := loadActiveEnterpriseTrial(
		context.Background(),
		"<ORG_ID>",
		func(context.Context, string) (orgRepo.GetActiveEnterpriseTrialRow, error) {
			return orgRepo.GetActiveEnterpriseTrialRow{}, lookupErr
		},
		logger,
	)

	require.Nil(t, trial)
}
