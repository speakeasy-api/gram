// dashboard.go holds composite operations built for a specific dashboard flow:
// one atomic call that internally performs what several management API methods
// would otherwise do in sequence. They live in this package rather than a
// subpackage because they reach package-local helpers across clienthandlers.go,
// cimd.go, proxyregister.go and mcpserverissuersync.go.

package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/gen/types"
	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth/registration"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urls"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	serverIdentityClientModeAuto     = "auto"
	serverIdentityClientModeExisting = "existing"
	serverIdentityClientModeManual   = "manual"

	serverIdentityStatusRegistered = "registered"
	serverIdentityStatusLinked     = "linked"
)

// serverIdentityRequest is the validated CommitServerIdentityConfiguration
// payload.
type serverIdentityRequest struct {
	mcpServerID         uuid.UUID
	providerID          uuid.UUID
	createProvider      *gen.CreateRemoteSessionIssuerForm
	logoAssetID         uuid.NullUUID
	existingClientID    uuid.UUID
	clientMode          string
	clientConfiguration *gen.ServerIdentityClientConfiguration
}

func (s *Service) CommitServerIdentityConfiguration(ctx context.Context, payload *gen.CommitServerIdentityConfigurationPayload) (*gen.CommitServerIdentityConfigurationResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	req, err := parseServerIdentityRequest(payload)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid identity configuration").LogError(ctx, logger)
	}
	target, err := s.authorizeServerIdentityTarget(ctx, logger, authCtx, req)
	if err != nil {
		return nil, err
	}
	if err := s.checkServerIdentityRequest(ctx, logger, authCtx, &req, target); err != nil {
		return nil, err
	}

	commit := s.identity.Prepare(req.plan(authCtx, target))
	if err := commit.Preflight(ctx); err != nil {
		return nil, identityOopsError(err, "check identity configuration").LogError(ctx, logger)
	}
	reg, err := commit.Register(ctx)
	if err != nil {
		return nil, identityOopsError(err, "register client").LogError(ctx, logger)
	}
	if !reg.Ready() {
		return serverIdentityRegistrationResult(reg), nil
	}

	tx, err := commit.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	if err := recheckServerIdentityTarget(ctx, tx.DB(), *authCtx.ProjectID, target); err != nil {
		return nil, identityOopsError(err, "recheck MCP server").LogError(ctx, logger)
	}
	if err := commit.Lock(ctx, tx); err != nil {
		return nil, identityOopsError(err, "lock identity configuration").LogError(ctx, logger)
	}
	if err := commit.Bind(ctx, tx, reg); err != nil {
		return nil, identityOopsError(err, "bind identity client").LogError(ctx, logger)
	}
	res, err := commit.Commit(ctx, tx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit identity configuration").LogError(ctx, logger)
	}
	result, err := serverIdentityResultView(res, req.clientMode)
	if err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "build remote session client view").LogError(ctx, logger)
	}
	return result, nil
}

// authorizeServerIdentityTarget loads the MCP server and requires write access
// to it and to every server sharing its user session issuer.
func (s *Service) authorizeServerIdentityTarget(ctx context.Context, logger *slog.Logger, authCtx *contextvalues.AuthContext, req serverIdentityRequest) (mcpserversrepo.McpServer, error) {
	mcpRepo := mcpserversrepo.New(s.db)
	target, err := mcpRepo.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        req.mcpServerID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return mcpserversrepo.McpServer{}, oops.E(oops.CodeNotFound, err, "MCP server not found").LogError(ctx, logger)
		}
		return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "get MCP server").LogError(ctx, logger)
	}

	checks := []authz.Check{authz.MCPCheck(authz.ScopeMCPWrite, target.ID.String(), target.ProjectID.String())}
	if req.createProvider != nil || req.clientMode != serverIdentityClientModeExisting {
		checks = append(checks, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil})
	}
	// A user session issuer is not unique to one MCP server. The client binding
	// this commit rewrites is keyed by issuer, and ResyncMCPServerRemoteSessionIssuers
	// re-stamps every server carrying that issuer, so write access to the named
	// target alone would let a caller repoint its siblings.
	if target.UserSessionIssuerID.Valid {
		sharing, err := mcpRepo.ListMCPServerIDsByUserSessionIssuerID(ctx, mcpserversrepo.ListMCPServerIDsByUserSessionIssuerIDParams{
			ProjectID:           *authCtx.ProjectID,
			UserSessionIssuerID: target.UserSessionIssuerID,
		})
		if err != nil {
			return mcpserversrepo.McpServer{}, oops.E(oops.CodeUnexpected, err, "list MCP servers sharing the user session issuer").LogError(ctx, logger)
		}
		for _, id := range sharing {
			if id == target.ID {
				continue
			}
			checks = append(checks, authz.MCPCheck(authz.ScopeMCPWrite, id.String(), target.ProjectID.String()))
		}
	}
	if err := s.authz.Require(ctx, checks...); err != nil {
		return mcpserversrepo.McpServer{}, err
	}
	return target, nil
}

