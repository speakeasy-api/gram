package remotesessions

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	adminrsgen "github.com/speakeasy-api/gram/server/gen/admin_remote_sessions"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urls"
)

// The adminRemoteSessions handlers curate global remote_session_issuer /
// remote_session_client records (project_id NULL AND organization_id NULL),
// shared across every organization. No project/org exists to scope an RBAC
// grant, so each handler gates inline on the platform-admin flag; audit is
// structured-logs only (audit_log.organization_id is NOT NULL).

// requirePlatformAdmin extracts the auth context and enforces the platform-admin
// flag. The returned logger is pre-tagged with the actor for audit/error lines.
func (s *Service) requirePlatformAdmin(ctx context.Context) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, s.logger, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogUserID(authCtx.UserID))

	if !authCtx.IsAdmin {
		return nil, logger, oops.E(oops.CodeForbidden, nil, "platform admin required").LogError(ctx, logger)
	}

	return authCtx, logger, nil
}

// orEmptySlice coalesces a nil slice to empty. The remote_session_issuers
// *_supported columns are NOT NULL: on INSERT an explicit NULL bypasses their
// empty-array default, and on UPDATE it violates the constraint outright. All
// four arrays are OPTIONAL in RFC 8414, so an upstream that omits one decodes
// to a nil slice and reaches the write path routinely.
func orEmptySlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// logGlobalMutation records a structured-log audit line (actor, action,
// subject) for a global mutation, standing in for the auditlogs rows globals
// can't have. Call it only after the transaction commits so the log never
// claims a mutation that rolled back.
func logGlobalMutation(ctx context.Context, logger *slog.Logger, authCtx *contextvalues.AuthContext, action, subject, subjectID string) {
	logger.InfoContext(ctx, "global remote session "+subject+" "+action,
		attr.SlogAuditAction(action),
		attr.SlogAuditSubject(subject),
		attr.SlogAuditSubjectID(subjectID),
		attr.SlogAuthUserEmail(conv.PtrValOrEmpty(authCtx.Email, "")),
	)
}

// --- Global issuers ---

