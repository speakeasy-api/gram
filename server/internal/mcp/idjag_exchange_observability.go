package mcp

import (
	"context"
	"log/slog"

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
	attrs := []slog.Attr{attr.SlogOAuthIDJAGRejectionReason(reason), attr.SlogUserSessionIssuerID(e.issuerID)}
	if err != nil {
		attrs = append(attrs, attr.SlogError(err))
	}
	return attrs
}

// rejectAssertion is reject for a validator refusal.
func (e *idJAGExchange) rejectAssertion(err error) []slog.Attr {
	return e.reject(mcpmetrics.IDJAGRejectionFromValidator(idjag.ReasonOf(err)), err)
}

func (e *idJAGExchange) accept() {
	e.outcome, e.reason = mcpmetrics.IDJAGExchangeAccepted, mcpmetrics.IDJAGRejectionNone
}

func (e *idJAGExchange) record(ctx context.Context) {
	e.metrics.RecordIDJAGExchange(ctx, e.issuerID, e.outcome, e.reason)
}