// checkServerIdentityRequest checks what only this flow knows about: the MCP
// server must be directly Remote MCP-backed, and a new provider's form and
// logo must be valid.
func (s *Service) checkServerIdentityRequest(ctx context.Context, logger *slog.Logger, authCtx *contextvalues.AuthContext, req *serverIdentityRequest, target mcpserversrepo.McpServer) error {
	if !target.RemoteMcpServerID.Valid || !target.UserSessionIssuerID.Valid {
		return oops.E(oops.CodeBadRequest, nil, "MCP server must be directly Remote MCP-backed and have a user session issuer").LogError(ctx, logger)
	}
	if _, err := remotemcprepo.New(s.db).GetServerByID(ctx, remotemcprepo.GetServerByIDParams{
		ID:        target.RemoteMcpServerID.UUID,
		ProjectID: *authCtx.ProjectID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "Remote MCP source not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "get Remote MCP source").LogError(ctx, logger)
	}
	if req.createProvider == nil {
		return nil
	}
	if err := validateServerIdentityProviderForm(req.createProvider); err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid Remote Identity Provider configuration").LogError(ctx, logger)
	}
	logoAssetID, err := conv.PtrToNullUUID(req.createProvider.LogoAssetID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid logo asset id").LogError(ctx, logger)
	}
	req.logoAssetID = logoAssetID
	if logoAssetID.Valid {
		assets, err := assetsrepo.New(s.db).GetAssetsByID(ctx, assetsrepo.GetAssetsByIDParams{ProjectID: *authCtx.ProjectID, Ids: []uuid.UUID{logoAssetID.UUID}})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "get logo asset").LogError(ctx, logger)
		}
		if len(assets) != 1 {
			return oops.E(oops.CodeNotFound, nil, "logo asset not found").LogError(ctx, logger)
		}
	}
	return nil
}

// plan describes the request as an identity plan: the server's current
// clients are replaced by the requested one.
func (r serverIdentityRequest) plan(authCtx *contextvalues.AuthContext, target mcpserversrepo.McpServer) IdentityPlan {
	provider := UseProvider(r.providerID)
	if r.createProvider != nil {
		provider = CreateProvider(createServerIdentityProviderParams(*authCtx.ProjectID, authCtx.ActiveOrganizationID, r.createProvider, r.logoAssetID))
	}
	var client ClientChoice
	switch r.clientMode {
	case serverIdentityClientModeExisting:
		client = LinkClient(r.existingClientID)
	case serverIdentityClientModeManual:
		client = ManualClient(ClientCredentials{
			ClientID:        conv.PtrValOr(r.clientConfiguration.ClientID, ""),
			ClientSecret:    conv.PtrValOr(r.clientConfiguration.ClientSecret, ""),
			SecretExpiresAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
			// A hand-entered credential carries no provider-reported issue
			// time, so the row is stamped when it is stored.
			IssuedAt:                pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
			TokenEndpointAuthMethod: r.clientConfiguration.TokenEndpointAuthMethod,
			Scope:                   r.clientConfiguration.Scope,
			Audience:                r.clientConfiguration.Audience,
		})
	default:
		client = RegisterClient(RegistrationPolicy{
			Scope:                   r.clientConfiguration.Scope,
			Audience:                r.clientConfiguration.Audience,
			TokenEndpointAuthMethod: r.clientConfiguration.TokenEndpointAuthMethod,
			RequireClientSecret:     false,
			AllowCIMD:               true,
		})
	}
	return IdentityPlan{
		Scope: IdentityScope{
			OrganizationID:   authCtx.ActiveOrganizationID,
			ProjectID:        *authCtx.ProjectID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName: authCtx.Email,
			// Gates tunnel-bound registration, which reaches a private network.
			ActorIsPlatformAdmin: authCtx.IsAdmin,
		},
		UserSessionIssuerID: target.UserSessionIssuerID.UUID,
		Provider:            provider,
		Client:              client,
		Bound:               ReplaceBound,
		ResourceDisplay:     nil,
	}
}

