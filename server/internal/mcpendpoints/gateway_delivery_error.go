package mcpendpoints

import (
	"errors"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
)

func gatewayDeliveryError(err error) *oops.ShareableError {
	switch {
	case errors.Is(err, admission.ErrApprovalRequired):
		return oops.E(oops.CodeConflict, err, "gateway audience requires Shadow MCP approval")
	case errors.Is(err, admission.ErrPrivateGatewayAudience):
		return oops.E(oops.CodeConflict, err, "private-only gateway cannot be distributed to Everyone")
	case errors.Is(err, admission.ErrUnavailable):
		return oops.E(oops.CodeUnavailable, err, "distribution approval could not be verified safely")
	default:
		return oops.E(oops.CodeUnexpected, err, "deliver eligible gateway")
	}
}
