package remotesessions

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// GatewayMemberCredentialsEnabled reports whether the organization may bind
// several clients of one remote issuer to a gateway's issuer, one per member.
// Disabled, indeterminate and failed evaluations all read as off.
func GatewayMemberCredentialsEnabled(ctx context.Context, logger *slog.Logger, features feature.Provider, organizationID, organizationSlug string) bool {
	if features == nil || organizationID == "" {
		return false
	}
	evaluation, err := feature.EvaluateFlag(ctx, features, feature.FlagGatewayMemberCredentials, organizationID, feature.OrgProjectGroups(organizationSlug, ""))
	if err != nil {
		logger.WarnContext(ctx, "evaluate gateway-member-credentials flag", attr.SlogError(err))
		return false
	}
	return evaluation == feature.EvaluationEnabled
}

// GatewayOwnsIssuerExclusively reports whether exactly one live gateway, and
// no server, toolset or Platform MCP registration, consumes the user session
// issuer. Only such an issuer may hold more than one client of a remote
// issuer. The caller must hold the issuer's owner-binding lock so a consumer
// cannot be added between this check and the binding write.
func GatewayOwnsIssuerExclusively(ctx context.Context, q *repo.Queries, userSessionIssuerID uuid.UUID) (bool, error) {
	ownership, err := q.GetUserSessionIssuerGatewayOwnership(ctx, userSessionIssuerID)
	if err != nil {
		return false, fmt.Errorf("get user session issuer gateway ownership: %w", err)
	}
	return ownership.Gateways == 1 && ownership.OtherConsumers == 0, nil
}