// serverIdentityRegistrationResult reports a registration that stopped short
// of a client: the provider needs manual setup, or refused registration.
func serverIdentityRegistrationResult(reg Registration) *gen.CommitServerIdentityConfigurationResult {
	if reg.ManualSetupRequired {
		return serverIdentityManualSetupResult(reg.Provider)
	}
	return serverIdentityFailureResult(reg.Provider, string(reg.Method), *reg.Failure)
}

// recheckServerIdentityTarget locks the MCP server and refuses the commit if
// its Remote MCP source or user session issuer moved, or the source went away,
// since they were read. The denormalized remote_session_issuer_id is not
// compared: the commit reads the current provider from the live bindings, and
// the post-commit restamp of that column can lag a concurrent commit.
func recheckServerIdentityTarget(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, target mcpserversrepo.McpServer) error {
	locked, err := mcpserversrepo.New(tx).LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{
		ID:        target.ID,
		ProjectID: projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return identityRefusal(ErrIdentityNotFound, err, "MCP server not found")
	}
	if err != nil {
		return fmt.Errorf("lock MCP server: %w", err)
	}
	if locked.RemoteMcpServerID != target.RemoteMcpServerID || locked.UserSessionIssuerID != target.UserSessionIssuerID {
		return identityRefusal(ErrIdentityConflict, nil, "MCP server identity configuration changed while preparing the request")
	}
	if _, err := remotemcprepo.New(tx).GetServerByID(ctx, remotemcprepo.GetServerByIDParams{ID: locked.RemoteMcpServerID.UUID, ProjectID: projectID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return identityRefusal(ErrIdentityNotFound, err, "Remote MCP source not found")
		}
		return fmt.Errorf("get Remote MCP source: %w", err)
	}
	return nil
}

// identityOopsError maps an identity transaction error onto the management
// API's codes. A refusal keeps its own caller-facing message; any other error
// is unexpected and reported with message.
func identityOopsError(err error, message string) *oops.ShareableError {
	var refusal *IdentityError
	if !errors.As(err, &refusal) {
		return oops.E(oops.CodeUnexpected, err, "%s", message)
	}
	code := oops.CodeUnexpected
	switch {
	case errors.Is(refusal.Kind, ErrIdentityNotFound):
		code = oops.CodeNotFound
	case errors.Is(refusal.Kind, ErrIdentityInvalid):
		code = oops.CodeBadRequest
	case errors.Is(refusal.Kind, ErrIdentityForbidden):
		code = oops.CodeForbidden
	case errors.Is(refusal.Kind, ErrIdentityConflict), errors.Is(refusal.Kind, ErrIdentityOrgWideBinding):
		code = oops.CodeConflict
	case errors.Is(refusal.Kind, ErrIdentityInvariant):
		code = oops.CodeInvariantViolation
	}
	return oops.E(code, err, "%s", refusal.Message)
}

