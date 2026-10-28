package mcp

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/idjag"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
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

// rejectAssertion is reject for a validator refusal.
func (e *idJAGExchange) rejectAssertion(err error) []slog.Attr {
	return e.reject(mcpmetrics.IDJAGRejectionFromValidator(idjag.ReasonOf(err)), err)
}

// acceptedAttrs are the attributes the accepted exchange's log line carries.
func (e *idJAGExchange) acceptedAttrs() []slog.Attr {
	return []slog.Attr{attr.SlogOAuthIDJAGOutcome(string(mcpmetrics.IDJAGExchangeAccepted))}
}

func (e *idJAGExchange) accept() {
	e.outcome, e.reason = mcpmetrics.IDJAGExchangeAccepted, mcpmetrics.IDJAGRejectionNone
}

func (e *idJAGExchange) record(ctx context.Context) {
	if e.outcome == mcpmetrics.IDJAGExchangeError {
		e.logger.InfoContext(ctx, "oauth ID-JAG token request failed", attr.SlogOAuthIDJAGOutcome(string(mcpmetrics.IDJAGExchangeError)))
	}
	e.metrics.RecordIDJAGExchange(ctx, e.issuerID, e.outcome, e.reason)
}

// recordSharedIDJAGResourceRejection records an ID-JAG exchange the shared
// token endpoint refused before the grant handler because of its resource:
// one rejection log line, counted as an ID-JAG exchange under reason. logger
// already carries the issuer.
func (s *Service) recordSharedIDJAGResourceRejection(ctx context.Context, logger *slog.Logger, issuerID uuid.UUID, rejection sharedResourceRejection, resources []string, reason mcpmetrics.IDJAGRejectionReason) {
	exchange := &idJAGExchange{metrics: s.metrics, logger: logger, issuerID: issuerID.String(), outcome: mcpmetrics.IDJAGExchangeError, reason: mcpmetrics.IDJAGRejectionNone}
	attrs := exchange.reject(reason, nil)
	s.recordSharedResourceRejectionAs(ctx, logger, "oauth ID-JAG token request rejected", issuerID, mcpmetrics.OAuthFlowStageToken, rejection, resources, attrs...)
	exchange.record(ctx)
}

// refuseIDJAGClient refuses a client that may not exchange an ID-JAG, with one
// rejection line and count. exchange is nil when the grant handler has not
// started one.
func (s *Service) refuseIDJAGClient(ctx context.Context, w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, logger *slog.Logger, exchange *idJAGExchange, clientID, authMethod string) error {
	if exchange == nil {
		exchange, logger = s.startIDJAGExchange(endpoint, logger)
		defer exchange.record(ctx)
	}
	reason := mcpmetrics.IDJAGRejectionClientNotAllowed
	logOAuthClientCredentialEvent(ctx, logger, r, "oauth ID-JAG token request rejected", clientID, authMethod, oauthwire.GrantTypeJWTBearer, string(reason), exchange.reject(reason, nil)...)
	description := "this client is not allowed to use the ID-JAG grant"
	// Pattern documents are deliberately not accepted; their app has a current stable document.
	if preset, ok := admission.CatalogPreset(clientID); ok {
		if product, ok := aivendors.IDJAGProductForDocument(preset.URL); ok {
			description += "; use a current version of " + product.DisplayName
		}
	}
	return writeTokenPolicyError(ctx, w, logger, oauthwire.GrantTypeJWTBearer, description)
}
