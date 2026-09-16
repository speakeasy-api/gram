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
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/gen/types"
	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
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

type serverIdentityPlan struct {
	mcpServerID         uuid.UUID
	providerID          uuid.UUID
	createProvider      *gen.CreateRemoteSessionIssuerForm
	logoAssetID         uuid.NullUUID
	existingClientID    uuid.UUID
	clientMode          string
	clientConfiguration *gen.ServerIdentityClientConfiguration
}

type serverIdentityProviderCapabilities struct {
	registrationEndpoint              pgtype.Text
	tokenEndpointAuthMethodsSupported []string
	clientIDMetadataDocumentSupported bool
}

func (s *Service) CommitServerIdentityConfiguration(ctx context.Context, payload *gen.CommitServerIdentityConfigurationPayload) (_ *gen.CommitServerIdentityConfigurationResult, retErr error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))
	plan, err := validateServerIdentityPlan(payload)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid identity configuration").LogError(ctx, logger)
	}

	mcpRepo := mcpserversrepo.New(s.db)
	target, err := mcpRepo.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        plan.mcpServerID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "MCP server not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get MCP server").LogError(ctx, logger)
	}

	checks := []authz.Check{authz.MCPCheck(authz.ScopeMCPWrite, target.ID.String(), target.ProjectID.String())}
	if plan.createProvider != nil || plan.clientMode != serverIdentityClientModeExisting {
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
			return nil, oops.E(oops.CodeUnexpected, err, "list MCP servers sharing the user session issuer").LogError(ctx, logger)
		}
		for _, id := range sharing {
			if id == target.ID {
				continue
			}
			checks = append(checks, authz.MCPCheck(authz.ScopeMCPWrite, id.String(), target.ProjectID.String()))
		}
	}
	if err := s.authz.Require(ctx, checks...); err != nil {
		return nil, err
	}

	if !target.RemoteMcpServerID.Valid || !target.UserSessionIssuerID.Valid {
		return nil, oops.E(oops.CodeBadRequest, nil, "MCP server must be directly Remote MCP-backed and have a user session issuer").LogError(ctx, logger)
	}
	if _, err := remotemcprepo.New(s.db).GetServerByID(ctx, remotemcprepo.GetServerByIDParams{
		ID:        target.RemoteMcpServerID.UUID,
		ProjectID: *authCtx.ProjectID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "Remote MCP source not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get Remote MCP source").LogError(ctx, logger)
	}

	q := repo.New(s.db)
	if _, err := q.GetUserSessionIssuerForProject(ctx, repo.GetUserSessionIssuerForProjectParams{
		ID:             target.UserSessionIssuerID.UUID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get user session issuer").LogError(ctx, logger)
	}

	var provider repo.RemoteSessionIssuer
	var providerCapabilities serverIdentityProviderCapabilities
	if plan.createProvider != nil {
		if err := validateServerIdentityProviderForm(plan.createProvider); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid Remote Identity Provider configuration").LogError(ctx, logger)
		}
		logoAssetID, err := conv.PtrToNullUUID(plan.createProvider.LogoAssetID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid logo asset id").LogError(ctx, logger)
		}
		plan.logoAssetID = logoAssetID
		if logoAssetID.Valid {
			assets, err := assetsrepo.New(s.db).GetAssetsByID(ctx, assetsrepo.GetAssetsByIDParams{ProjectID: *authCtx.ProjectID, Ids: []uuid.UUID{logoAssetID.UUID}})
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "get logo asset").LogError(ctx, logger)
			}
			if len(assets) != 1 {
				return nil, oops.E(oops.CodeNotFound, nil, "logo asset not found").LogError(ctx, logger)
			}
		}
		if _, err := q.GetRemoteSessionIssuerBySlug(ctx, repo.GetRemoteSessionIssuerBySlugParams{
			Slug:      plan.createProvider.Slug,
			ProjectID: conv.ToNullUUID(*authCtx.ProjectID),
		}); err == nil {
			return nil, oops.E(oops.CodeConflict, nil, "an issuer with this slug already exists").LogError(ctx, logger)
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeUnexpected, err, "check remote session issuer slug").LogError(ctx, logger)
		}
		providerCapabilities = providerCapabilitiesFromForm(plan.createProvider)
	} else {
		provider, err = q.GetRemoteSessionIssuerByID(ctx, repo.GetRemoteSessionIssuerByIDParams{
			ID:                    plan.providerID,
			ProjectID:             conv.ToNullUUID(*authCtx.ProjectID),
			OrganizationID:        conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
			IncludeOrganizational: true,
			IncludeGlobal:         true,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "Remote Identity Provider not found").LogError(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "get Remote Identity Provider").LogError(ctx, logger)
		}
		providerCapabilities = providerCapabilitiesFromRow(provider)
	}

	if plan.clientMode == serverIdentityClientModeExisting {
		existing, err := q.GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
			ProjectID:      *authCtx.ProjectID,
			OrganizationID: authCtx.ActiveOrganizationID,
			ID:             plan.existingClientID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "get remote session client").LogError(ctx, logger)
		}
		if existing.RemoteSessionClient.RemoteSessionIssuerID != provider.ID {
			return nil, oops.E(oops.CodeBadRequest, nil, "existing client does not belong to the selected provider").LogError(ctx, logger)
		}
		if !target.RemoteSessionIssuerID.Valid || target.RemoteSessionIssuerID.UUID != provider.ID {
			if err := preflightServerIdentityClientBinding(ctx, q, *authCtx.ProjectID, authCtx.ActiveOrganizationID, target.UserSessionIssuerID.UUID, provider.ID, existing.RemoteSessionClient.ID); err != nil {
				return nil, serverIdentityPreflightError(err)
			}
		}
	} else if plan.createProvider == nil && (!target.RemoteSessionIssuerID.Valid || target.RemoteSessionIssuerID.UUID != provider.ID) {
		if err := preflightServerIdentityClientBinding(ctx, q, *authCtx.ProjectID, authCtx.ActiveOrganizationID, target.UserSessionIssuerID.UUID, provider.ID, uuid.Nil); err != nil {
			return nil, serverIdentityPreflightError(err)
		}
	}

	registrationMethod := plan.clientMode
	var registered ProxyRegisterResponse
	dcrSucceeded := false
	localCommitCompleted := false
	defer func() {
		if dcrSucceeded && !localCommitCompleted && retErr != nil {
			if recorder, ok := s.registrationTelemetry.(registration.PostRegistrationCommitFailureRecorder); ok {
				recorder.RecordPostRegistrationCommitFailure(context.WithoutCancel(ctx), registration.MethodDCR)
			}
		}
	}()

	if plan.clientMode == serverIdentityClientModeAuto {
		if supportsServerIdentityCIMD(providerCapabilities) {
			registrationMethod = string(registration.MethodCIMD)
		} else if providerCapabilities.registrationEndpoint.Valid && strings.TrimSpace(providerCapabilities.registrationEndpoint.String) != "" {
			if !urls.IsAbsoluteHTTPSOrLoopback(providerCapabilities.registrationEndpoint.String) {
				return nil, oops.E(oops.CodeBadRequest, nil, "registration endpoint must be an absolute https URL, or http on loopback").LogError(ctx, logger)
			}
			registrationMethod = string(registration.MethodDCR)
			// Registering through a tunnel reaches a private network the
			// project cannot otherwise address, so it carries the same
			// platform-admin gate the management path applies when the binding
			// is created. mcp:write on one server is not enough to borrow it.
			if provider.TunneledMcpServerID.Valid && !authCtx.IsAdmin {
				return nil, oops.E(oops.CodeForbidden, nil, "registering through an MCP tunnel requires a platform admin").LogError(ctx, logger)
			}
			scope := strings.Join(plan.clientConfiguration.Scope, " ")
			registered, err = RegisterDynamicClient(ctx, s.policy, s.tunnels, s.serverURL, ProxyRegisterRequest{
				RegistrationEndpoint:    providerCapabilities.registrationEndpoint.String,
				TunneledMcpServerID:     conv.PtrEmpty(tunnelBindingID(provider.TunneledMcpServerID)),
				Scope:                   conv.PtrEmpty(scope),
				TokenEndpointAuthMethod: plan.clientConfiguration.TokenEndpointAuthMethod,
			}, s.registrationTelemetry)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return nil, err
				}
				failure := registration.ClassifyDCR(err)
				return serverIdentityFailureResult(provider, registrationMethod, failure), nil
			}
			dcrSucceeded = true
			if method, failure, ok := validateRegisteredClient(registered, plan.clientConfiguration.TokenEndpointAuthMethod); !ok {
				s.registrationTelemetry.RecordFailure(ctx, registration.MethodDCR, failure)
				return serverIdentityFailureResult(provider, registrationMethod, failure), nil
			} else {
				registered.TokenEndpointAuthMethod = method
			}
		} else {
			return serverIdentityManualSetupResult(provider), nil
		}
	}

	var secretCiphertext pgtype.Text
	clientSecret := ""
	if registrationMethod == string(registration.MethodDCR) {
		clientSecret = registered.ClientSecret
	} else if plan.clientMode == serverIdentityClientModeManual {
		clientSecret = conv.PtrValOr(plan.clientConfiguration.ClientSecret, "")
	}
	if clientSecret != "" {
		encrypted, err := s.enc.Encrypt([]byte(clientSecret))
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "encrypt client secret").LogError(ctx, logger)
		}
		secretCiphertext = conv.ToPGText(encrypted)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txMCPRepo := mcpserversrepo.New(dbtx)
	lockedTarget, err := txMCPRepo.LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{
		ID:        target.ID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "MCP server not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock MCP server").LogError(ctx, logger)
	}
	if !lockedTarget.RemoteMcpServerID.Valid || !lockedTarget.UserSessionIssuerID.Valid || lockedTarget.RemoteMcpServerID.UUID != target.RemoteMcpServerID.UUID || lockedTarget.UserSessionIssuerID.UUID != target.UserSessionIssuerID.UUID || lockedTarget.RemoteSessionIssuerID != target.RemoteSessionIssuerID {
		return nil, oops.E(oops.CodeConflict, nil, "MCP server identity configuration changed while preparing the request").LogError(ctx, logger)
	}
	if _, err := remotemcprepo.New(dbtx).GetServerByID(ctx, remotemcprepo.GetServerByIDParams{ID: lockedTarget.RemoteMcpServerID.UUID, ProjectID: *authCtx.ProjectID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "Remote MCP source not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get Remote MCP source").LogError(ctx, logger)
	}

	txRepo := repo.New(dbtx)
	if err := lockUserSessionIssuersForClientBinding(
		ctx,
		logger,
		dbtx,
		txRepo,
		*authCtx.ProjectID,
		authCtx.ActiveOrganizationID,
		[]uuid.UUID{target.UserSessionIssuerID.UUID},
	); err != nil {
		return nil, err
	}

	providerCreated := false
	if plan.createProvider != nil {
		provider, err = txRepo.CreateRemoteSessionIssuer(ctx, createServerIdentityProviderParams(*authCtx.ProjectID, authCtx.ActiveOrganizationID, plan.createProvider, plan.logoAssetID))
		if err != nil {
			if isRemoteSessionIssuerSlugConflict(err) {
				return nil, oops.E(oops.CodeConflict, err, "an issuer with this slug already exists").LogError(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "create Remote Identity Provider").LogError(ctx, logger)
		}
		providerCreated = true
	} else {
		// Locked, not merely re-read: UpdateRemoteSessionIssuer takes no
		// advisory lock, so an unlocked re-read only narrows the race to the
		// gap between it and the client insert below.
		currentProvider, err := txRepo.GetRemoteSessionIssuerByIDForConfigurationCommit(ctx, repo.GetRemoteSessionIssuerByIDForConfigurationCommitParams{
			ID:                    provider.ID,
			ProjectID:             conv.ToNullUUID(*authCtx.ProjectID),
			OrganizationID:        conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
			IncludeOrganizational: true,
			IncludeGlobal:         true,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "Remote Identity Provider not found").LogError(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "get Remote Identity Provider").LogError(ctx, logger)
		}
		if registrationMethod == string(registration.MethodDCR) && currentProvider.RegistrationEndpoint != providerCapabilities.registrationEndpoint {
			return nil, oops.E(oops.CodeConflict, nil, "Remote Identity Provider changed while registering the client").LogError(ctx, logger)
		}
		if registrationMethod == string(registration.MethodCIMD) && preflightCIMDIssuer(currentProvider) != nil {
			return nil, oops.E(oops.CodeConflict, nil, "Remote Identity Provider changed while preparing the client").LogError(ctx, logger)
		}
		provider = currentProvider
	}

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	if providerCreated {
		if err := s.auditLogger.LogRemoteSessionIssuerCreate(ctx, dbtx, audit.LogRemoteSessionIssuerCreateEvent{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Actor:                  actor,
			ActorDisplayName:       authCtx.Email,
			ActorSlug:              nil,
			RemoteSessionIssuerURN: urn.NewRemoteSessionIssuer(provider.ID),
			Slug:                   provider.Slug,
			IssuerURL:              provider.Issuer,
			Name:                   conv.FromPGText[string](provider.Name),
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log Remote Identity Provider creation").LogError(ctx, logger)
		}
	}

	if lockedTarget.RemoteSessionIssuerID.Valid {
		bound, err := txRepo.ListRemoteSessionClientsByProjectIDForUserSessionIssuer(ctx, repo.ListRemoteSessionClientsByProjectIDForUserSessionIssuerParams{
			UserSessionIssuerID:   target.UserSessionIssuerID.UUID,
			ProjectID:             *authCtx.ProjectID,
			OrganizationID:        authCtx.ActiveOrganizationID,
			RemoteSessionIssuerID: lockedTarget.RemoteSessionIssuerID,
			Cursor:                uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			LimitValue:            2,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list current MCP server identity clients").LogError(ctx, logger)
		}
		if len(bound) > 1 {
			return nil, oops.E(oops.CodeInvariantViolation, nil, "MCP server has multiple clients for its current Remote Identity Provider").LogError(ctx, logger)
		}
		for _, current := range bound {
			if plan.clientMode == serverIdentityClientModeExisting && current.RemoteSessionClient.ID == plan.existingClientID {
				continue
			}
			affected, err := txRepo.DetachRemoteSessionClientFromUserSessionIssuer(ctx, repo.DetachRemoteSessionClientFromUserSessionIssuerParams{
				RemoteSessionClientID: current.RemoteSessionClient.ID,
				UserSessionIssuerID:   target.UserSessionIssuerID.UUID,
			})
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "detach previous MCP server identity client").LogError(ctx, logger)
			}
			if affected == 0 {
				continue
			}
			if err := s.auditLogger.LogRemoteSessionClientDetachUserSessionIssuer(ctx, dbtx, audit.LogRemoteSessionClientUserSessionIssuerAttachmentEvent{
				OrganizationID:         authCtx.ActiveOrganizationID,
				ProjectID:              *authCtx.ProjectID,
				Actor:                  actor,
				ActorDisplayName:       authCtx.Email,
				ActorSlug:              nil,
				RemoteSessionClientURN: urn.NewRemoteSessionClient(current.RemoteSessionClient.ID),
				ClientID:               current.RemoteSessionClient.ClientID,
				UserSessionIssuerURN:   urn.NewUserSessionIssuer(target.UserSessionIssuerID.UUID),
			}); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "log previous MCP server identity client detachment").LogError(ctx, logger)
			}
		}
	}

	var client repo.RemoteSessionClient
	var userIssuerIDs []uuid.UUID
	if plan.clientMode == serverIdentityClientModeExisting {
		existing, err := txRepo.GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
			ProjectID:      *authCtx.ProjectID,
			OrganizationID: authCtx.ActiveOrganizationID,
			ID:             plan.existingClientID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "get remote session client").LogError(ctx, logger)
		}
		if existing.RemoteSessionClient.RemoteSessionIssuerID != provider.ID {
			return nil, oops.E(oops.CodeConflict, nil, "existing client no longer belongs to the selected provider").LogError(ctx, logger)
		}
		if err := s.guardSingleClientPerRemoteIssuer(ctx, logger, txRepo, authCtx.ActiveOrganizationID, *authCtx.ProjectID, target.UserSessionIssuerID.UUID, provider.ID, existing.RemoteSessionClient.ID); err != nil {
			return nil, err
		}
		client = existing.RemoteSessionClient
		userIssuerIDs = existing.UserSessionIssuerIds
	} else {
		if _, err := s.validateNewClientIssuers(ctx, logger, dbtx, txRepo, *authCtx.ProjectID, authCtx.ActiveOrganizationID, provider.ID, []uuid.UUID{target.UserSessionIssuerID.UUID}); err != nil {
			return nil, err
		}
		now := conv.ToPGTimestamptz(time.Now().UTC())
		var createErr error
		if registrationMethod == string(registration.MethodCIMD) {
			id, err := uuid.NewV7()
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "generate client id").LogError(ctx, logger)
			}
			client, createErr = txRepo.CreateRemoteSessionClientCIMD(ctx, repo.CreateRemoteSessionClientCIMDParams{
				ID:                    id,
				ProjectID:             conv.ToNullUUID(*authCtx.ProjectID),
				OrganizationID:        conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
				RemoteSessionIssuerID: provider.ID,
				ClientIDMetadataUri:   ClientMetadataDocumentURL(s.serverURL, id),
				ClientIDIssuedAt:      now,
				Scope:                 plan.clientConfiguration.Scope,
				Audience:              conv.PtrToPGText(plan.clientConfiguration.Audience),
			})
		} else {
			clientID := strings.TrimSpace(conv.PtrValOr(plan.clientConfiguration.ClientID, ""))
			authMethod := plan.clientConfiguration.TokenEndpointAuthMethod
			secretExpiresAt := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
			clientIDIssuedAt := now
			if registrationMethod == string(registration.MethodDCR) {
				clientID = registered.ClientID
				authMethod = conv.PtrEmpty(registered.TokenEndpointAuthMethod)
				secretExpiresAt = registered.ClientSecretExpiresAt
				// Keep what the provider said it issued. Rewriting it to our
				// own clock loses the only record of when the credential
				// actually began, which is what a rotation window is measured
				// against. Providers may omit it, hence the fallback.
				if registered.ClientIDIssuedAt.Valid {
					clientIDIssuedAt = registered.ClientIDIssuedAt
				}
			}
			client, createErr = txRepo.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
				ProjectID:                       conv.ToNullUUID(*authCtx.ProjectID),
				OrganizationID:                  conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
				RemoteSessionIssuerID:           provider.ID,
				ClientID:                        clientID,
				ClientSecretEncrypted:           secretCiphertext,
				ClientIDIssuedAt:                clientIDIssuedAt,
				ClientSecretExpiresAt:           secretExpiresAt,
				TokenEndpointAuthAudienceFormat: pgtype.Text{String: "", Valid: false},
				TokenEndpointAuthMethod:         conv.PtrToPGText(authMethod),
				Scope:                           plan.clientConfiguration.Scope,
				Audience:                        conv.PtrToPGText(plan.clientConfiguration.Audience),
				LegacyCallbackUrl:               false,
			})
		}
		if createErr != nil {
			return nil, oops.E(oops.CodeUnexpected, createErr, "create remote session client").LogError(ctx, logger)
		}
		userIssuerIDs = []uuid.UUID{target.UserSessionIssuerID.UUID}
		if err := s.auditLogger.LogRemoteSessionClientCreate(ctx, dbtx, audit.LogRemoteSessionClientCreateEvent{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Actor:                  actor,
			ActorDisplayName:       authCtx.Email,
			ActorSlug:              nil,
			RemoteSessionClientURN: urn.NewRemoteSessionClient(client.ID),
			ClientID:               client.ClientID,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log remote session client creation").LogError(ctx, logger)
		}
	}

	wasAttached := slices.Contains(userIssuerIDs, target.UserSessionIssuerID.UUID)
	if err := txRepo.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   target.UserSessionIssuerID.UUID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "attach remote session client to user session issuer").LogError(ctx, logger)
	}
	if !wasAttached {
		userIssuerIDs = append(userIssuerIDs, target.UserSessionIssuerID.UUID)
		sort.Slice(userIssuerIDs, func(i, j int) bool { return userIssuerIDs[i].String() < userIssuerIDs[j].String() })
		if err := s.auditLogger.LogRemoteSessionClientAttachUserSessionIssuer(ctx, dbtx, audit.LogRemoteSessionClientUserSessionIssuerAttachmentEvent{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Actor:                  actor,
			ActorDisplayName:       authCtx.Email,
			ActorSlug:              nil,
			RemoteSessionClientURN: urn.NewRemoteSessionClient(client.ID),
			ClientID:               client.ClientID,
			UserSessionIssuerURN:   urn.NewUserSessionIssuer(target.UserSessionIssuerID.UUID),
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log remote session client attachment").LogError(ctx, logger)
		}
	}

	if err := ResyncMCPServerRemoteSessionIssuers(ctx, dbtx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, []uuid.UUID{target.UserSessionIssuerID.UUID}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update MCP server identity configuration").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit identity configuration").LogError(ctx, logger)
	}
	localCommitCompleted = true

	clientView, err := mv.BuildRemoteSessionClientView(client, userIssuerIDs)
	if err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "build remote session client view").LogError(ctx, logger)
	}
	providerView := mv.BuildRemoteSessionIssuerView(provider)
	providerTier := scopeOf(provider).String()
	clientTier := "organization-level"
	if client.ProjectID.Valid {
		clientTier = "project-specific"
	}
	status := serverIdentityStatusRegistered
	if plan.clientMode == serverIdentityClientModeExisting {
		status = serverIdentityStatusLinked
	}
	providerPath := serverIdentityProviderPath(provider.ID)
	clientPath := serverIdentityClientPath(provider.ID, client.ID)
	return &gen.CommitServerIdentityConfigurationResult{
		Status:              &status,
		RegistrationMethod:  &registrationMethod,
		ManualSetupRequired: false,
		Provider:            providerView,
		Client:              clientView,
		ProviderTier:        &providerTier,
		ClientTier:          &clientTier,
		ProviderPath:        &providerPath,
		ClientPath:          &clientPath,
		Failure:             nil,
	}, nil
}