func serverIdentityResultView(res IdentityResult, clientMode string) (*gen.CommitServerIdentityConfigurationResult, error) {
	clientView, err := mv.BuildRemoteSessionClientView(res.Client, res.Bindings)
	if err != nil {
		return nil, fmt.Errorf("build remote session client view: %w", err)
	}
	providerTier := scopeOf(res.Provider).String()
	clientTier := conv.Ternary(res.Client.ProjectID.Valid, "project-specific", "organization-level")
	status := conv.Ternary(clientMode == serverIdentityClientModeExisting, serverIdentityStatusLinked, serverIdentityStatusRegistered)
	method := string(res.Method)
	providerPath := serverIdentityProviderPath(res.Provider.ID)
	clientPath := serverIdentityClientPath(res.Provider.ID, res.Client.ID)
	return &gen.CommitServerIdentityConfigurationResult{
		Status:              &status,
		RegistrationMethod:  &method,
		ManualSetupRequired: false,
		Provider:            mv.BuildRemoteSessionIssuerView(res.Provider),
		Client:              clientView,
		ProviderTier:        &providerTier,
		ClientTier:          &clientTier,
		ProviderPath:        &providerPath,
		ClientPath:          &clientPath,
		Failure:             nil,
	}, nil
}

func parseServerIdentityRequest(payload *gen.CommitServerIdentityConfigurationPayload) (serverIdentityRequest, error) {
	req := serverIdentityRequest{
		mcpServerID:         uuid.Nil,
		providerID:          uuid.Nil,
		createProvider:      payload.CreateProvider,
		logoAssetID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		existingClientID:    uuid.Nil,
		clientMode:          payload.ClientMode,
		clientConfiguration: payload.ClientConfiguration,
	}
	var err error
	req.mcpServerID, err = uuid.Parse(payload.McpServerID)
	if err != nil {
		return serverIdentityRequest{}, fmt.Errorf("parse mcp_server_id: %w", err)
	}
	if (payload.ProviderID == nil) == (payload.CreateProvider == nil) {
		return serverIdentityRequest{}, errors.New("exactly one of provider_id or create_provider is required")
	}
	if payload.CreateProvider != nil {
		// Normalized once so the slug-conflict preflight and the insert agree.
		form := *payload.CreateProvider
		form.Slug = strings.TrimSpace(form.Slug)
		req.createProvider = &form
	}
	if payload.ProviderID != nil {
		req.providerID, err = uuid.Parse(*payload.ProviderID)
		if err != nil {
			return serverIdentityRequest{}, fmt.Errorf("parse provider_id: %w", err)
		}
	}
	switch payload.ClientMode {
	case serverIdentityClientModeExisting:
		if payload.CreateProvider != nil {
			return serverIdentityRequest{}, errors.New("existing client mode requires an existing provider")
		}
		if payload.ExistingClientID == nil || payload.ClientConfiguration != nil {
			return serverIdentityRequest{}, errors.New("existing client mode requires existing_client_id and forbids client_configuration")
		}
		req.existingClientID, err = uuid.Parse(*payload.ExistingClientID)
		if err != nil {
			return serverIdentityRequest{}, fmt.Errorf("parse existing_client_id: %w", err)
		}
	case serverIdentityClientModeAuto:
		if payload.ExistingClientID != nil || payload.ClientConfiguration == nil {
			return serverIdentityRequest{}, errors.New("auto client mode requires client_configuration and forbids existing_client_id")
		}
		if payload.ClientConfiguration.ClientID != nil || payload.ClientConfiguration.ClientSecret != nil {
			return serverIdentityRequest{}, errors.New("auto client mode forbids client_id and client_secret")
		}
	case serverIdentityClientModeManual:
		if payload.ExistingClientID != nil || payload.ClientConfiguration == nil {
			return serverIdentityRequest{}, errors.New("manual client mode requires client_configuration and forbids existing_client_id")
		}
		clientID := strings.TrimSpace(conv.PtrValOr(payload.ClientConfiguration.ClientID, ""))
		if clientID == "" {
			return serverIdentityRequest{}, errors.New("manual client mode requires client_id")
		}
		secret := conv.PtrValOr(payload.ClientConfiguration.ClientSecret, "")
		method := conv.PtrValOr(payload.ClientConfiguration.TokenEndpointAuthMethod, "")
		if (method == string(TokenEndpointAuthMethodBasic) || method == string(TokenEndpointAuthMethodPost)) && secret == "" {
			return serverIdentityRequest{}, fmt.Errorf("%s requires client_secret", method)
		}
	default:
		return serverIdentityRequest{}, fmt.Errorf("unsupported client_mode %q", payload.ClientMode)
	}
	return req, nil
}

