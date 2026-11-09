package mcp

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// An MCP server's upstream issuer (the remote session issuer its credentials
// are routed and consented by) comes from one of two sources: the
// denormalized mcp_servers.remote_session_issuer_id column, or the one
// provider among the server's live client associations.
// feature.FlagUpstreamIssuerFromAssociations picks the authoritative one per
// organization. Every site computes both and records how they compare, so
// the cutover is measured before it changes anything and stays measured as
// a rollback. Everything here goes away with the column.

// Surfaces an upstream issuer comparison runs on.
const (
	upstreamIssuerSurfaceDirect         = "direct"
	upstreamIssuerSurfaceGatewayProxied = "gateway_proxied"
	upstreamIssuerSurfaceGatewayHosted  = "gateway_hosted"
	upstreamIssuerSurfaceGatewayState   = "gateway_state"
	upstreamIssuerSurfaceConsent        = "consent"
)

// upstreamIssuerOutcome is how a server's two upstream issuer sources compared
// at one site.
type upstreamIssuerOutcome string

const (
	// upstreamIssuerMatch is the same result from both sources.
	upstreamIssuerMatch upstreamIssuerOutcome = "match"

	// upstreamIssuerDiffer is a different result, for a server whose
	// associations name exactly one provider.
	upstreamIssuerDiffer upstreamIssuerOutcome = "differ"

	// upstreamIssuerNoAssociation is a different result for a server with no
	// live association, where the association source has no issuer at all.
	upstreamIssuerNoAssociation upstreamIssuerOutcome = "no_association"

	// upstreamIssuerSeveralProviders is a different result for a server whose
	// associations name several providers, so the association source has no
	// single issuer.
	upstreamIssuerSeveralProviders upstreamIssuerOutcome = "several_providers"

	// upstreamIssuerChoice is a credential's own client choice governing, so
	// neither source decided.
	upstreamIssuerChoice upstreamIssuerOutcome = "choice"

	// upstreamIssuerNoBoundClients is a direct request whose user session
	// issuer binds no client, so the association source was never read.
	upstreamIssuerNoBoundClients upstreamIssuerOutcome = "no_bound_clients"

	// upstreamIssuerAssociationUnread is a request that resolved its tokens
	// without reading the server's associations, such as through a typed auth
	// client, so only the column could decide.
	upstreamIssuerAssociationUnread upstreamIssuerOutcome = "association_unread"

	// upstreamIssuerError is a comparison that could not be computed.
	upstreamIssuerError upstreamIssuerOutcome = "error"
)

// Sites an upstream issuer comparison runs at.
const (
	// upstreamIssuerSiteRoute routes a direct request's credential.
	upstreamIssuerSiteRoute = "route"

	// upstreamIssuerSiteConsentPreview routes the consent page's tool preview.
	upstreamIssuerSiteConsentPreview = "consent_preview"
)

// Authoritative sources, as recorded.
const (
	upstreamIssuerSourceColumn       = "column"
	upstreamIssuerSourceAssociations = "associations"
)

// upstreamIssuerReconsentOutcome is what one consent check would do if the
// other upstream issuer source were authoritative.
type upstreamIssuerReconsentOutcome string

const (
	// upstreamIssuerReconsentUnchanged is a check both sources decide alike.
	upstreamIssuerReconsentUnchanged upstreamIssuerReconsentOutcome = "unchanged"

	// upstreamIssuerReconsentForced is stored evidence that matches the
	// authoritative source but not the other: switching forces a new consent.
	upstreamIssuerReconsentForced upstreamIssuerReconsentOutcome = "would_reconsent"

	// upstreamIssuerReconsentAlreadyInvalid is stored evidence the
	// authoritative source already rejects, so switching costs nothing extra.
	upstreamIssuerReconsentAlreadyInvalid upstreamIssuerReconsentOutcome = "already_invalid"

	// upstreamIssuerReconsentError is a check whose other hash could not be
	// computed.
	upstreamIssuerReconsentError upstreamIssuerReconsentOutcome = "error"
)

// discardLogger routes the non-authoritative source silently: routing logs
// every fail-closed outcome, and only the authoritative one happened.
var discardLogger = slog.New(slog.DiscardHandler)

// associationUpstreamIssuer is the upstream issuer a server's live client
// associations give: their one provider, or none when they name several
// providers or none.
type associationUpstreamIssuer struct {
	// Provider is the associations' one provider, unset unless Providers is 1.
	Provider uuid.NullUUID

	// Providers is how many distinct live providers the associations name.
	Providers int64
}

// upstreamIssuerKeys are a server's two upstream issuer sources.
type upstreamIssuerKeys struct {
	// column is mcp_servers.remote_session_issuer_id.
	column uuid.NullUUID

	// association is what the server's client associations give.
	association associationUpstreamIssuer

	// known reports that association was read. The direct path reads it with
	// the issuer's bound clients, so it is unknown when the issuer binds none.
	known bool
}

// authoritative is the source that decides, given the organization's flag.
func (k upstreamIssuerKeys) authoritative(fromAssociations bool) uuid.NullUUID {
	if fromAssociations && k.known {
		return k.association.Provider
	}
	return k.column
}