// CreateGlobalIssuer creates a global remote_session_issuer (project_id NULL,
// organization_id NULL), reusing CreateRemoteSessionIssuer with NULL scoping.
func (s *Service) CreateGlobalIssuer(ctx context.Context, payload *adminrsgen.CreateGlobalIssuerPayload) (*types.RemoteSessionIssuer, error) {
	authCtx, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(payload.Slug) == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "slug is required").LogError(ctx, logger)
	}
	if strings.TrimSpace(payload.Issuer) == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "issuer is required").LogError(ctx, logger)
	}

	// Operator-supplied and later rendered as a link, so it is validated here.
	// An empty value stays legal: the create query stores it as NULL.
	if v := conv.PtrValOr(payload.ClientSetupDocumentationURL, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "client_setup_documentation_url must be an absolute http(s) URL").LogError(ctx, logger)
	}

	logoAssetID, err := conv.PtrToNullUUID(payload.LogoAssetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid logo asset id").LogError(ctx, logger)
	}

	// Discovery drops malformed documentation URLs, but a caller holding the write
	// scope can POST them without ever calling discover, and they are persisted
	// and later rendered as links. An empty value stays legal: the update queries
	// read it as the explicit "clear to NULL" sentinel.
	if v := conv.PtrValOr(payload.ServiceDocumentation, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "service_documentation must be an absolute http(s) URL").LogError(ctx, logger)
	}
	if v := conv.PtrValOr(payload.OpPolicyURI, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "op_policy_uri must be an absolute http(s) URL").LogError(ctx, logger)
	}
	if v := conv.PtrValOr(payload.OpTosURI, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "op_tos_uri must be an absolute http(s) URL").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	issuer, err := repo.New(dbtx).CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:                    pgtype.Text{String: "", Valid: false},
		Slug:                              strings.TrimSpace(payload.Slug),
		Issuer:                            strings.TrimSpace(payload.Issuer),
		Name:                              conv.PtrToPGTextTrimmed(payload.Name),
		LogoAssetID:                       logoAssetID,
		ClientSetupDocumentationUrl:       conv.PtrToPGTextEmpty(payload.ClientSetupDocumentationURL),
		AuthorizationEndpoint:             conv.PtrToPGText(payload.AuthorizationEndpoint),
		TokenEndpoint:                     conv.PtrToPGText(payload.TokenEndpoint),
		RegistrationEndpoint:              conv.PtrToPGText(payload.RegistrationEndpoint),
		JwksUri:                           conv.PtrToPGText(payload.JwksURI),
		ServiceDocumentation:              conv.PtrToPGTextEmpty(payload.ServiceDocumentation),
		OpPolicyUri:                       conv.PtrToPGTextEmpty(payload.OpPolicyURI),
		OpTosUri:                          conv.PtrToPGTextEmpty(payload.OpTosURI),
		ScopesSupported:                   orEmptySlice(payload.ScopesSupported),
		GrantTypesSupported:               orEmptySlice(payload.GrantTypesSupported),
		ResponseTypesSupported:            orEmptySlice(payload.ResponseTypesSupported),
		TokenEndpointAuthMethodsSupported: orEmptySlice(payload.TokenEndpointAuthMethodsSupported),
		ClientIDMetadataDocumentSupported: conv.PtrValOr(payload.ClientIDMetadataDocumentSupported, false),
		Oidc:                              conv.PtrValOr(payload.Oidc, false),
		Passthrough:                       conv.PtrValOr(payload.Passthrough, false),
	})
	if err != nil {
		if isGlobalRemoteSessionIssuerSlugConflict(err) {
			return nil, oops.E(oops.CodeConflict, err, "a global issuer with this slug already exists").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create global remote session issuer").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	logGlobalMutation(ctx, logger, authCtx, "create", "issuer", issuer.ID.String())

	return mv.BuildRemoteSessionIssuerView(issuer), nil
}

// GetGlobalIssuerDuplicatePreflight reports the global issuers that already
// describe a given upstream authorization server, so the catalog create and edit
// forms can warn before curating a second entry for one issuer.
//
// Scoped to the global partition. Tenant issuers naming the same URL are not
// reported: ListGlobalIssuerConvergenceCandidates is the surface for those, and
// answering that question here would put another organization's configuration
// in front of a form that is only asking about the shared catalog.
func (s *Service) GetGlobalIssuerDuplicatePreflight(ctx context.Context, payload *adminrsgen.GetGlobalIssuerDuplicatePreflightPayload) (*types.RemoteSessionIssuerDuplicatePreflight, error) {
	_, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}

	canonical, err := parseCanonicalIssuerURL(conv.PtrValOrEmpty(payload.Issuer, ""))
	if err != nil {
		return emptyIssuerDuplicatePreflight(), nil
	}

	candidates, err := repo.New(s.db).ListGlobalRemoteSessionIssuersByIssuerURL(ctx, repo.ListGlobalRemoteSessionIssuersByIssuerURLParams{
		Issuers:    canonical.matchCandidates(),
		LimitValue: maxIssuerDuplicateMatchesPerTier,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list global remote session issuers by issuer url").LogError(ctx, logger)
	}

	rows := make([]issuerDuplicateCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		rows = append(rows, issuerDuplicateCandidateFromRecord(candidate))
	}

	return buildIssuerDuplicatePreflight(rows), nil
}

// ListGlobalIssuers lists the global remote_session_issuers, each with the
// global and tenant-owned client counts that decide whether it can be deleted.
func (s *Service) ListGlobalIssuers(ctx context.Context, payload *adminrsgen.ListGlobalIssuersPayload) (*adminrsgen.ListGlobalRemoteSessionIssuersResult, error) {
	_, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}

	limit := pageLimit(payload.Limit)
	cursor, err := parseCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, logger)
	}

	rows, err := repo.New(s.db).ListGlobalRemoteSessionIssuers(ctx, repo.ListGlobalRemoteSessionIssuersParams{
		Cursor:     cursor,
		LimitValue: limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list global remote session issuers").LogError(ctx, logger)
	}

	items := make([]*adminrsgen.GlobalRemoteSessionIssuer, 0, len(rows))
	for _, row := range rows {
		items = append(items, &adminrsgen.GlobalRemoteSessionIssuer{
			Issuer:            mv.BuildRemoteSessionIssuerView(row.RemoteSessionIssuer),
			GlobalClientCount: int(row.GlobalClientCount),
			TenantClientCount: int(row.TenantClientCount),
		})
	}

	var nextCursor *string
	if len(rows) >= int(limit) {
		c := rows[len(rows)-1].RemoteSessionIssuer.ID.String()
		nextCursor = &c
	}

	return &adminrsgen.ListGlobalRemoteSessionIssuersResult{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// GetGlobalIssuer resolves a global remote_session_issuer by id, with the same
// client counts the listing carries so the detail view can describe a delete
// without a second round trip.
func (s *Service) GetGlobalIssuer(ctx context.Context, payload *adminrsgen.GetGlobalIssuerPayload) (*adminrsgen.GlobalRemoteSessionIssuer, error) {
	_, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}

	issuerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogError(ctx, logger)
	}

	row, err := repo.New(s.db).GetGlobalRemoteSessionIssuerWithClientCountsByID(ctx, issuerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "global remote session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get global remote session issuer").LogError(ctx, logger)
	}

	return &adminrsgen.GlobalRemoteSessionIssuer{
		Issuer:            mv.BuildRemoteSessionIssuerView(row.RemoteSessionIssuer),
		GlobalClientCount: int(row.GlobalClientCount),
		TenantClientCount: int(row.TenantClientCount),
	}, nil
}

