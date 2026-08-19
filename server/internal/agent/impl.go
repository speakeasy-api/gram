package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"
	"golang.org/x/sync/errgroup"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	srv "github.com/speakeasy-api/gram/server/gen/http/agent/server"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/marketplace"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// ProductFeaturesClient is the slice of the product-features client the agent
// service needs; declared here (mirroring hooks.ProductFeaturesClient) so
// tests can stub feature state without a database round-trip.
type ProductFeaturesClient interface {
	IsFeatureEnabled(ctx context.Context, organizationID string, feature productfeatures.Feature) (bool, error)
}

type Service struct {
	tracer          trace.Tracer
	logger          *slog.Logger
	db              *pgxpool.Pool
	repo            *repo.Queries
	auth            *auth.Auth
	authz           *authz.Engine
	audit           *audit.Logger
	productFeatures ProductFeaturesClient
	serverURL       string
	blobStore       assets.BlobStore
}

var (
	_ gen.Service = (*Service)(nil)
	_ gen.Auther  = (*Service)(nil)
)

// NewService constructs the agent service.
func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
	productFeatures ProductFeaturesClient,
	serverURL string,
	blobStore assets.BlobStore,
) *Service {
	logger = logger.With(attr.SlogComponent("agent"))
	return &Service{
		tracer:          tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/agent"),
		logger:          logger,
		db:              db,
		repo:            repo.New(db),
		auth:            auth.New(logger, db, sessions, authzEngine),
		authz:           authzEngine,
		audit:           auditLogger,
		productFeatures: productFeatures,
		serverURL:       serverURL,
		blobStore:       blobStore,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
	// Public capability-URL route for minted session handoffs (see
	// handoffs.go): unauthenticated by design, the token is the credential.
	o11y.AttachHandler(mux, http.MethodGet, "/shared/handoffs/{token}", oops.ErrHandle(service.logger, service.ServeSessionHandoff).ServeHTTP)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// GetPlugins returns every plugin assigned to the device user's resolved
// principal set within the caller's org, marketplace-first. From the polling
// user's email it resolves directory audiences, email → user_id, then RBAC
// role membership, to produce directory_group:<id>, directory_attribute:<...>,
// user:<id>, user:all, and role:<...> principals; the email principal and the
// org wildcard are always included so email- and everyone-scoped assignments
// still deliver.
//
// The polling identity is resolved by credential type (DNO-383), because who
// the key belongs to differs:
//   - Per-user key (`agent_user`): the key owner IS the enrolled developer
//     (minted by token-exchange or manual enrollment), so the polling identity
//     is the authenticated key owner (authCtx.Email) and the vouched email is
//     ignored. Delivery is bound to the authenticated principal.
//   - Org install key (`agent` scope): the key owner is whoever minted the org
//     token in the dashboard (an admin), NOT the developer. The polling identity
//     is the vouched email the MDM profile supplies in the Gram-User-Email
//     header — required here. This is the zero-touch MDM path where the
//     developer never signs in.
//
// SECURITY: a per-user `agent_user` key's polling identity is the authenticated
// key owner, so it cannot claim another member's user-/role-scoped plugins. An
// org `agent` install key still trusts the caller-supplied payload.Email (not
// bound to the authenticated principal): any holder of the shared org key can
// claim another member's email. That is the accepted shared-org-key limitation,
// and it closes for each device as it migrates to a per-user key.
// placeholderSerials are SMBIOS/DMI defaults that white-box hardware reports
// verbatim instead of a real serial. Every MDM passes them straight through,
// so an organization can hold many DIFFERENT machines carrying the identical
// "serial" in inventory. Storing a heartbeat under one would let a single
// agent install attest every one of those machines as device-verified — the
// strongest claim this product makes, asserted for machines that never ran
// the agent. Rejecting them costs those devices nothing: they fall back to
// the assigned-user email match, exactly like an agent that reports no serial
// at all.
var placeholderSerials = map[string]bool{
	"to be filled by o.e.m.": true,
	"to be filled by oem":    true,
	"default string":         true,
	"system serial number":   true,
	"not specified":          true,
	"not applicable":         true,
	"unknown":                true,
	"none":                   true,
	"n/a":                    true,
	"invalid":                true,
	"0":                      true,
	"123456789":              true,
	"0123456789":             true,
	"serial number":          true,
	"oem":                    true,
	"o.e.m.":                 true,
}

// normalizeSerial canonicalizes an agent-reported hardware serial for storage,
// returning "" when the value cannot serve as a device identity.
//
// Lowercasing mirrors conv.NormalizeEmail on the sibling user path: this
// table's dedup key and every coverage reader compare LOWER(serial_number),
// so the stored value must already be in that form or a machine could hold
// two rows and fan out its coverage.
func normalizeSerial(reported *string) string {
	serial := strings.ToLower(strings.TrimSpace(conv.PtrValOr(reported, "")))
	if placeholderSerials[serial] {
		return ""
	}
	return serial
}

func (s *Service) GetPlugins(ctx context.Context, payload *gen.GetPluginsPayload) (*gen.GetPluginsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// Resolve the polling identity by credential type. An org install key carries
	// the `agent` scope; a per-user key carries only `agent_user` (agent implies
	// agent_user, never the reverse — see auth.effectiveScopes), so the presence
	// of `agent` distinguishes them.
	isInstallKey := slices.Contains(authCtx.APIKeyScopes, auth.APIKeyScopeAgent.String())

	var email string
	if isInstallKey {
		// Org key: the owner is an admin, not the developer, so we must be vouched
		// an email — the MDM profile supplies it via the Gram-User-Email header,
		// or the deprecated `?email=` param on agents predating it.
		vouched := conv.PtrValOr(payload.Email, "")
		if vouched == "" {
			vouched = conv.PtrValOr(payload.LegacyEmail, "")
		}
		email = conv.NormalizeEmail(vouched)
		if email == "" {
			return nil, oops.E(oops.CodeBadRequest, nil, "the Gram-User-Email header is required when authenticating with an org-scoped agent install key")
		}
	} else if authCtx.Email != nil {
		// Per-user key: the owner is the enrolled developer, bound to the token.
		email = conv.NormalizeEmail(*authCtx.Email)
	}
	emailPrincipal, err := urn.ParsePrincipal(string(urn.PrincipalTypeEmail) + ":" + email)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid email")
	}

	// Best-effort: record that this user's device agent polled, so the dashboard
	// can show who is actively running it. Never fail the sync if the write fails
	// (mirrors api_keys.last_accessed_at). The query's ON CONFLICT guard caps
	// writes to at most once per minute per (org, email).
	if err := s.repo.UpsertDeviceAgentSync(ctx, repo.UpsertDeviceAgentSyncParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Email:          email,
	}); err != nil {
		s.logger.WarnContext(ctx, "failed to record device agent sync",
			attr.SlogError(err),
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		)
	}

	// Agents that can read their hardware serial additionally record a
	// per-device heartbeat, which is what lets coverage attest a specific
	// machine rather than its assigned user. Absent or blank means the agent
	// predates the capability, or runs on hardware with no readable serial
	// (white-box PCs report blank or a placeholder) — either way coverage
	// falls back to the per-user email match above, so this stays additive
	// and never gates the sync.
	// Normalized the way conv.NormalizeEmail normalizes the sibling path's
	// email: the dedup key and every reader compare LOWER(serial_number), so
	// storing the vendor's casing verbatim would leave the stored value and
	// its own key disagreeing.
	if serial := normalizeSerial(payload.SerialNumber); serial != "" {
		if err := s.repo.UpsertDeviceAgentDeviceSync(ctx, repo.UpsertDeviceAgentDeviceSyncParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			SerialNumber:   serial,
			Email:          email,
			Hostname:       conv.PtrToPGTextTrimmed(payload.Hostname),
		}); err != nil {
			s.logger.WarnContext(ctx, "failed to record device agent device sync",
				attr.SlogError(err),
				attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
			)
		}
	}

	// Assignments can target the email or the org wildcard directly; those always
	// apply regardless of whether the email maps to an org member.
	principals := []string{emailPrincipal.String(), urn.PrincipalWildcard}
	directoryAudiences, err := plugins.ResolveDirectoryAudiencePrincipalsByEmails(ctx, s.db, authCtx.ActiveOrganizationID, []string{email})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error resolving agent directory audiences").LogError(ctx, s.logger)
	}
	principals = append(principals, directoryAudiences[email]...)

	// Resolve the reported email to an org member so user:<id>, user:all, and
	// role:<kind>:<uuid> assignments deliver too. A non-member (or unknown email) is not
	// an error: the caller still receives email- and wildcard-scoped plugins.
	user, err := usersrepo.New(s.db).GetConnectedUserByEmail(ctx, usersrepo.GetConnectedUserByEmailParams{
		Email:          email,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	switch {
	case err == nil:
		resolved, err := authz.ResolveUserPrincipals(ctx, s.db, authCtx.ActiveOrganizationID, user.ID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error resolving agent principals").LogError(ctx, s.logger)
		}
		for _, principal := range resolved {
			principals = append(principals, principal.String())
		}
	case errors.Is(err, pgx.ErrNoRows):
		// Email is not an active member of this org; wildcard/email scoping only.
	default:
		return nil, oops.E(oops.CodeUnexpected, err, "error resolving agent user").LogError(ctx, s.logger)
	}

	var (
		rows             []repo.GetAgentPluginSetRow
		configurationRow repo.DeviceAgentConfiguration
		hasConfiguration bool
	)
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		var err error
		rows, err = s.repo.GetAgentPluginSet(groupCtx, repo.GetAgentPluginSetParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			PrincipalUrns:  principals,
		})
		if err != nil {
			return fmt.Errorf("resolve plugin set: %w", err)
		}
		return nil
	})
	group.Go(func() error {
		var err error
		configurationRow, err = s.repo.GetDeviceAgentConfiguration(groupCtx, authCtx.ActiveOrganizationID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil
		case err != nil:
			return fmt.Errorf("resolve remote configuration: %w", err)
		default:
			hasConfiguration = true
			return nil
		}
	})
	if err := group.Wait(); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error resolving agent policy").LogError(ctx, s.logger)
	}

	base := strings.TrimRight(s.serverURL, "/")
	marketplaceURL := func(token string) string {
		return base + marketplace.RoutePrefix + token + ".git"
	}

	result := mv.BuildAgentPluginsView(rows, marketplaceURL)
	if hasConfiguration {
		configuration, err := buildDeviceAgentConfigurationView(configurationRow)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error decoding agent configuration").LogError(ctx, s.logger)
		}
		attachDeviceAgentConfiguration(result, configuration)
	}

	return result, nil
}

