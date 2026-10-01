// The organization's own authorization server: one token endpoint per
// organization at which a workload exchanges its platform identity token for
// a session on any of the organization's MCP servers it may reach. It serves
// the clientless workload assertion grant and its RFC 8414 metadata only; the
// request's resource (RFC 8707) names the server.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// OrganizationAuthorizationServerRoute is the chi route pattern of an
// organization's authorization server: its issuer path, under which the
// token endpoint sits and after which its metadata path is named.
const OrganizationAuthorizationServerRoute = "/o/{orgSlug}"

// FederationOrganizationEndpointDisabled means the organization is outside
// the organization token endpoint's rollout, so its routes answer 404.
const FederationOrganizationEndpointDisabled FederationNotReadyReason = "organization_endpoint_disabled"

// OrganizationAuthorizationServer is what a federating platform is pointed at
// to exchange workload identity tokens once for the whole organization. Its
// values are the ones the organization's RFC 8414 document serves.
type OrganizationAuthorizationServer struct {
	// Issuer is the organization authorization server's issuer identifier,
	// and the iss of every session it mints.
	Issuer string

	// TokenEndpoint is where the platform sends its assertion. An assertion's
	// aud must be exactly this or Issuer.
	TokenEndpoint string

	// MetadataURL is where the RFC 8414 document is served.
	MetadataURL string

	// OnAuthenticationHost reports whether the endpoint is on Gram's
	// dedicated authentication host rather than the platform host.
	OnAuthenticationHost bool

	// Enabled reports whether the organization is inside the rollout, so the
	// metadata and token routes serve at all.
	Enabled bool

	// GrantTypesSupported is grant_types_supported as the document lists it.
	GrantTypesSupported []string

	// WorkloadGrantAdvertised reports whether jwt-bearer is listed because
	// the clientless workload assertion grant is available here.
	WorkloadGrantAdvertised bool

	// NotReady is why an exchange here cannot succeed, or FederationReady.
	NotReady FederationNotReadyReason
}

// organizationAuthorizationServerMetadata is the RFC 8414 document of an
// organization's authorization server. It has a token endpoint and nothing
// else: no authorization, registration or revocation endpoint, and no
// response type.
type organizationAuthorizationServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
}

// OrganizationAuthorizationServer derives the organization's authorization
// server as its metadata document serves it. The caller is responsible for
// authorizing the read.
func (s *Service) OrganizationAuthorizationServer(ctx context.Context, organizationID string) (OrganizationAuthorizationServer, error) {
	logger := s.logger.With(attr.SlogOrganizationID(organizationID))
	organization, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		return OrganizationAuthorizationServer{}, oops.E(oops.CodeUnexpected, err, "load organization").LogError(ctx, logger)
	}
	return s.organizationAuthorizationServer(ctx, logger, organization)
}