// UpdateGlobalIssuer patches a global remote_session_issuer.
func (s *Service) UpdateGlobalIssuer(ctx context.Context, payload *adminrsgen.UpdateGlobalIssuerPayload) (*types.RemoteSessionIssuer, error) {
	authCtx, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}

	issuerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogError(ctx, logger)
	}

	if payload.Slug != nil && strings.TrimSpace(*payload.Slug) == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "slug cannot be set to empty").LogError(ctx, logger)
	}
	if payload.Issuer != nil && strings.TrimSpace(*payload.Issuer) == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "issuer cannot be set to empty").LogError(ctx, logger)
	}

	// Operator-supplied and later rendered as a link, so it is validated here.
	// An empty value stays legal: the update query reads it as the explicit
	// "clear to NULL" sentinel.
	if v := conv.PtrValOr(payload.ClientSetupDocumentationURL, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "client_setup_documentation_url must be an absolute http(s) URL").LogError(ctx, logger)
	}

	logoAssetID, err := conv.PtrToNullUUID(payload.LogoAssetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid logo asset id").LogError(ctx, logger)
	}

	// Discovery drops malformed documentation URLs, but a caller holding the write
	// scope can POST them without ever calling discover, and they are persisted
	// and later rendered as links. An empty value stays legal: the update queries
	// read it as the explicit "clear to NULL" sentinel.
	if v := conv.PtrValOr(payload.ServiceDocumentation, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "service_documentation must be an absolute http(s) URL").LogError(ctx, logger)
	}
	if v := conv.PtrValOr(payload.OpPolicyURI, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "op_policy_uri must be an absolute http(s) URL").LogError(ctx, logger)
	}
	if v := conv.PtrValOr(payload.OpTosURI, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "op_tos_uri must be an absolute http(s) URL").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	updated, err := repo.New(dbtx).UpdateGlobalRemoteSessionIssuer(ctx, repo.UpdateGlobalRemoteSessionIssuerParams{
		// Trimmed so the stored slug/issuer match what the emptiness validation
		// above saw; whitespace-only never reaches here, so the trimmed-empty →
		// NULL (keep) behavior of PtrToPGTextTrimmed cannot trigger.
		Slug:                              conv.PtrToPGTextTrimmed(payload.Slug),
		Issuer:                            conv.PtrToPGTextTrimmed(payload.Issuer),
		Name:                              conv.PtrToPGText(payload.Name),
		LogoAssetID:                       logoAssetID,
		ClientSetupDocumentationUrl:       conv.PtrToPGText(payload.ClientSetupDocumentationURL),
		AuthorizationEndpoint:             conv.PtrToPGText(payload.AuthorizationEndpoint),
		TokenEndpoint:                     conv.PtrToPGText(payload.TokenEndpoint),
		RegistrationEndpoint:              conv.PtrToPGText(payload.RegistrationEndpoint),
		JwksUri:                           conv.PtrToPGText(payload.JwksURI),
		ServiceDocumentation:              conv.PtrToPGText(payload.ServiceDocumentation),
		OpPolicyUri:                       conv.PtrToPGText(payload.OpPolicyURI),
		OpTosUri:                          conv.PtrToPGText(payload.OpTosURI),
		ScopesSupported:                   payload.ScopesSupported,
		GrantTypesSupported:               payload.GrantTypesSupported,
		ResponseTypesSupported:            payload.ResponseTypesSupported,
		TokenEndpointAuthMethodsSupported: payload.TokenEndpointAuthMethodsSupported,
		ClientIDMetadataDocumentSupported: conv.PtrToPGBool(payload.ClientIDMetadataDocumentSupported),
		Oidc:                              conv.PtrToPGBool(payload.Oidc),
		Passthrough:                       conv.PtrToPGBool(payload.Passthrough),
		ID:                                issuerID,
	})
	if err != nil {
		if isGlobalRemoteSessionIssuerSlugConflict(err) {
			return nil, oops.E(oops.CodeConflict, err, "a global issuer with this slug already exists").LogError(ctx, logger)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "global remote session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update global remote session issuer").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	logGlobalMutation(ctx, logger, authCtx, "update", "issuer", updated.ID.String())

	return mv.BuildRemoteSessionIssuerView(updated), nil
}