// ListSyncedUsers returns the emails seen polling agent.getPlugins for the
// caller's org, most recently active first, for the dashboard's device-agent
// users view. Org admins only; attribution is by the email the agent reports on
// each sync (the org-scoped API key is shared across the fleet).
func (s *Service) ListSyncedUsers(ctx context.Context, _ *gen.ListSyncedUsersPayload) (*gen.ListSyncedUsersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(authCtx.ActiveOrganizationID),
		attr.UserID(authCtx.UserID),
	)

	rows, err := s.repo.ListDeviceAgentSyncs(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing device agent syncs").LogError(ctx, s.logger)
	}

	users := make([]*gen.SyncedAgentUser, 0, len(rows))
	for _, r := range rows {
		users = append(users, &gen.SyncedAgentUser{
			Email:       r.Email,
			FirstSeenAt: r.FirstSeenAt.Time.Format(time.RFC3339),
			LastSeenAt:  r.LastSeenAt.Time.Format(time.RFC3339),
		})
	}

	return &gen.ListSyncedUsersResult{Users: users}, nil
}

func (s *Service) GetConfiguration(ctx context.Context, _ *gen.GetConfigurationPayload) (*gen.DeviceAgentConfiguration, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{
		Scope:        authz.ScopeOrgAdmin,
		ResourceKind: "",
		ResourceID:   authCtx.ActiveOrganizationID,
		Dimensions:   nil,
	}); err != nil {
		return nil, err
	}

	row, err := s.repo.GetDeviceAgentConfiguration(ctx, authCtx.ActiveOrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return defaultDeviceAgentConfigurationView(), nil
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get device agent configuration").LogError(ctx, s.logger)
	default:
		view, err := buildDeviceAgentConfigurationView(row)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "decode device agent configuration").LogError(ctx, s.logger)
		}
		return view, nil
	}
}