func (s *Service) organizationAuthorizationServer(ctx context.Context, logger *slog.Logger, organization orgrepo.OrganizationMetadatum) (OrganizationAuthorizationServer, error) {
	baseURL, onAuthenticationHost := s.organizationAuthorizationServerBaseURL()
	issuer, err := url.JoinPath(baseURL, "o", organization.Slug)
	if err != nil {
		return OrganizationAuthorizationServer{}, oops.E(oops.CodeUnexpected, err, "build organization issuer URL").LogError(ctx, logger)
	}
	metadataURL, err := url.JoinPath(baseURL, wellknown.OAuthAuthorizationServerPath, "o", organization.Slug)
	if err != nil {
		return OrganizationAuthorizationServer{}, oops.E(oops.CodeUnexpected, err, "build organization metadata URL").LogError(ctx, logger)
	}
	server := OrganizationAuthorizationServer{
		Issuer:                  issuer,
		TokenEndpoint:           issuer + "/token",
		MetadataURL:             metadataURL,
		OnAuthenticationHost:    onAuthenticationHost,
		Enabled:                 false,
		GrantTypesSupported:     []string{},
		WorkloadGrantAdvertised: false,
		NotReady:                FederationOrganizationEndpointDisabled,
	}

	evaluation, err := feature.EvaluateFlag(ctx, s.features, feature.FlagOrgTokenEndpoint, organization.ID, feature.OrgProjectGroups(organization.Slug, ""))
	if err != nil {
		return OrganizationAuthorizationServer{}, oops.E(oops.CodeUnavailable, err, "organization token endpoint rollout is unavailable").LogWarn(ctx, logger)
	}
	if evaluation != feature.EvaluationEnabled {
		return server, nil
	}
	server.Enabled = true

	// Advertised from the deployment alone, as a server's own metadata is:
	// the agent authorization rollout is enforced at the token endpoint and
	// reported through NotReady.
	if s.workloadGrant == nil {
		server.NotReady = FederationWorkloadGrantUnavailable
		return server, nil
	}
	server.GrantTypesSupported = []string{oauthwire.GrantTypeJWTBearer}
	server.WorkloadGrantAdvertised = true
	server.NotReady = FederationReady

	rolloutEnabled, _, err := s.organizationAgentAuthorizationRollout(ctx, logger, organization.ID)
	if err != nil {
		return OrganizationAuthorizationServer{}, oops.E(oops.CodeUnavailable, err, "agent authorization rollout is unavailable").LogError(ctx, logger)
	}
	if !rolloutEnabled {
		server.NotReady = FederationAgentRolloutDisabled
	}
	return server, nil
}

// organizationAuthorizationServerBaseURL is the origin every organization's
// authorization server is served on: the authentication host when one is
// configured, otherwise the platform host.
func (s *Service) organizationAuthorizationServerBaseURL() (string, bool) {
	if s.authenticationHostBaseURL != "" {
		return s.authenticationHostBaseURL, true
	}
	return s.platformBaseURL(), false
}

// servesOrganizationAuthorizationServer reports whether the request arrived
// where organization authorization servers are served: the authentication
// host when one is configured, otherwise the platform host alone, never a
// custom domain or private network listener.
func (s *Service) servesOrganizationAuthorizationServer(ctx context.Context) bool {
	if s.authenticationHostBaseURL != "" {
		return OnAuthenticationHost(ctx)
	}
	if origin, ok := requestorigin.FromContext(ctx); ok && origin.Surface != requestorigin.SurfacePlatform {
		return false
	}
	return customdomains.FromContext(ctx) == nil
}

// loadOrganizationAuthorizationServer resolves the organization a request's
// route names and its authorization server. Every way the route does not
// serve answers 404 alike: the wrong host, an unknown or disabled
// organization, or one outside the rollout.
func (s *Service) loadOrganizationAuthorizationServer(r *http.Request) (orgrepo.OrganizationMetadatum, OrganizationAuthorizationServer, *slog.Logger, error) {
	ctx := r.Context()
	notFound := func(err error) error {
		return oops.E(oops.CodeNotFound, err, "not found")
	}
	if !s.servesOrganizationAuthorizationServer(ctx) {
		return orgrepo.OrganizationMetadatum{}, OrganizationAuthorizationServer{}, nil, notFound(nil)
	}
	slug := chi.URLParam(r, "orgSlug")
	if slug == "" {
		return orgrepo.OrganizationMetadatum{}, OrganizationAuthorizationServer{}, nil, notFound(nil)
	}
	organization, err := orgrepo.New(s.db).GetOrganizationMetadataBySlug(ctx, slug)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return orgrepo.OrganizationMetadatum{}, OrganizationAuthorizationServer{}, nil, notFound(err)
	case err != nil:
		return orgrepo.OrganizationMetadatum{}, OrganizationAuthorizationServer{}, nil, oops.E(oops.CodeUnexpected, err, "load organization").LogError(ctx, s.logger)
	case organization.DisabledAt.Valid:
		return orgrepo.OrganizationMetadatum{}, OrganizationAuthorizationServer{}, nil, notFound(nil)
	}
	logger := s.logger.With(attr.SlogOrganizationID(organization.ID))

	server, err := s.organizationAuthorizationServer(ctx, logger, organization)
	if err != nil {
		return orgrepo.OrganizationMetadatum{}, OrganizationAuthorizationServer{}, nil, err
	}
	if !server.Enabled {
		return orgrepo.OrganizationMetadatum{}, OrganizationAuthorizationServer{}, nil, notFound(nil)
	}
	return organization, server, logger, nil
}