// DeleteGlobalIssuer soft-deletes a global remote_session_issuer, blocked when
// any global clients still reference it (the operator deletes the clients
// first). Mirrors the org-scoped DeleteIssuer.
func (s *Service) DeleteGlobalIssuer(ctx context.Context, payload *adminrsgen.DeleteGlobalIssuerPayload) error {
	authCtx, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return err
	}

	issuerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Take the advisory lock BEFORE the FOR UPDATE row lock below. Tenant client
	// creation acquires the advisory lock first and then locks the issuer row via
	// its client insert's FK KEY SHARE. Acquiring the row lock first here would
	// invert that order and deadlock: this delete would hold the issuer row and
	// wait for the advisory lock while a concurrent create holds the advisory lock
	// and waits for the row. Same order on both paths (advisory, then row) avoids
	// the cycle. The advisory lock also serializes the count-then-delete against
	// every client writer, tenant and global alike, so a client cannot be inserted
	// between the count and the delete.
	if err := txRepo.LockRemoteSessionIssuerForClientBinding(ctx, issuerID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock remote session issuer for client binding").LogError(ctx, logger)
	}

	// Establish the issuer is global before counting clients or deleting, so a
	// non-global id returns NotFound rather than probing client counts. FOR
	// UPDATE also serializes this delete against a concurrent CreateGlobalClient,
	// which takes the same row lock.
	if _, err := txRepo.GetGlobalRemoteSessionIssuerByIDForUpdate(ctx, issuerID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "global remote session issuer not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "get global remote session issuer").LogError(ctx, logger)
	}

	// Count all clients as the fail-safe (a delete must never strand a live
	// client), then split out the tenant-held ones. Tenant clients on a platform
	// issuer are owned by projects and organizations; they never appear in
	// ListGlobalRemoteSessionClientsByIssuerID, so a bare "delete the clients
	// first" would point a platform admin at clients they cannot see or remove.
	// Reporting the two counts distinctly tells them which blockers are theirs
	// (the global clients) and which belong to tenants.
	clientCount, err := txRepo.CountRemoteSessionClientsByIssuerID(ctx, issuerID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "count remote session clients").LogError(ctx, logger)
	}
	if clientCount > 0 {
		tenantCount, err := txRepo.CountTenantRemoteSessionClientsByIssuerID(ctx, issuerID)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "count tenant remote session clients").LogError(ctx, logger)
		}
		globalCount := clientCount - tenantCount
		// Name only the populations that actually block, so the message never
		// tells an admin to "delete the 0 global clients". The platform catalog
		// UI shows this verbatim, and an instruction that cannot be followed
		// reads as a bug in the delete rather than a fact about the data.
		switch {
		case tenantCount == 0:
			return oops.E(oops.CodeConflict, nil, "global remote session issuer has %d active global client(s); delete them here first", globalCount).LogError(ctx, logger)
		case globalCount == 0:
			return oops.E(oops.CodeConflict, nil, "global remote session issuer has %d active tenant-owned client(s); they must be removed by their owning organizations", tenantCount).LogError(ctx, logger)
		default:
			return oops.E(oops.CodeConflict, nil, "global remote session issuer has active clients: %d global, %d tenant-owned; delete the global client(s) here, tenant-owned clients must be removed by their owning organizations", globalCount, tenantCount).LogError(ctx, logger)
		}
	}

	deleted, err := txRepo.DeleteGlobalRemoteSessionIssuer(ctx, issuerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "delete global remote session issuer").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	logGlobalMutation(ctx, logger, authCtx, "delete", "issuer", deleted.ID.String())

	return nil
}

// FetchGlobalIssuerMetadata fetches an upstream issuer's RFC 8414 metadata
// document and returns a draft suitable for CreateGlobalIssuer. Keyed by issuer
// URL, so no record need exist and nothing is persisted.
func (s *Service) FetchGlobalIssuerMetadata(ctx context.Context, payload *adminrsgen.FetchGlobalIssuerMetadataPayload) (*types.RemoteSessionIssuerDraft, error) {
	_, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}

	issuerURL := strings.TrimSpace(payload.Issuer)
	if issuerURL == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "issuer is required").LogError(ctx, logger)
	}

	if !urls.IsAbsoluteHTTP(issuerURL) {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid issuer url").LogError(ctx, logger)
	}

	doc, warnings, err := discoverIssuerMetadata(ctx, s.policy, issuerURL)
	if err != nil {
		return nil, mapDiscoveryError(ctx, logger, err, oops.CodeBadRequest)
	}

	return buildIssuerDraft(doc, issuerURL, warnings), nil
}

// RefreshGlobalIssuerMetadata re-reads an existing global issuer's RFC 8414
// metadata document and persists the discovered values, returning the updated
// issuer alongside any warnings.
//
// Like every other global mutation this records a structured-log line rather
// than an auditlogs row, since audit_log.organization_id is NOT NULL and a
// global issuer belongs to no organization.
func (s *Service) RefreshGlobalIssuerMetadata(ctx context.Context, payload *adminrsgen.RefreshGlobalIssuerMetadataPayload) (*types.RemoteSessionIssuerRefresh, error) {
	authCtx, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}

	issuerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogError(ctx, logger)
	}

	// Read outside the transaction: discovery below is an upstream HTTP call
	// under a ten-second budget and must not hold a pooled connection. The
	// update re-asserts this row's identity — for a global issuer, that both
	// scope columns are still NULL — so a row that stopped being global aborts
	// the write instead of being written through.
	existing, err := repo.New(s.db).GetGlobalRemoteSessionIssuerByID(ctx, issuerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "global remote session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get global remote session issuer").LogError(ctx, logger)
	}

	params, warnings, err := refreshIssuerMetadata(ctx, s.policy, existing)
	if err != nil {
		return nil, mapDiscoveryError(ctx, logger, err, oops.CodeGatewayError)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	updated, err := repo.New(dbtx).UpdateRemoteSessionIssuerDiscoveredMetadata(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeConflict, err, "%s", refreshConflictMessage).LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update global remote session issuer discovered metadata").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	// Recorded as "update", matching the remote_session_issuer.update audit
	// action the project and org tiers emit for the same operation. The
	// before/after state a refresh produced is visible in the row itself.
	logGlobalMutation(ctx, logger, authCtx, "update", "issuer", updated.ID.String())

	return &types.RemoteSessionIssuerRefresh{
		Issuer:            mv.BuildRemoteSessionIssuerView(updated),
		DiscoveryWarnings: warnings,
	}, nil
}

