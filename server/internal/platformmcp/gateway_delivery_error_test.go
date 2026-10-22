package platformmcp

import (
	"fmt"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/stretchr/testify/require"
)

func TestGatewayDeliveryAdmissionRefusals(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		cause error
		code  string
	}{
		{"approval", admission.ErrApprovalRequired, "conflict"},
		{"private audience", admission.ErrPrivateGatewayAudience, "conflict"},
		{"unavailable", admission.ErrUnavailable, unavailableCode},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var service *GatewayService
			err := service.sanitize(t.Context(), fmt.Errorf("deliver updated gateway: %w", tt.cause))
			var refusal *GatewayError
			require.ErrorAs(t, err, &refusal)
			require.Equal(t, tt.code, refusal.Code)
			require.ErrorIs(t, err, tt.cause)
		})
	}
}
