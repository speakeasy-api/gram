package mcp

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/idjag"
)

// idJAGExchange records one ID-JAG exchange on oauth.id_jag.exchange when the
// grant handler returns. It starts as an error, so every server failure is
// counted without marking each return.
type idJAGExchange struct {
	metrics  *mcpmetrics.Metrics
	issuerID string
	outcome  mcpmetrics.IDJAGExchangeOutcome
	reason   mcpmetrics.IDJAGRejectionReason
}

func (s *Service) startIDJAGExchange(endpoint *ResolvedMcpEndpoint) *idJAGExchange {
	return &idJAGExchange{metrics: s.metrics, issuerID: endpoint.UserSessionIssuerID.String(), outcome: mcpmetrics.IDJAGExchangeError, reason: mcpmetrics.IDJAGRejectionNone}
}

// reject marks the exchange rejected and returns the attributes its
// rejection log line carries. err is logged, never returned to the client.
func (e *idJAGExchange) reject(reason mcpmetrics.IDJAGRejectionReason, err error) []slog.Attr {
	e.outcome, e.reason = mcpmetrics.IDJAGExchangeRejected, reason
	attrs := []slog.Attr{attr.SlogOAuthIDJAGOutcome(string(mcpmetrics.IDJAGExchangeRejected)), attr.SlogOAuthIDJAGRejectionReason(reason), attr.SlogUserSessionIssuerID(e.issuerID)}
	if err != nil {
		attrs = append(attrs, attr.SlogError(err))
	}
	return attrs
}

// rejectAssertion is reject for a validator refusal.
func (e *idJAGExchange) rejectAssertion(err error) []slog.Attr {
	return e.reject(mcpmetrics.IDJAGRejectionFromValidator(idjag.ReasonOf(err)), err)
}

// acceptedAttrs are the attributes the accepted exchange's log line carries.
func (e *idJAGExchange) acceptedAttrs() []slog.Attr {
	return []slog.Attr{attr.SlogOAuthIDJAGOutcome(string(mcpmetrics.IDJAGExchangeAccepted)), attr.SlogUserSessionIssuerID(e.issuerID)}
}

func (e *idJAGExchange) accept() {
	e.outcome, e.reason = mcpmetrics.IDJAGExchangeAccepted, mcpmetrics.IDJAGRejectionNone
}

func (e *idJAGExchange) record(ctx context.Context) {
	e.metrics.RecordIDJAGExchange(ctx, e.issuerID, e.outcome, e.reason)
}

// recordSharedIDJAGResourceMissing records an ID-JAG exchange the shared
// token endpoint refused before the grant handler because the request named
// no resource: one rejection log line, counted as an ID-JAG exchange.
func (s *Service) recordSharedIDJAGResourceMissing(ctx context.Context, logger *slog.Logger, issuerID uuid.UUID, resources []string) {
	exchange := &idJAGExchange{metrics: s.metrics, issuerID: issuerID.String(), outcome: mcpmetrics.IDJAGExchangeError, reason: mcpmetrics.IDJAGRejectionNone}
	attrs := exchange.reject(mcpmetrics.IDJAGRejectionFromValidator(idjag.ReasonResourceNotIndicated), nil)
	s.recordSharedResourceRejectionAs(ctx, logger, "oauth ID-JAG token request rejected", issuerID, mcpmetrics.OAuthFlowStageToken, sharedResourceMissing, resources, attrs...)
	exchange.record(ctx)
}