// --- Tenant issuer convergence ---

// loadPlatformMigrationPair resolves a tenant source issuer and a global target
// issuer and validates the scope ladder between them. It is the platform-admin
// counterpart of loadMigrationPair: same sequence, different partitions. The
// source is loaded from the tenant partition across every organization, which
// only this surface may do, and the target strictly from the global one.
//
// forUpdate row-locks both for the rest of the transaction so a concurrent
// moveIssuer or updateIssuer cannot rewrite the scope or the endpoint metadata
// between validation and commit. Callers that lock must already hold the
// advisory locks from lockIssuersForMigration, which order the row locks so two
// concurrent migrations of the same pair cannot deadlock.
func loadPlatformMigrationPair(ctx context.Context, r *repo.Queries, logger *slog.Logger, sourceIDRaw, targetIDRaw string, forUpdate bool) (source, target repo.RemoteSessionIssuer, err error) {
	sourceID, err := uuid.Parse(sourceIDRaw)
	if err != nil {
		return source, target, oops.E(oops.CodeBadRequest, err, "invalid source issuer id").LogError(ctx, logger)
	}
	targetID, err := uuid.Parse(targetIDRaw)
	if err != nil {
		return source, target, oops.E(oops.CodeBadRequest, err, "invalid target issuer id").LogError(ctx, logger)
	}

	if sourceID == targetID {
		return source, target, oops.E(oops.CodeBadRequest, nil, "source and target issuer must differ").LogError(ctx, logger)
	}

	if forUpdate {
		source, err = r.GetTenantRemoteSessionIssuerByIDForUpdate(ctx, sourceID)
	} else {
		source, err = r.GetTenantRemoteSessionIssuerByID(ctx, sourceID)
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The source query covers only the tenant partition, so a global issuer
			// id lands here. Say why rather than reporting a 404 for a row the admin
			// can see one page over in the platform catalog. The restriction is not
			// the scope ladder, which permits global to global: it is that the
			// preflight names the MCP servers attached to the source's clients, and
			// for a global source that list spans every organization on the platform.
			if _, globalErr := r.GetGlobalRemoteSessionIssuerByID(ctx, sourceID); globalErr == nil {
				return source, target, oops.E(oops.CodeBadRequest, nil, "source must be an organization- or project-level issuer; consolidating one global issuer onto another is not supported here").LogError(ctx, logger)
			}
			return source, target, oops.E(oops.CodeNotFound, err, "source remote session issuer not found").LogError(ctx, logger)
		}
		return source, target, oops.E(oops.CodeUnexpected, err, "get source remote session issuer").LogError(ctx, logger)
	}

	if forUpdate {
		target, err = r.GetGlobalRemoteSessionIssuerByIDForUpdate(ctx, targetID)
	} else {
		target, err = r.GetGlobalRemoteSessionIssuerByID(ctx, targetID)
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return source, target, oops.E(oops.CodeNotFound, err, "target global remote session issuer not found").LogError(ctx, logger)
		}
		return source, target, oops.E(oops.CodeUnexpected, err, "get target global remote session issuer").LogError(ctx, logger)
	}

	var scopeErr migrationScopeError
	if err := validateMigrationScope(source, target); errors.As(err, &scopeErr) {
		return source, target, oops.E(oops.CodeBadRequest, err, "%s", scopeErr.reason).LogError(ctx, logger)
	} else if err != nil {
		return source, target, oops.E(oops.CodeUnexpected, err, "validate migration scope").LogError(ctx, logger)
	}

	return source, target, nil
}

