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
// counted and logged without marking each return. Accepted and rejected
// exchanges are logged by the caller with the attributes returned here.
type idJAGExchange struct {
	metrics  *mcpmetrics.Metrics
	logger   *slog.Logger
	issuerID string
	outcome  mcpmetrics.IDJAGExchangeOutcome
	reason   mcpmetrics.IDJAGRejectionReason
}

// startIDJAGExchange returns the exchange and logger, which carries the
// issuer, that every line about the exchange is written with.
func (s *Service) startIDJAGExchange(endpoint *ResolvedMcpEndpoint, logger *slog.Logger) (*idJAGExchange, *slog.Logger) {
	issuerID := endpoint.UserSessionIssuerID.String()
	logger = logger.With(attr.SlogUserSessionIssuerID(issuerID))
	return &idJAGExchange{metrics: s.metrics, logger: logger, issuerID: issuerID, outcome: mcpmetrics.IDJAGExchangeError, reason: mcpmetrics.IDJAGRejectionNone}, logger
}

// reject marks the exchange rejected and returns the attributes its
// rejection log line carries. err is logged, never returned to the client.
func (e *idJAGExchange) reject(reason mcpmetrics.IDJAGRejectionReason, err error) []slog.Attr {
	e.outcome, e.reason = mcpmetrics.IDJAGExchangeRejected, reason
	attrs := []slog.Attr{attr.SlogOAuthIDJAGOutcome(string(mcpmetrics.IDJAGExchangeRejected)), attr.SlogOAuthIDJAGRejectionReason(reason)}
	if err != nil {
		attrs = append(attrs, attr.SlogError(err))
	}
	return attrs
}

// idJAGInfrastructureReason reports a validator refusal caused by a
// Speakeasy or identity provider failure rather than the assertion.
func idJAGInfrastructureReason(reason idjag.Reason) bool {
	switch reason {
	case idjag.ReasonTrustUnavailable, idjag.ReasonKeyUnresolvable, idjag.ReasonSubjectUnavailable, idjag.ReasonReplayStoreUnavailable, idjag.ReasonVerifierMisconfigured:
		return true
	default:
		return false
	}
}

// rejectAssertion is reject for a validator refusal. An infrastructure
// reason keeps its reason label but records outcome=error.
func (e *idJAGExchange) rejectAssertion(err error) []slog.Attr {
	reason := idjag.ReasonOf(err)
	attrs := e.reject(mcpmetrics.IDJAGRejectionFromValidator(reason), err)
	if idJAGInfrastructureReason(reason) {
		e.outcome = mcpmetrics.IDJAGExchangeError
		attrs[0] = attr.SlogOAuthIDJAGOutcome(string(mcpmetrics.IDJAGExchangeError))
	}
	return attrs
}

// acceptedAttrs are the attributes the accepted exchange's log line carries.
func (e *idJAGExchange) acceptedAttrs() []slog.Attr {
	return []slog.Attr{attr.SlogOAuthIDJAGOutcome(string(mcpmetrics.IDJAGExchangeAccepted))}
}

func (e *idJAGExchange) accept() {
	e.outcome, e.reason = mcpmetrics.IDJAGExchangeAccepted, mcpmetrics.IDJAGRejectionNone
}

func (e *idJAGExchange) record(ctx context.Context) {
	if e.outcome == mcpmetrics.IDJAGExchangeError && e.reason == mcpmetrics.IDJAGRejectionNone {
		e.logger.InfoContext(ctx, "oauth ID-JAG token request failed", attr.SlogOAuthIDJAGOutcome(string(mcpmetrics.IDJAGExchangeError)))
	}
	e.metrics.RecordIDJAGExchange(ctx, e.issuerID, e.outcome, e.reason)
}

// recordSharedIDJAGResourceMissing records an ID-JAG exchange the shared
// token endpoint refused before the grant handler because the request named
// no resource: one rejection log line, counted as an ID-JAG exchange. logger
// already carries the issuer.
func (s *Service) recordSharedIDJAGResourceMissing(ctx context.Context, logger *slog.Logger, issuerID uuid.UUID, resources []string) {
	exchange := &idJAGExchange{metrics: s.metrics, logger: logger, issuerID: issuerID.String(), outcome: mcpmetrics.IDJAGExchangeError, reason: mcpmetrics.IDJAGRejectionNone}
	attrs := exchange.reject(mcpmetrics.IDJAGRejectionFromValidator(idjag.ReasonResourceNotIndicated), nil)
	s.recordSharedResourceRejectionAs(ctx, logger, "oauth ID-JAG token request rejected", issuerID, mcpmetrics.OAuthFlowStageToken, sharedResourceMissing, resources, attrs...)
	exchange.record(ctx)
}