// other is the source that does not decide.
func (k upstreamIssuerKeys) other(fromAssociations bool) uuid.NullUUID {
	if fromAssociations && k.known {
		return k.column
	}
	return k.association.Provider
}

// outcome classifies a comparison whose two results agreed or not.
func (k upstreamIssuerKeys) outcome(agreed bool) upstreamIssuerOutcome {
	switch {
	case !k.known:
		return upstreamIssuerNoBoundClients
	case agreed:
		return upstreamIssuerMatch
	case k.association.Providers == 0:
		return upstreamIssuerNoAssociation
	case k.association.Providers > 1:
		return upstreamIssuerSeveralProviders
	default:
		return upstreamIssuerDiffer
	}
}

// upstreamIssuerSource names the authoritative source for the record.
func upstreamIssuerSource(fromAssociations bool, keys upstreamIssuerKeys) string {
	if fromAssociations && keys.known {
		return upstreamIssuerSourceAssociations
	}
	return upstreamIssuerSourceColumn
}

// resolverModeLabel is the recorded name of an organization's typed auth
// client resolver mode.
func resolverModeLabel(mode feature.AuthClientResolverMode) string {
	switch mode {
	case feature.AuthClientResolverEnforced:
		return "enforced"
	case feature.AuthClientResolverShadow:
		return "shadow"
	default:
		return "off"
	}
}

// sameRoutedUpstream reports whether two routings of one token map reached
// the same result: the same credential, or no credential, or a fail-closed
// refusal for the same reason.
func sameRoutedUpstream(a remotesessions.UpstreamToken, aErr error, b remotesessions.UpstreamToken, bErr error) bool {
	if (aErr == nil) != (bErr == nil) {
		return false
	}
	if aErr != nil {
		var aRouting, bRouting *upstreamRoutingError
		if errors.As(aErr, &aRouting) && errors.As(bErr, &bRouting) {
			return aRouting.reason == bRouting.reason
		}
		return errors.Is(aErr, remotesessions.ErrNoValidToken) == errors.Is(bErr, remotesessions.ErrNoValidToken)
	}
	return a.RemoteSessionClientID == b.RemoteSessionClientID && a.RemoteSessionID == b.RemoteSessionID && a.Token == b.Token
}

// upstreamIssuerParity is one comparison to record.
type upstreamIssuerParity struct {
	// organizationID is the server's organization.
	organizationID string

	// serverID is the MCP server compared.
	serverID uuid.UUID

	// surface is the serving surface the comparison ran on.
	surface string

	// site is the code site within surface.
	site string

	// keys are the server's two sources.
	keys upstreamIssuerKeys

	// outcome is how the two compared.
	outcome upstreamIssuerOutcome

	// fromAssociations is the organization's flag.
	fromAssociations bool
}

// recordUpstreamIssuerParity counts one comparison and, for a disagreement,
// logs it once per organization, server, site and outcome in the dedup
// window. Agreement and governing choices log nothing.
func (s *Service) recordUpstreamIssuerParity(ctx context.Context, logger *slog.Logger, p upstreamIssuerParity) {
	s.metrics.RecordUpstreamIssuerParity(ctx, mcpmetrics.UpstreamIssuerParity{
		Surface:      p.surface,
		Site:         p.site,
		Outcome:      string(p.outcome),
		Source:       upstreamIssuerSource(p.fromAssociations, p.keys),
		ResolverMode: resolverModeLabel(feature.AuthClientResolver(ctx, s.features, p.organizationID)),
	})
	if p.outcome == upstreamIssuerMatch || p.outcome == upstreamIssuerChoice {
		return
	}
	key := strings.Join([]string{p.organizationID, p.serverID.String(), p.surface, p.site, string(p.outcome)}, "|")
	level := slog.LevelDebug
	if s.upstreamIssuerParityLog.first(key) {
		level = slog.LevelInfo
	}
	logger.Log(ctx, level, "upstream issuer sources disagree",
		attr.SlogOrganizationID(p.organizationID),
		attr.SlogMcpServerID(p.serverID.String()),
		attr.SlogUpstreamIssuerSurface(p.surface),
		attr.SlogUpstreamIssuerSite(p.site),
		attr.SlogUpstreamIssuerOutcome(string(p.outcome)),
		attr.SlogRemoteSessionIssuerID(p.keys.column.UUID.String()),
		attr.SlogUpstreamIssuerAssociationProvider(p.keys.association.Provider.UUID.String()),
		attr.SlogUpstreamIssuerAssociationProviders(p.keys.association.Providers),
	)
}

// upstreamIssuerReconsent classifies one consent check: stored is the
// evidence on record, decided the hash from the authoritative source, and
// other the hash from the other source.
func upstreamIssuerReconsent(stored, decided, other string, otherErr error) upstreamIssuerReconsentOutcome {
	switch {
	case otherErr != nil:
		return upstreamIssuerReconsentError
	case stored != decided:
		return upstreamIssuerReconsentAlreadyInvalid
	case stored != other:
		return upstreamIssuerReconsentForced
	default:
		return upstreamIssuerReconsentUnchanged
	}
}