func validateServerIdentityProviderForm(form *gen.CreateRemoteSessionIssuerForm) error {
	if form.Slug == "" {
		return errors.New("slug is required")
	}
	trimmedIssuer := strings.TrimSpace(form.Issuer)
	if trimmedIssuer == "" {
		return errors.New("issuer is required")
	}
	// Binding a provider to a tunnel is platform-admin-only and is authorized by
	// resolveIssuerTunnelBinding on the management path. This RPC is gated by
	// mcp:write/project:write, so it refuses the field rather than silently
	// dropping a binding the caller asked for.
	if conv.PtrValOr(form.TunneledMcpServerID, "") != "" {
		return errors.New("tunneled_mcp_server_id is not accepted here; bind the tunnel through the Remote Identity Provider API")
	}
	if err := validateRemoteSessionProviderURLs(trimmedIssuer, remoteSessionProviderEndpoints{
		authorizationEndpoint: form.AuthorizationEndpoint,
		tokenEndpoint:         form.TokenEndpoint,
		revocationEndpoint:    form.RevocationEndpoint,
		registrationEndpoint:  form.RegistrationEndpoint,
		jwksURI:               form.JwksURI,
		userinfoEndpoint:      form.UserinfoEndpoint,
		introspectionEndpoint: form.IntrospectionEndpoint,
	}); err != nil {
		return err
	}
	for name, value := range map[string]*string{
		"client_setup_documentation_url": form.ClientSetupDocumentationURL,
		"service_documentation":          form.ServiceDocumentation,
		"op_policy_uri":                  form.OpPolicyURI,
		"op_tos_uri":                     form.OpTosURI,
	} {
		if v := conv.PtrValOr(value, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
			return fmt.Errorf("%s must be an absolute http(s) URL", name)
		}
	}
	return nil
}

func createServerIdentityProviderParams(projectID uuid.UUID, organizationID string, form *gen.CreateRemoteSessionIssuerForm, logoAssetID uuid.NullUUID) repo.CreateRemoteSessionIssuerParams {
	return repo.CreateRemoteSessionIssuerParams{
		TunneledMcpServerID:                 uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ProjectID:                           conv.ToNullUUID(projectID),
		OrganizationID:                      conv.ToPGText(organizationID),
		Slug:                                form.Slug,
		Issuer:                              strings.TrimSpace(form.Issuer),
		Name:                                conv.PtrToPGTextTrimmed(form.Name),
		LogoAssetID:                         logoAssetID,
		ClientSetupDocumentationUrl:         conv.PtrToPGTextEmpty(form.ClientSetupDocumentationURL),
		AuthorizationEndpoint:               conv.PtrToPGText(form.AuthorizationEndpoint),
		TokenEndpoint:                       conv.PtrToPGText(form.TokenEndpoint),
		RevocationEndpoint:                  conv.PtrToPGText(form.RevocationEndpoint),
		RegistrationEndpoint:                conv.PtrToPGText(form.RegistrationEndpoint),
		JwksUri:                             conv.PtrToPGText(form.JwksURI),
		ServiceDocumentation:                conv.PtrToPGTextEmpty(form.ServiceDocumentation),
		OpPolicyUri:                         conv.PtrToPGTextEmpty(form.OpPolicyURI),
		OpTosUri:                            conv.PtrToPGTextEmpty(form.OpTosURI),
		ScopesSupported:                     orEmptySlice(form.ScopesSupported),
		GrantTypesSupported:                 orEmptySlice(form.GrantTypesSupported),
		AuthorizationGrantProfilesSupported: form.AuthorizationGrantProfilesSupported,
		ResponseTypesSupported:              orEmptySlice(form.ResponseTypesSupported),
		TokenEndpointAuthMethodsSupported:   orEmptySlice(form.TokenEndpointAuthMethodsSupported),
		CodeChallengeMethodsSupported:       form.CodeChallengeMethodsSupported,
		ClientIDMetadataDocumentSupported:   conv.PtrValOr(form.ClientIDMetadataDocumentSupported, false),
		UserinfoEndpoint:                    conv.PtrToPGTextEmpty(form.UserinfoEndpoint),
		IntrospectionEndpoint:               conv.PtrToPGTextEmpty(form.IntrospectionEndpoint),
		IntrospectionEndpointAuthMethodsSupported:  form.IntrospectionEndpointAuthMethodsSupported,
		IDTokenSigningAlgValuesSupported:           form.IDTokenSigningAlgValuesSupported,
		ClaimsSupported:                            form.ClaimsSupported,
		BackchannelLogoutSupported:                 conv.PtrToPGBool(form.BackchannelLogoutSupported),
		AuthorizationResponseIssParameterSupported: conv.PtrToPGBool(form.AuthorizationResponseIssParameterSupported),
		ScopeOverride:                              scopeOverride(form.ScopeOverride),
		ResourceIndicatorSupported:                 conv.PtrToPGBool(form.ResourceIndicatorSupported),
		Metadata:                                   nil,
		MetadataFetchedAt:                          pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		MetadataLastError:                          "",
		MetadataLastErrorUrl:                       "",
		Oidc:                                       conv.PtrValOr(form.Oidc, false),
		Passthrough:                                conv.PtrValOr(form.Passthrough, false),
	}
}