func validateServerIdentityPlan(payload *gen.CommitServerIdentityConfigurationPayload) (serverIdentityPlan, error) {
	plan := serverIdentityPlan{
		mcpServerID:         uuid.Nil,
		providerID:          uuid.Nil,
		createProvider:      payload.CreateProvider,
		logoAssetID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		existingClientID:    uuid.Nil,
		clientMode:          payload.ClientMode,
		clientConfiguration: payload.ClientConfiguration,
	}
	var err error
	plan.mcpServerID, err = uuid.Parse(payload.McpServerID)
	if err != nil {
		return serverIdentityPlan{}, fmt.Errorf("parse mcp_server_id: %w", err)
	}
	if (payload.ProviderID == nil) == (payload.CreateProvider == nil) {
		return serverIdentityPlan{}, errors.New("exactly one of provider_id or create_provider is required")
	}
	if payload.ProviderID != nil {
		plan.providerID, err = uuid.Parse(*payload.ProviderID)
		if err != nil {
			return serverIdentityPlan{}, fmt.Errorf("parse provider_id: %w", err)
		}
	}
	switch payload.ClientMode {
	case serverIdentityClientModeExisting:
		if payload.CreateProvider != nil {
			return serverIdentityPlan{}, errors.New("existing client mode requires an existing provider")
		}
		if payload.ExistingClientID == nil || payload.ClientConfiguration != nil {
			return serverIdentityPlan{}, errors.New("existing client mode requires existing_client_id and forbids client_configuration")
		}
		plan.existingClientID, err = uuid.Parse(*payload.ExistingClientID)
		if err != nil {
			return serverIdentityPlan{}, fmt.Errorf("parse existing_client_id: %w", err)
		}
	case serverIdentityClientModeAuto:
		if payload.ExistingClientID != nil || payload.ClientConfiguration == nil {
			return serverIdentityPlan{}, errors.New("auto client mode requires client_configuration and forbids existing_client_id")
		}
		if payload.ClientConfiguration.ClientID != nil || payload.ClientConfiguration.ClientSecret != nil {
			return serverIdentityPlan{}, errors.New("auto client mode forbids client_id and client_secret")
		}
	case serverIdentityClientModeManual:
		if payload.ExistingClientID != nil || payload.ClientConfiguration == nil {
			return serverIdentityPlan{}, errors.New("manual client mode requires client_configuration and forbids existing_client_id")
		}
		clientID := strings.TrimSpace(conv.PtrValOr(payload.ClientConfiguration.ClientID, ""))
		if clientID == "" {
			return serverIdentityPlan{}, errors.New("manual client mode requires client_id")
		}
		secret := conv.PtrValOr(payload.ClientConfiguration.ClientSecret, "")
		method := conv.PtrValOr(payload.ClientConfiguration.TokenEndpointAuthMethod, "")
		if (method == string(TokenEndpointAuthMethodBasic) || method == string(TokenEndpointAuthMethodPost)) && secret == "" {
			return serverIdentityPlan{}, fmt.Errorf("%s requires client_secret", method)
		}
	default:
		return serverIdentityPlan{}, fmt.Errorf("unsupported client_mode %q", payload.ClientMode)
	}
	return plan, nil
}