// HandleGetOrganizationAuthorizationServer serves the RFC 8414 document of an
// organization's authorization server at
// `/.well-known/oauth-authorization-server/o/{orgSlug}`.
func (s *Service) HandleGetOrganizationAuthorizationServer(w http.ResponseWriter, r *http.Request) error {
	_, server, logger, err := s.loadOrganizationAuthorizationServer(r)
	if err != nil {
		return err
	}
	return writeJSONMetadata(r.Context(), w, r, logger, organizationAuthorizationServerMetadataFor(server))
}

func organizationAuthorizationServerMetadataFor(server OrganizationAuthorizationServer) organizationAuthorizationServerMetadata {
	return organizationAuthorizationServerMetadata{
		Issuer:                            server.Issuer,
		TokenEndpoint:                     server.TokenEndpoint,
		GrantTypesSupported:               server.GrantTypesSupported,
		ResponseTypesSupported:            []string{},
		TokenEndpointAuthMethodsSupported: []string{oauthwire.AuthMethodNone},
	}
}

// HandleOrganizationToken is the organization authorization server's token
// endpoint, `POST /o/{orgSlug}/token`. It serves the clientless workload
// assertion grant alone.
//
// The assertion is admitted against the organization's own issuers and
// admissions before the resource is looked at, so an unauthenticated caller
// learns nothing about which resources exist. Once admitted, a resource that
// does not resolve, belongs to another organization, or is out of the
// workload's agent's reach is refused with the same invalid_target.
func (s *Service) HandleOrganizationToken(w http.ResponseWriter, r *http.Request) error {
	organization, server, logger, err := s.loadOrganizationAuthorizationServer(r)
	if err != nil {
		return err
	}
	ctx := r.Context()

	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidRequest, "failed to parse form")
	}
	if r.PostForm.Get("grant_type") != oauthwire.GrantTypeJWTBearer || !server.WorkloadGrantAdvertised {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeUnsupportedGrantType, "unsupported grant_type")
	}
	if _, _, basic := r.BasicAuth(); basic || extractClientCredentials(r).presented() {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidRequest, "client authentication is not accepted at this token endpoint")
	}

	presented := presentedWorkload{issuerURL: "", subject: "", issuerID: uuid.Nil}
	rolloutEnabled, _, err := s.organizationAgentAuthorizationRollout(ctx, logger, organization.ID)
	switch {
	case err != nil:
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, workloadGrantStageUnavailable("agent_rollout_unavailable", err))
	case !rolloutEnabled:
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, refuseWorkloadGrant("agent_rollout_disabled", errWorkloadRolloutDisabled))
	}

	assertion := r.PostForm.Get("assertion")
	resources := r.PostForm["resource"]
	switch {
	case assertion == "":
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidRequest, "assertion is required")
	case len(resources) == 0:
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidRequest, "resource is required")
	case len(resources) > 1:
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidTarget, "exactly one resource is required")
	}
	resource := resources[0]

	presented, err = admitWorkloadAssertion(ctx, s.workloadGrant, organizationWorkloadTenancy(organization.ID), []string{server.Issuer, server.TokenEndpoint}, assertion)
	if err != nil {
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, err)
	}

	logger = logger.With(attr.SlogOAuthResource(resource))
	targetCtx := s.organizationTargetContext(ctx)
	endpoint, err := s.resolveOrganizationWorkloadTarget(targetCtx, logger, organization.ID, resource)
	if err != nil {
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, err)
	}

	err = s.issueWorkloadSession(targetCtx, w, endpoint, presented, workloadSessionIssuance{
		Resource: resource,
		BaseURL:  s.platformBaseURL(),
		Issuer:   server.Issuer,
	}, logger)
	if refusal, refused := errors.AsType[*workloadGrantError](err); refused {
		// After admission every refusal is about the target, and must read
		// the same as a resource that does not exist.
		if refusal.outcome == workloadGrantRefused {
			refusal.outcome = workloadGrantTargetRefused
		}
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, refusal)
	}
	return err
}