// ListGlobalIssuerConvergenceCandidates lists the tenant issuers that name the same
// upstream authorization server as a given global issuer, so a platform admin
// can see who could be consolidated onto the shared catalog entry.
func (s *Service) ListGlobalIssuerConvergenceCandidates(ctx context.Context, payload *adminrsgen.ListGlobalIssuerConvergenceCandidatesPayload) (*adminrsgen.ListIssuerConvergenceCandidatesResult, error) {
	_, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}

	targetID, err := uuid.Parse(payload.TargetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid target issuer id").LogError(ctx, logger)
	}

	limit := pageLimit(payload.Limit)
	cursor, err := parseCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, logger)
	}

	r := repo.New(s.db)

	target, err := r.GetGlobalRemoteSessionIssuerByID(ctx, targetID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "global remote session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get global remote session issuer").LogError(ctx, logger)
	}

	// A stored issuer URL that does not parse is matched only by its exact
	// spelling. Widening a lookup on a value that could not be understood risks
	// offering a candidate that names a different upstream, and the parity guard
	// compares the same two values the same way.
	issuers := []string{target.Issuer}
	if canonical, canonicalErr := parseCanonicalIssuerURL(target.Issuer); canonicalErr == nil {
		issuers = canonical.matchCandidates()
	}

	rows, err := r.ListTenantRemoteSessionIssuersByIssuerURL(ctx, repo.ListTenantRemoteSessionIssuersByIssuerURLParams{
		Issuers:    issuers,
		Cursor:     cursor,
		LimitValue: limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list tenant remote session issuers by issuer url").LogError(ctx, logger)
	}

	items := make([]*adminrsgen.IssuerConvergenceCandidate, 0, len(rows))
	for _, row := range rows {
		candidate := row.RemoteSessionIssuer
		items = append(items, &adminrsgen.IssuerConvergenceCandidate{
			Issuer: mv.BuildRemoteSessionIssuerView(candidate),
			// Falls back to the owning project's organization, so a legacy
			// project-scoped issuer that predates organization_id on this table
			// still names the tenant it belongs to.
			OrganizationID:   row.OwnerOrganizationID,
			OrganizationName: row.OrganizationName,
			ClientCount:      int(row.ClientCount),
			// Both are pure functions of the two issuer records, so the listing can
			// explain a near-miss without the per-candidate queries the full
			// preflight needs.
			EndpointMismatches: endpointMismatches(candidate, target),
			Warnings:           migrationWarnings(candidate, target),
		})
	}

	var nextCursor *string
	if len(rows) >= int(limit) {
		c := rows[len(rows)-1].RemoteSessionIssuer.ID.String()
		nextCursor = &c
	}

	return &adminrsgen.ListIssuerConvergenceCandidatesResult{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// GetGlobalIssuerMigratePreflight reports what consolidating a tenant issuer
// onto a global one would do, and every blocker that would make it fail, so the
// confirmation dialog is authoritative before the mutation runs.
func (s *Service) GetGlobalIssuerMigratePreflight(ctx context.Context, payload *adminrsgen.GetGlobalIssuerMigratePreflightPayload) (*adminrsgen.IssuerMigratePreflight, error) {
	_, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}

	r := repo.New(s.db)

	source, target, err := loadPlatformMigrationPair(ctx, r, logger, payload.SourceID, payload.TargetID, false)
	if err != nil {
		return nil, err
	}

	preflight, err := buildMigratePreflight(ctx, r, source, target)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build remote session issuer migrate preflight").LogError(ctx, logger)
	}

	// Reported so the dialog can say that a successful migration is effectively
	// one-way: migrated clients stay tenant-owned, and DeleteGlobalIssuer refuses
	// while any tenant-owned client references the issuer, which only the owning
	// organizations can clear.
	targetTenantClients, err := r.CountTenantRemoteSessionClientsByIssuerID(ctx, target.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count tenant remote session clients on target issuer").LogError(ctx, logger)
	}

	return &adminrsgen.IssuerMigratePreflight{
		ClientCount:               int(preflight.clientCount),
		McpServerNames:            preflight.mcpServerNames,
		EndpointMismatches:        preflight.endpointMismatches,
		ConflictingMcpServerNames: preflight.conflictingMcpServerNames,
		Warnings:                  preflight.warnings,
		CanMigrate:                preflight.canMigrate(),
		TargetTenantClientCount:   int(targetTenantClients),
	}, nil
}