func serverIdentityFailureResult(provider repo.RemoteSessionIssuer, method string, failure registration.Failure) *gen.CommitServerIdentityConfigurationResult {
	var providerView *types.RemoteSessionIssuer
	var providerTier *string
	var providerPath *string
	if provider.ID != uuid.Nil {
		providerView = mv.BuildRemoteSessionIssuerView(provider)
		tier := scopeOf(provider).String()
		path := serverIdentityProviderPath(provider.ID)
		providerTier = &tier
		providerPath = &path
	}
	return &gen.CommitServerIdentityConfigurationResult{
		Status:              nil,
		RegistrationMethod:  &method,
		ManualSetupRequired: false,
		Provider:            providerView,
		Client:              nil,
		ProviderTier:        providerTier,
		ClientTier:          nil,
		ProviderPath:        providerPath,
		ClientPath:          nil,
		Failure: &gen.ServerIdentityRegistrationFailure{
			Outcome:         string(failure.Outcome),
			Reason:          string(failure.Reason),
			Retryable:       failure.Retryable,
			ProviderMessage: failure.ProviderMessage,
			HTTPStatus:      failure.HTTPStatus,
		},
	}
}

func serverIdentityManualSetupResult(provider repo.RemoteSessionIssuer) *gen.CommitServerIdentityConfigurationResult {
	providerView := mv.BuildRemoteSessionIssuerView(provider)
	var providerTier *string
	var providerPath *string
	if provider.ID != uuid.Nil {
		tier := scopeOf(provider).String()
		path := serverIdentityProviderPath(provider.ID)
		providerTier = &tier
		providerPath = &path
	} else {
		providerView = nil
	}
	return &gen.CommitServerIdentityConfigurationResult{
		Status:              nil,
		RegistrationMethod:  nil,
		ManualSetupRequired: true,
		Provider:            providerView,
		Client:              nil,
		ProviderTier:        providerTier,
		ClientTier:          nil,
		ProviderPath:        providerPath,
		ClientPath:          nil,
		Failure:             nil,
	}
}

func serverIdentityProviderPath(providerID uuid.UUID) string {
	return "/remote-identity-providers/" + providerID.String()
}

func serverIdentityClientPath(providerID, clientID uuid.UUID) string {
	return serverIdentityProviderPath(providerID) + "/clients/" + clientID.String()
}