func validateServerIdentityProviderForm(form *gen.CreateRemoteSessionIssuerForm) error {
	if strings.TrimSpace(form.Slug) == "" {
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
	if err := validateRemoteSessionProviderURLs(remoteSessionProviderURLs{
		issuer:                trimmedIssuer,
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

func providerCapabilitiesFromForm(form *gen.CreateRemoteSessionIssuerForm) serverIdentityProviderCapabilities {
	return serverIdentityProviderCapabilities{
		registrationEndpoint:              conv.PtrToPGText(form.RegistrationEndpoint),
		tokenEndpointAuthMethodsSupported: form.TokenEndpointAuthMethodsSupported,
		clientIDMetadataDocumentSupported: conv.PtrValOr(form.ClientIDMetadataDocumentSupported, false),
	}
}

func providerCapabilitiesFromRow(provider repo.RemoteSessionIssuer) serverIdentityProviderCapabilities {
	return serverIdentityProviderCapabilities{
		registrationEndpoint:              provider.RegistrationEndpoint,
		tokenEndpointAuthMethodsSupported: provider.TokenEndpointAuthMethodsSupported,
		clientIDMetadataDocumentSupported: provider.ClientIDMetadataDocumentSupported,
	}
}

func supportsServerIdentityCIMD(provider serverIdentityProviderCapabilities) bool {
	if !provider.clientIDMetadataDocumentSupported {
		return false
	}
	methods := provider.tokenEndpointAuthMethodsSupported
	return len(methods) == 0 || slices.Contains(methods, string(TokenEndpointAuthMethodNone))
}

func createServerIdentityProviderParams(projectID uuid.UUID, organizationID string, form *gen.CreateRemoteSessionIssuerForm, logoAssetID uuid.NullUUID) repo.CreateRemoteSessionIssuerParams {
	return repo.CreateRemoteSessionIssuerParams{
		TunneledMcpServerID:               uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ProjectID:                         conv.ToNullUUID(projectID),
		OrganizationID:                    conv.ToPGText(organizationID),
		Slug:                              strings.TrimSpace(form.Slug),
		Issuer:                            strings.TrimSpace(form.Issuer),
		Name:                              conv.PtrToPGTextTrimmed(form.Name),
		LogoAssetID:                       logoAssetID,
		ClientSetupDocumentationUrl:       conv.PtrToPGTextEmpty(form.ClientSetupDocumentationURL),
		AuthorizationEndpoint:             conv.PtrToPGText(form.AuthorizationEndpoint),
		TokenEndpoint:                     conv.PtrToPGText(form.TokenEndpoint),
		RevocationEndpoint:                conv.PtrToPGText(form.RevocationEndpoint),
		RegistrationEndpoint:              conv.PtrToPGText(form.RegistrationEndpoint),
		JwksUri:                           conv.PtrToPGText(form.JwksURI),
		ServiceDocumentation:              conv.PtrToPGTextEmpty(form.ServiceDocumentation),
		OpPolicyUri:                       conv.PtrToPGTextEmpty(form.OpPolicyURI),
		OpTosUri:                          conv.PtrToPGTextEmpty(form.OpTosURI),
		ScopesSupported:                   orEmptySlice(form.ScopesSupported),
		GrantTypesSupported:               orEmptySlice(form.GrantTypesSupported),
		ResponseTypesSupported:            orEmptySlice(form.ResponseTypesSupported),
		TokenEndpointAuthMethodsSupported: orEmptySlice(form.TokenEndpointAuthMethodsSupported),
		CodeChallengeMethodsSupported:     form.CodeChallengeMethodsSupported,
		ClientIDMetadataDocumentSupported: conv.PtrValOr(form.ClientIDMetadataDocumentSupported, false),
		UserinfoEndpoint:                  conv.PtrToPGTextEmpty(form.UserinfoEndpoint),
		IntrospectionEndpoint:             conv.PtrToPGTextEmpty(form.IntrospectionEndpoint),
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

func preflightServerIdentityClientBinding(ctx context.Context, q *repo.Queries, projectID uuid.UUID, organizationID string, userIssuerID, providerID, excludedClientID uuid.UUID) error {
	bound, err := q.ListRemoteSessionClientsByProjectIDForUserSessionIssuer(ctx, repo.ListRemoteSessionClientsByProjectIDForUserSessionIssuerParams{
		UserSessionIssuerID:   userIssuerID,
		ProjectID:             projectID,
		OrganizationID:        organizationID,
		RemoteSessionIssuerID: uuid.NullUUID{UUID: providerID, Valid: true},
		Cursor:                uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		LimitValue:            2,
	})
	if err != nil {
		return fmt.Errorf("list remote session clients for user/remote issuer: %w", err)
	}
	for _, candidate := range bound {
		if candidate.RemoteSessionClient.ID != excludedClientID {
			return errServerIdentityClientConflict
		}
	}
	return nil
}

var errServerIdentityClientConflict = errors.New("a remote session client is already bound to this user session issuer for the same remote session issuer")

func serverIdentityPreflightError(err error) error {
	if errors.Is(err, errServerIdentityClientConflict) {
		return oops.E(oops.CodeConflict, err, "a remote session client is already bound to this user session issuer for the same remote session issuer")
	}
	return oops.E(oops.CodeUnexpected, err, "validate remote session client binding")
}

func validateRegisteredClient(registered ProxyRegisterResponse, preferredMethod *string) (string, registration.Failure, bool) {
	method := registered.TokenEndpointAuthMethod
	if method == "" {
		method = conv.PtrValOr(preferredMethod, "")
		if method == "" {
			if registered.ClientSecret == "" {
				method = string(TokenEndpointAuthMethodNone)
			} else {
				method = string(TokenEndpointAuthMethodBasic)
			}
		}
		registered.TokenEndpointAuthMethod = method
	}
	if method != string(TokenEndpointAuthMethodBasic) && method != string(TokenEndpointAuthMethodPost) && method != string(TokenEndpointAuthMethodNone) {
		return "", registration.InvalidSuccessResponse(0), false
	}
	if (method == string(TokenEndpointAuthMethodBasic) || method == string(TokenEndpointAuthMethodPost)) && registered.ClientSecret == "" {
		return "", registration.InvalidSuccessResponse(0), false
	}
	return method, registration.Failure{Outcome: "", Reason: "", Retryable: false, HTTPStatus: nil, ProviderMessage: nil}, true
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