// MigrateToGlobalIssuer consolidates one organization's issuer onto a global one:
// every active client is re-pointed onto the target and the now-empty source is
// soft-deleted, in one transaction. Remote sessions reference the client rather
// than the issuer, so they survive the re-point untouched and no user
// re-authenticates.
//
// Re-pointing strictly precedes the soft-delete because the runtime resolution
// query filters `i.deleted IS FALSE`: a client left on a tombstoned issuer stops
// resolving. Holding both in one transaction removes the window entirely.
//
// Like every other platform-admin mutation this records a structured-log line
// rather than an auditlogs row.
func (s *Service) MigrateToGlobalIssuer(ctx context.Context, payload *adminrsgen.MigrateToGlobalIssuerPayload) (*adminrsgen.MigrateRemoteSessionIssuerResult, error) {
	authCtx, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}

	// Reject a malformed or self-referential request before opening a
	// transaction, so a rejected call never occupies a pooled connection. The
	// loaders below parse these again, which is cheap and keeps them usable on
	// their own.
	if _, err := uuid.Parse(payload.SourceID); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid source issuer id").LogError(ctx, logger)
	}
	if _, err := uuid.Parse(payload.TargetID); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid target issuer id").LogError(ctx, logger)
	}
	if payload.SourceID == payload.TargetID {
		return nil, oops.E(oops.CodeBadRequest, nil, "source and target issuer must differ").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Establish that both issuers exist and sit in the partitions this surface
	// accepts before taking any lock, so a bad id can never advisory-lock a row.
	source, target, err := loadPlatformMigrationPair(ctx, txRepo, logger, payload.SourceID, payload.TargetID, false)
	if err != nil {
		return nil, err
	}

	// Serialize against a concurrent client attach on either issuer before
	// reading the conflict set, so the set we act on cannot go stale under us.
	// Nothing in the schema enforces the one-client-per-(user_session_issuer,
	// remote_session_issuer) invariant, so this advisory lock is the only thing
	// standing between a racing attach and a duplicate binding. Taken before the
	// row locks below, matching every other writer on these tables.
	if err := lockIssuersForMigration(ctx, txRepo, source.ID, target.ID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock issuers for migration").LogError(ctx, logger)
	}

	// Re-read both issuers under a row lock and re-validate. The advisory lock
	// above only serializes writers that take it, and neither moveIssuer (which
	// rewrites project_id) nor updateIssuer (which rewrites the endpoints) does.
	source, target, err = loadPlatformMigrationPair(ctx, txRepo, logger, payload.SourceID, payload.TargetID, true)
	if err != nil {
		return nil, err
	}

	clientsMigrated, err := runIssuerMigration(ctx, txRepo, logger, source, target)
	if err != nil {
		return nil, err
	}

	// The source now has no active clients, so the delete guard that the
	// tenant-facing deletes apply is satisfied by construction.
	deleted, err := txRepo.DeleteTenantRemoteSessionIssuer(ctx, source.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "soft-delete migrated remote session issuer").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	// A legacy project-scoped issuer carries no organization_id of its own, so
	// resolve it from the owning project rather than logging a blank tenant for
	// the very rows convergence most often touches.
	affectedOrganizationID := conv.FromPGTextOrEmpty[string](deleted.OrganizationID)
	if affectedOrganizationID == "" && deleted.ProjectID.Valid {
		if orgID, err := repo.New(s.db).GetProjectOrganizationID(ctx, deleted.ProjectID.UUID); err != nil {
			// The migration has already committed; an unresolvable owner degrades
			// the log rather than the outcome.
			logger.WarnContext(ctx, "could not resolve owning organization for migrated issuer", attr.SlogError(err))
		} else {
			affectedOrganizationID = orgID
		}
	}

	// This log line is the only durable record of the operation, and unlike every
	// other global mutation it rewrote a specific organization's rows. Carry
	// enough to answer "what did we do to whom, and how much moved" without
	// having to resolve the now soft-deleted source row: the affected
	// organization, the target that absorbed the clients, and the count.
	logGlobalMutation(ctx, logger.With(
		attr.SlogOrganizationID(affectedOrganizationID),
		attr.SlogRemoteSessionIssuerID(target.ID.String()),
		attr.SlogRemoteSessionClientMigratedCount(clientsMigrated),
	), authCtx, "migrate", "issuer", deleted.ID.String())

	return &adminrsgen.MigrateRemoteSessionIssuerResult{
		Issuer:          mv.BuildRemoteSessionIssuerView(target),
		ClientsMigrated: int(clientsMigrated),
		SourceDeleted:   true,
	}, nil
}

// --- Global clients ---

// CreateGlobalClient registers a global remote_session_client under an existing
// global issuer, reusing CreateRemoteSessionClient with NULL scoping. Global
// clients carry no user_session_issuer attachments.
func (s *Service) CreateGlobalClient(ctx context.Context, payload *adminrsgen.CreateGlobalClientPayload) (*types.RemoteSessionClient, error) {
	authCtx, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}

	issuerID, err := uuid.Parse(payload.RemoteSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_issuer_id").LogError(ctx, logger)
	}

	clientID := strings.TrimSpace(payload.ClientID)
	if clientID == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "client_id is required").LogError(ctx, logger)
	}

	// Encrypt a supplied client secret before it touches the database; an absent
	// or blank secret leaves the stored ciphertext NULL.
	var secretCiphertext pgtype.Text
	if payload.ClientSecret != nil && strings.TrimSpace(*payload.ClientSecret) != "" {
		ciphertext, encErr := s.enc.Encrypt([]byte(*payload.ClientSecret))
		if encErr != nil {
			return nil, oops.E(oops.CodeUnexpected, encErr, "encrypt client secret").LogError(ctx, logger)
		}
		secretCiphertext = conv.ToPGText(ciphertext)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Reject an issuer that isn't global so a global client can't be registered
	// against a project- or org-scoped issuer. FOR UPDATE serializes this insert
	// against a concurrent DeleteGlobalIssuer, which takes the same lock before
	// counting clients.
	if _, err := txRepo.GetGlobalRemoteSessionIssuerByIDForUpdate(ctx, issuerID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "global remote session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get global remote session issuer").LogError(ctx, logger)
	}

	created, err := txRepo.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:               uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:          pgtype.Text{String: "", Valid: false},
		RemoteSessionIssuerID:   issuerID,
		ClientID:                clientID,
		ClientSecretEncrypted:   secretCiphertext,
		ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now().UTC()),
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod: conv.PtrToPGText(payload.TokenEndpointAuthMethod),
		Scope:                   payload.Scope,
		Audience:                conv.PtrToPGText(payload.Audience),
		LegacyCallbackUrl:       false,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create global remote session client").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	logGlobalMutation(ctx, logger, authCtx, "create", "client", created.ID.String())

	return mv.BuildGlobalRemoteSessionClientView(created), nil
}

