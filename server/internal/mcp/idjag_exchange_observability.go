package mcp

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
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
	logger = idJAGExchangeLogger(endpoint, logger)
	return &idJAGExchange{metrics: s.metrics, logger: logger, issuerID: issuerID, outcome: mcpmetrics.IDJAGExchangeError, reason: mcpmetrics.IDJAGRejectionNone}, logger
}

// idJAGExchangeLogger adds the issuer and, for gateway-default exchanges, the resource source.
func idJAGExchangeLogger(endpoint *ResolvedMcpEndpoint, logger *slog.Logger) *slog.Logger {
	logger = logger.With(attr.SlogUserSessionIssuerID(endpoint.UserSessionIssuerID.String()))
	if endpoint.sharedResource != nil && endpoint.sharedResource.organizationGatewayDefault {
		logger = logger.With(attr.SlogOAuthIDJAGResourceSource(idJAGResourceSourceOrganizationGateway))
	}
	return logger
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
	exchange := s.startSharedIDJAGExchange(issuerID, logger)
	attrs := exchange.reject(reason, nil)
	s.recordSharedResourceRejectionAs(ctx, logger, "oauth ID-JAG token request rejected", issuerID, mcpmetrics.OAuthFlowStageToken, rejection, resources, attrs...)
	exchange.record(ctx)
}

// rejectTokenClient logs one refusal of the client a token request presents.
// On the ID-JAG grant that line is the exchange's single rejection line, and
// the exchange is counted under idJAGReason.
func (s *Service) rejectTokenClient(ctx context.Context, logger *slog.Logger, r *http.Request, endpoint *ResolvedMcpEndpoint, grantType, clientID, authMethod, reason string, idJAGReason mcpmetrics.IDJAGRejectionReason) {
	if grantType != oauthwire.GrantTypeJWTBearer {
		logOAuthClientCredentialEvent(ctx, logger, r, "oauth token client authentication rejected", clientID, authMethod, grantType, reason)
		return
	}
	exchange, logger := s.startIDJAGExchange(endpoint, logger)
	logOAuthClientCredentialEvent(ctx, logger, r, "oauth ID-JAG token request rejected", clientID, authMethod, grantType, reason, exchange.reject(idJAGReason, nil)...)
	exchange.record(ctx)
}

// failTokenClient counts an ID-JAG exchange that failed while checking its
// client as a failed exchange under reason. Other grants are not counted.
func (s *Service) failTokenClient(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint, grantType string, reason mcpmetrics.IDJAGRejectionReason, err error) {
	if grantType != oauthwire.GrantTypeJWTBearer {
		return
	}
	issuerID := endpoint.UserSessionIssuerID.String()
	idJAGExchangeLogger(endpoint, logger).InfoContext(ctx, "oauth ID-JAG token request failed", attr.SlogOAuthIDJAGOutcome(string(mcpmetrics.IDJAGExchangeError)), attr.SlogOAuthIDJAGRejectionReason(reason), attr.SlogError(err))
	s.metrics.RecordIDJAGExchange(ctx, issuerID, mcpmetrics.IDJAGExchangeError, reason)
}

// recordSharedIDJAGAssertionRejection records an ID-JAG exchange the shared
// token endpoint refused before the grant handler because the assertion could
// not be read. logger already carries the issuer.
func (s *Service) recordSharedIDJAGAssertionRejection(ctx context.Context, logger *slog.Logger, issuerID uuid.UUID, err error) {
	exchange := s.startSharedIDJAGExchange(issuerID, logger)
	logger.LogAttrs(ctx, slog.LevelInfo, "oauth ID-JAG token request rejected", exchange.rejectAssertion(err)...)
	exchange.record(ctx)
}

// recordSharedIDJAGRequestRejection records an ID-JAG exchange the shared
// token endpoint refused before the grant handler because the request itself
// was malformed. logger already carries the issuer.
func (s *Service) recordSharedIDJAGRequestRejection(ctx context.Context, logger *slog.Logger, r *http.Request, issuerID uuid.UUID, reason string, idJAGReason mcpmetrics.IDJAGRejectionReason) {
	exchange := s.startSharedIDJAGExchange(issuerID, logger)
	creds := extractClientCredentials(r)
	clientID, _ := resolvePresentedClientID(creds)
	logOAuthClientCredentialEvent(ctx, logger, r, "oauth ID-JAG token request rejected", clientID, creds.method, oauthwire.GrantTypeJWTBearer, reason, exchange.reject(idJAGReason, nil)...)
	exchange.record(ctx)
}

// startSharedIDJAGExchange starts an exchange the shared token endpoint
// refuses before it names an MCP server. logger already carries the issuer.
func (s *Service) startSharedIDJAGExchange(issuerID uuid.UUID, logger *slog.Logger) *idJAGExchange {
	return &idJAGExchange{metrics: s.metrics, logger: logger, issuerID: issuerID.String(), outcome: mcpmetrics.IDJAGExchangeError, reason: mcpmetrics.IDJAGRejectionNone}
}
