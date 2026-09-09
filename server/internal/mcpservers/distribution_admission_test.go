package mcpservers

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestCheckDistributionAdmission_NilService(t *testing.T) {
	t.Parallel()
	var service *Service
	err := service.checkDistributionAdmission(t.Context(), nil, admission.RolloutConfig{}, nil, "", uuid.Nil, uuid.Nil, "", false)
	require.ErrorIs(t, err, admission.ErrUnavailable)
	var fault *oops.ShareableError
	require.ErrorAs(t, err, &fault)
	require.Equal(t, oops.CodeUnexpected, fault.Code)
}

func TestCheckDistributionAdmission_MissingGuard(t *testing.T) {
	t.Parallel()
	service := new(Service)
	service.logger = testenv.NewLogger(t)
	err := service.checkDistributionAdmission(t.Context(), nil, admission.RolloutConfig{}, nil, "", uuid.Nil, uuid.Nil, "", false)
	require.ErrorIs(t, err, admission.ErrUnavailable)
	var fault *oops.ShareableError
	require.ErrorAs(t, err, &fault)
	require.Equal(t, oops.CodeUnexpected, fault.Code)
}