// ListGlobalClients lists the global clients registered with a global issuer.
func (s *Service) ListGlobalClients(ctx context.Context, payload *adminrsgen.ListGlobalClientsPayload) (*adminrsgen.ListRemoteSessionClientsResult, error) {
	_, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}

	issuerID, err := uuid.Parse(payload.RemoteSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_issuer_id").LogError(ctx, logger)
	}

	limit := pageLimit(payload.Limit)
	cursor, err := parseCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, logger)
	}

	rows, err := repo.New(s.db).ListGlobalRemoteSessionClientsByIssuerID(ctx, repo.ListGlobalRemoteSessionClientsByIssuerIDParams{
		RemoteSessionIssuerID: issuerID,
		Cursor:                cursor,
		LimitValue:            limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list global remote session clients").LogError(ctx, logger)
	}

	items := make([]*types.RemoteSessionClient, 0, len(rows))
	for _, row := range rows {
		items = append(items, mv.BuildGlobalRemoteSessionClientView(row))
	}

	var nextCursor *string
	if len(rows) >= int(limit) {
		c := rows[len(rows)-1].ID.String()
		nextCursor = &c
	}

	return &adminrsgen.ListRemoteSessionClientsResult{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// GetGlobalClient resolves a global client by id.
func (s *Service) GetGlobalClient(ctx context.Context, payload *adminrsgen.GetGlobalClientPayload) (*types.RemoteSessionClient, error) {
	_, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	client, err := repo.New(s.db).GetGlobalRemoteSessionClientByID(ctx, clientID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "global remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get global remote session client").LogError(ctx, logger)
	}

	return mv.BuildGlobalRemoteSessionClientView(client), nil
}

// UpdateGlobalClient patches a global client's non-issuer fields, rotating the
// client secret when supplied.
func (s *Service) UpdateGlobalClient(ctx context.Context, payload *adminrsgen.UpdateGlobalClientPayload) (*types.RemoteSessionClient, error) {
	authCtx, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	// Encrypt a rotated client secret before it touches the database; an absent
	// secret leaves the stored ciphertext untouched (narg NULL → COALESCE keeps).
	var clientSecretEncrypted pgtype.Text
	if payload.ClientSecret != nil {
		if strings.TrimSpace(*payload.ClientSecret) == "" {
			return nil, oops.E(oops.CodeBadRequest, nil, "client_secret cannot be empty").LogError(ctx, logger)
		}
		ciphertext, encErr := s.enc.Encrypt([]byte(*payload.ClientSecret))
		if encErr != nil {
			return nil, oops.E(oops.CodeUnexpected, encErr, "encrypt client secret").LogError(ctx, logger)
		}
		clientSecretEncrypted = conv.ToPGText(ciphertext)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	updated, err := repo.New(dbtx).UpdateGlobalRemoteSessionClient(ctx, repo.UpdateGlobalRemoteSessionClientParams{
		ClientSecretEncrypted:   clientSecretEncrypted,
		TokenEndpointAuthMethod: conv.PtrToPGText(payload.TokenEndpointAuthMethod),
		Scope:                   payload.Scope,
		Audience:                conv.PtrToPGText(payload.Audience),
		ID:                      clientID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "global remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update global remote session client").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	logGlobalMutation(ctx, logger, authCtx, "update", "client", updated.ID.String())

	return mv.BuildGlobalRemoteSessionClientView(updated), nil
}

// DeleteGlobalClient soft-deletes a global client and cascades the
// remote_sessions minted against it.
func (s *Service) DeleteGlobalClient(ctx context.Context, payload *adminrsgen.DeleteGlobalClientPayload) error {
	authCtx, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return err
	}

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	deleted, err := txRepo.DeleteGlobalRemoteSessionClient(ctx, clientID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "delete global remote session client").LogError(ctx, logger)
	}

	if _, err := txRepo.SoftDeleteRemoteSessionsByClientID(ctx, deleted.ID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "soft-delete dependent remote sessions").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	logGlobalMutation(ctx, logger, authCtx, "delete", "client", deleted.ID.String())

	return nil
}