// upstreamIssuerReconsentCheck is one consent check to record.
type upstreamIssuerReconsentCheck struct {
	// organizationID is the organization the consent belongs to.
	organizationID string

	// serverID is the MCP server, or gateway, whose evidence was checked.
	serverID uuid.UUID

	// site is the consent check site.
	site string

	// consenter identifies whose consent it is, such as a user session, so a
	// forced re-consent logs once per person rather than per request.
	consenter string

	// outcome is the check's forecast.
	outcome upstreamIssuerReconsentOutcome
}

// recordUpstreamIssuerReconsent counts one consent check and logs a forced
// re-consent once per organization, server, site and consenter in the dedup
// window.
func (s *Service) recordUpstreamIssuerReconsent(ctx context.Context, logger *slog.Logger, c upstreamIssuerReconsentCheck) {
	s.metrics.RecordUpstreamIssuerReconsent(ctx, c.site, string(c.outcome))
	if c.outcome != upstreamIssuerReconsentForced && c.outcome != upstreamIssuerReconsentError {
		return
	}
	key := strings.Join([]string{"reconsent", c.organizationID, c.serverID.String(), c.site, c.consenter, string(c.outcome)}, "|")
	level := slog.LevelDebug
	if s.upstreamIssuerParityLog.first(key) {
		level = slog.LevelInfo
	}
	logger.Log(ctx, level, "switching upstream issuer source changes consent",
		attr.SlogOrganizationID(c.organizationID),
		attr.SlogMcpServerID(c.serverID.String()),
		attr.SlogUpstreamIssuerSite(c.site),
		attr.SlogUpstreamIssuerOutcome(string(c.outcome)),
		attr.SlogUpstreamIssuerConsenter(c.consenter),
	)
}

// directUpstreamIssuerInput is one direct routing to compare.
type directUpstreamIssuerInput struct {
	// organizationID is the server's organization.
	organizationID string

	// serverID is the proxied MCP server.
	serverID uuid.UUID

	// site is where the routing happens.
	site string

	// tokens are the credentials already resolved for the request.
	tokens map[uuid.UUID]remotesessions.UpstreamToken

	// upstreamResource is the backend's upstream resource.
	upstreamResource string

	// tunneled reports a tunneled backend.
	tunneled bool

	// backendIssuerID is the server's column value, or the issuer of the
	// credential's own choice when choiceGoverns.
	backendIssuerID uuid.NullUUID

	// choiceGoverns reports that a credential's own client choice replaced
	// the column, so neither source decides.
	choiceGoverns bool

	// association is what the server's associations give, or nil when the
	// request resolved its tokens without reading them.
	association *remotesessions.ServerAssociationIssuer
}

// directUpstreamIssuer routes a direct request's resolved tokens with each of
// the server's two upstream issuer sources, records how the results compared,
// and returns the source the organization's flag makes authoritative. It never
// resolves a token itself. A governing choice, and a request that did not read
// the associations, return backendIssuerID unchanged.
func (s *Service) directUpstreamIssuer(ctx context.Context, logger *slog.Logger, in directUpstreamIssuerInput) uuid.NullUUID {
	fromAssociations := feature.UpstreamIssuerFromAssociations(ctx, s.features, in.organizationID)
	parity := upstreamIssuerParity{
		organizationID:   in.organizationID,
		serverID:         in.serverID,
		surface:          upstreamIssuerSurfaceDirect,
		site:             in.site,
		keys:             upstreamIssuerKeys{column: in.backendIssuerID, association: associationUpstreamIssuer{Provider: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Providers: 0}, known: false},
		outcome:          upstreamIssuerMatch,
		fromAssociations: fromAssociations,
	}
	switch {
	case in.choiceGoverns:
		parity.outcome = upstreamIssuerChoice
		s.recordUpstreamIssuerParity(ctx, logger, parity)
		return in.backendIssuerID
	case in.association == nil:
		parity.outcome = upstreamIssuerAssociationUnread
		s.recordUpstreamIssuerParity(ctx, logger, parity)
		return in.backendIssuerID
	}
	keys := upstreamIssuerKeys{
		column:      in.backendIssuerID,
		association: associationUpstreamIssuer{Provider: in.association.Provider, Providers: in.association.Providers},
		known:       in.association.Known,
	}
	parity.keys = keys
	if !keys.known {
		parity.outcome = upstreamIssuerNoBoundClients
		s.recordUpstreamIssuerParity(ctx, logger, parity)
		return in.backendIssuerID
	}
	columnToken, columnErr := routeUpstreamToken(ctx, discardLogger, in.tokens, in.upstreamResource, in.tunneled, keys.column)
	associationToken, associationErr := routeUpstreamToken(ctx, discardLogger, in.tokens, in.upstreamResource, in.tunneled, keys.association.Provider)
	parity.outcome = keys.outcome(sameRoutedUpstream(columnToken, columnErr, associationToken, associationErr))
	s.recordUpstreamIssuerParity(ctx, logger, parity)
	return keys.authoritative(fromAssociations)
}