func (s *Service) UpdateConfiguration(ctx context.Context, payload *gen.UpdateConfigurationPayload) (*gen.DeviceAgentConfiguration, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{
		Scope:        authz.ScopeOrgAdmin,
		ResourceKind: "",
		ResourceID:   authCtx.ActiveOrganizationID,
		Dimensions:   nil,
	}); err != nil {
		return nil, err
	}

	config := normalizeDeviceAgentConfiguration(payload.Config)
	if !authCtx.IsAdmin {
		for _, key := range platformAdminOnlyDeviceAgentConfigurationKeys {
			if _, present := config[key]; present {
				return nil, oops.E(oops.CodeForbidden, nil, "%s can only be set by a platform administrator", key)
			}
		}
	}

	// Fail fast on malformed input before opening a transaction and taking the
	// org-wide update lock; the merged document is validated again below.
	if _, err := validateDeviceAgentConfiguration(config); err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin device agent configuration update").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)

	// Serialize concurrent updates: the advisory lock covers the absent-row
	// case, and FOR UPDATE covers existing rows, so the unknown-key merge and
	// audit before-snapshot cannot read a config replaced beneath them.
	if err := queries.AcquireDeviceAgentConfigurationLock(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize device agent configuration update").LogError(ctx, s.logger)
	}

	var beforeSnapshot *audit.DeviceAgentConfigurationSnapshot
	before, err := queries.GetDeviceAgentConfigurationForUpdate(ctx, authCtx.ActiveOrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get existing device agent configuration").LogError(ctx, s.logger)
	default:
		if before.SchemaVersion > deviceAgentConfigurationSchemaVersion {
			return nil, oops.E(
				oops.CodeConflict,
				nil,
				"stored device agent configuration uses schema version %d, which this server cannot edit",
				before.SchemaVersion,
			)
		}
		snapshot, err := buildDeviceAgentConfigurationSnapshot(before)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "decode existing device agent configuration").LogError(ctx, s.logger)
		}
		beforeSnapshot = &snapshot

		config, err = mergeStoredDeviceAgentConfiguration(config, before.Config, replaceableDeviceAgentConfigurationKeys(authCtx.IsAdmin))
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "merge device agent configuration").LogError(ctx, s.logger)
		}
	}

	configJSON, err := validateDeviceAgentConfiguration(config)
	if err != nil {
		return nil, err
	}

	row, err := queries.UpsertDeviceAgentConfiguration(ctx, repo.UpsertDeviceAgentConfigurationParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		SchemaVersion:  deviceAgentConfigurationSchemaVersion,
		Config:         configJSON,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update device agent configuration").LogError(ctx, s.logger)
	}
	afterSnapshot, err := buildDeviceAgentConfigurationSnapshot(row)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "decode updated device agent configuration").LogError(ctx, s.logger)
	}

	if err := s.audit.LogOrganizationDeviceAgentConfigurationUpdated(
		ctx,
		dbtx,
		audit.LogOrganizationDeviceAgentConfigurationUpdatedEvent{
			OrganizationID:                         authCtx.ActiveOrganizationID,
			Actor:                                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:                       authCtx.Email,
			ActorSlug:                              nil,
			OrganizationSlug:                       authCtx.OrganizationSlug,
			DeviceAgentConfigurationSnapshotBefore: beforeSnapshot,
			DeviceAgentConfigurationSnapshotAfter:  &afterSnapshot,
		},
	); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log device agent configuration update").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit device agent configuration update").LogError(ctx, s.logger)
	}

	view, err := buildDeviceAgentConfigurationView(row)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "decode device agent configuration response").LogError(ctx, s.logger)
	}
	return view, nil
}
