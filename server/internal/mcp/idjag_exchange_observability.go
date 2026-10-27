package mcp

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/idjag"
)

// ID-JAG rejections decided by the token endpoint rather than the assertion
// validator. Together with idjag.Reason they are the closed set recorded on
// oauth.id_jag.exchange.
const (
	idJAGRejectionInvalidRequest       idjag.Reason = "invalid_request"
	idJAGRejectionResourceMismatch     idjag.Reason = "resource_mismatch"
	idJAGRejectionSubjectNotAuthorized idjag.Reason = "subject_not_authorized"

	// idJAGRejectionUnclassified is a validator error without a typed reason.
	idJAGRejectionUnclassified idjag.Reason = "unclassified"
)

// idJAGRejectionReason is the validator's typed reason for err.
func idJAGRejectionReason(err error) idjag.Reason {
	if reason := idjag.ReasonOf(err); reason != "" {
		return reason
	}
	return idJAGRejectionUnclassified
}

// recordIDJAGRejection logs and counts one rejected ID-JAG exchange. err is
// logged but never returned to the client.
func (s *Service) recordIDJAGRejection(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint, reason idjag.Reason, err error) {
	args := []any{attr.SlogOAuthIDJAGRejectionReason(reason), attr.SlogUserSessionIssuerID(endpoint.UserSessionIssuerID.String())}
	if err != nil {
		args = append(args, attr.SlogError(err))
	}
	logger.InfoContext(ctx, "oauth ID-JAG token request rejected", args...)
	s.metrics.RecordIDJAGExchange(ctx, endpoint.UserSessionIssuerID.String(), mcpmetrics.IDJAGExchangeRejected, string(reason))
}

// recordIDJAGAccepted counts one ID-JAG exchange that minted a session.
func (s *Service) recordIDJAGAccepted(ctx context.Context, endpoint *ResolvedMcpEndpoint) {
	s.metrics.RecordIDJAGExchange(ctx, endpoint.UserSessionIssuerID.String(), mcpmetrics.IDJAGExchangeAccepted, "")
}