// organizationTargetContext is the context a target server is resolved and
// its session minted under: a public request on the platform host. The
// organization endpoint is its own authorization server, so where the
// target's own authorization server is announced does not decide whether it
// may be reached from here; the authentication host marker is cleared so the
// target's user session issuer is not required to opt in to that host.
func (s *Service) organizationTargetContext(ctx context.Context) context.Context {
	ctx = requestorigin.WithContext(ctx, requestorigin.Origin{
		Surface:          requestorigin.SurfacePlatform,
		BaseURL:          s.platformBaseURL(),
		OrganizationID:   "",
		NetworkIngressID: uuid.Nil,
		NetworkIdentity:  nil,
	})
	ctx = customdomains.WithContext(ctx, nil)
	return context.WithValue(ctx, authenticationHostContextKey{}, "")
}

// resolveOrganizationWorkloadTarget resolves a resource indicator to the MCP
// endpoint it names in the organization, as a public request for it on the
// platform host resolves. ctx is an organizationTargetContext.
//
// Only a platform-host resource is accepted, in its canonical spelling: the
// organization endpoint is served on the authentication host, which stands in
// for the platform host alone, never for a custom domain or a private
// network address.
func (s *Service) resolveOrganizationWorkloadTarget(ctx context.Context, logger *slog.Logger, organizationID, resource string) (*ResolvedMcpEndpoint, error) {
	refuse := func(reason string, err error) error {
		return &workloadGrantError{reason: reason, outcome: workloadGrantTargetRefused, retryAfter: 0, err: err}
	}

	platformBaseURL := s.platformBaseURL()
	slug, ok := strings.CutPrefix(resource, platformBaseURL+"/mcp/")
	if !ok || slug == "" || strings.ContainsAny(slug, "/?#%") {
		return nil, refuse("resource_not_platform_mcp_url", errors.New("resource is not an MCP server URL on the platform host"))
	}

	endpoint, err := s.LoadResolvedMcpEndpointBySlug(ctx, logger, slug, "mcp")
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && shareable.Code == oops.CodeNotFound {
		return nil, refuse("resource_not_found", err)
	}
	if err != nil {
		return nil, workloadGrantStageUnavailable("resource_resolution_unavailable", err)
	}
	if endpoint.OrganizationID != organizationID {
		return nil, refuse("resource_other_organization", fmt.Errorf("resource belongs to organization %s", endpoint.OrganizationID))
	}
	// A session for a platform-host resource is useless once the
	// organization's custom domain carries an IP allowlist: runtime dispatch
	// on the platform host refuses every request for it.
	lockedDown, err := s.organizationCustomDomainLockdown(ctx, logger, organizationID)
	if err != nil {
		return nil, workloadGrantStageUnavailable("resource_lockdown_unavailable", err)
	}
	if lockedDown {
		return nil, refuse("resource_custom_domain_locked", errors.New("resource is only reachable through the organization's custom domain"))
	}
	canonical, err := endpoint.RootURL(platformBaseURL)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build workload grant resource identifier").LogError(ctx, logger)
	}
	if canonical != resource {
		return nil, refuse("resource_not_canonical", errors.New("resource is not the server's canonical URL"))
	}
	return endpoint, nil
}
