package mcpendpoints

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
)

func TestGatewayDeliveryError(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		cause error
		code  oops.Code
	}{
		{"approval", admission.ErrApprovalRequired, oops.CodeConflict},
		{"private Everyone", admission.ErrPrivateGatewayAudience, oops.CodeConflict},
		{"unavailable", admission.ErrUnavailable, oops.CodeUnavailable},
		{"unexpected", errors.New("database failure"), oops.CodeUnexpected},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := gatewayDeliveryError(fmt.Errorf("role delivery: %w", tt.cause))
			require.Equal(t, tt.code, err.Code)
			require.ErrorIs(t, err, tt.cause)
		})
	}
}
