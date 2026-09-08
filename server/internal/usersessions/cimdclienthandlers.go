// Management API handlers for the userSessionIssuersCimdClients service:
// the read-only CIMD preset catalog Gram curates, and CRUD over the custom
// document URLs an individual issuer additionally admits.
//
// Together with the client_id_metadata_admission_mode field on the
// userSessionIssuers endpoints, these are the configuration surface for CIMD
// admission control, consumed by the dashboard's Authentication section.

package usersessions

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/user_session_issuers_cimd_clients"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const (
	// verifyRatePerMin / verifyRateBurst bound VerifyURL per project. Sized
	// for a human clicking Verify while configuring an issuer — a handful of
	// URLs in a sitting — not for enumeration. Matches the shape of the
	// externalCredentials verify limiter.
	verifyRatePerMin = 10
	verifyRateBurst  = 5
)

// Lists Gram's curated CIMD preset catalog. The catalog is a compile-time
// constant with no tenant data, but the endpoint still carries the standard
// project auth: it is only meaningful next to the issuer configuration it
// accompanies, and an unauthenticated surface would be a needless addition
// to the public attack area.
func (s *Service) ListPresets(ctx context.Context, payload *gen.ListPresetsPayload) (*gen.ListCimdClientPresetsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	presets := admission.Catalog()
	items := make([]*types.CimdClientPreset, 0, len(presets))
	for _, preset := range presets {
		items = append(items, &types.CimdClientPreset{
			VendorKey:           preset.VendorKey,
			DisplayName:         preset.DisplayName,
			ClientIDMetadataURI: preset.URL,
			IsPattern:           preset.IsPattern(),
			Enabled:             preset.Enabled,
		})
	}

	return &gen.ListCimdClientPresetsResult{Items: items}, nil
}

// Probes a CIMD document URL without persisting anything, so an operator can
// confirm a URL works before adding it. Create deliberately performs no fetch
// of its own, so this is the only place the document is checked before it
// reaches an authorization request.
//
// Every probe outcome — including a total failure to reach the host — is a
// successful call returning verified=false. An error return means the request
// itself was bad or the caller is not authorized, never that the document is.
//
// Takes the write scope rather than read: this is a pre-flight for a write,
// it makes Gram issue an outbound request to a caller-chosen URL, and gating
// it lower than the create it precedes would be an odd seam.
func (s *Service) VerifyURL(ctx context.Context, payload *gen.VerifyURLPayload) (*gen.VerifyCimdURLResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	// Bound per project. Fail OPEN on a limiter outage: a Redis blip must
	// not stop an operator configuring an issuer, and the downside of an
	// unbounded window is bounded by how long the store stays down.
	switch res, limitErr := s.verifyLimiter.Allow(ctx, authCtx.ProjectID.String()); {
	case limitErr != nil:
		s.logger.WarnContext(ctx, "cimd verify rate limiter unavailable, allowing", attr.SlogError(limitErr))
	case !res.Allowed:
		return nil, oops.E(oops.CodeRateLimitExceeded, nil, "verify rate limit exceeded, try again shortly")
	}

	// Inspect applies the §3 syntax rules itself and reports a violation as
	// the invalid_url outcome, so unlike create there is no separate hard
	// fail here: a syntactically dead URL is a probe result the operator
	// asked for, not a malformed request.
	result := s.cimdResolver.Inspect(ctx, payload.ClientIDMetadataURI)

	// This is the only endpoint that makes Gram issue an outbound request to
	// a caller-chosen host, and the resolver's own log line carries the
	// origin but no tenant. Stamp the project so egress can be attributed if
	// the endpoint is ever abused as a scanner.
	s.logger.InfoContext(ctx, "cimd url verified",
		attr.SlogProjectID(authCtx.ProjectID.String()),
		attr.SlogOutcome(string(result.Outcome)),
	)

	view := &gen.VerifyCimdURLResult{
		Verified:   result.Outcome == cimd.OutcomeValid,
		Outcome:    string(result.Outcome),
		HTTPStatus: nil,
		Reason:     nil,
		Detail:     result.Detail,
		ClientName: nil,
	}
	view.HTTPStatus = conv.PtrEmpty(result.HTTPStatus)
	view.Reason = conv.PtrEmpty(result.Reason)
	if result.Document != nil {
		view.ClientName = conv.PtrEmpty(result.Document.ClientName)
	}

	return view, nil
}

// Allows an additional CIMD document URL on an issuer.
func (s *Service) CreateUserSessionIssuerCimdClient(ctx context.Context, payload *gen.CreateUserSessionIssuerCimdClientPayload) (*gen.CreateUserSessionIssuerCimdClientResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	issuerID, err := uuid.Parse(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer id").LogError(ctx, s.logger)
	}

	logger := s.logger.With(
		attr.SlogProjectID(authCtx.ProjectID.String()),
		attr.SlogUserSessionIssuerID(issuerID.String()),
	)

	// Hard-fail on syntax. A URL that cannot be a valid CIMD client_id can
	// never be presented as one, so storing it would only ever produce a
	// confusing dead entry in the issuer's policy. This is the same §3
	// check the authorize path applies, and it includes the length cap.
	if _, err := cimd.ValidateClientIDURL(payload.ClientIDMetadataURI); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid client_id_metadata_uri: %s", err.Error()).LogError(ctx, logger)
	}

	// No document fetch here, deliberately. It never blocked the write, so
	// it bought an advisory string rather than protection, at the cost of an
	// outbound request (up to the resolver's 10s timeout) on every add. The
	// warning was also ephemeral — nothing persists it, so it vanished on
	// the next read and could not be acted on later. VerifyURL now answers
	// the same question on demand, in full, and the document is re-fetched
	// and re-validated on every authorization regardless, so skipping the
	// probe here cannot smuggle a bad document into the policy.

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	issuer, err := txRepo.GetUserSessionIssuerByID(ctx, repo.GetUserSessionIssuerByIDParams{
		ID:             issuerID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get user session issuer").LogError(ctx, logger)
	}

	row, err := txRepo.CreateUserSessionIssuerCimdClient(ctx, repo.CreateUserSessionIssuerCimdClientParams{
		ProjectID:           *authCtx.ProjectID,
		OrganizationID:      authCtx.ActiveOrganizationID,
		UserSessionIssuerID: issuerID,
		ClientIDMetadataUri: payload.ClientIDMetadataURI,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create user session issuer cimd client").LogError(ctx, logger)
	}

	projectID := ""
	if row.ProjectID.Valid {
		projectID = row.ProjectID.UUID.String()
	}

	view := &types.UserSessionIssuerCimdClient{
		ID:                  row.ID.String(),
		ProjectID:           projectID,
		UserSessionIssuerID: row.UserSessionIssuerID.String(),
		ClientIDMetadataURI: row.ClientIDMetadataUri,
		CreatedAt:           row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:           row.UpdatedAt.Time.Format(time.RFC3339),
	}

	// Only a real new grant is audited. The query is idempotent, so a repeat
	// add returns the existing row; recording an "add" for it would put a
	// second grant of the same URL in the trail at a time when nothing was
	// granted, and an operator reading that history would see a change that
	// never happened.
	if row.Inserted {
		if err := s.audit.LogUserSessionIssuerCimdClientAdd(ctx, dbtx, audit.LogUserSessionIssuerCimdClientAddEvent{
			OrganizationID:        authCtx.ActiveOrganizationID,
			ProjectID:             *authCtx.ProjectID,
			Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:      authCtx.Email,
			ActorSlug:             nil,
			CimdClientURN:         urn.NewUserSessionIssuerCimdClient(row.ID),
			ClientIDMetadataURI:   row.ClientIDMetadataUri,
			CimdClientSnapshot:    view,
			UserSessionIssuerURN:  urn.NewUserSessionIssuer(issuer.ID),
			UserSessionIssuerSlug: issuer.Slug,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log user session issuer cimd client add").LogError(ctx, logger)
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return &gen.CreateUserSessionIssuerCimdClientResult{
		Client: view,
	}, nil
}

// Lists an issuer's custom CIMD URLs; keyset paginated by id (descending).
func (s *Service) ListUserSessionIssuerCimdClients(ctx context.Context, payload *gen.ListUserSessionIssuerCimdClientsPayload) (*gen.ListUserSessionIssuerCimdClientsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	issuerID, err := uuid.Parse(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer id").LogError(ctx, s.logger)
	}

	var cursor uuid.NullUUID
	if payload.Cursor != nil {
		parsed, err := uuid.Parse(*payload.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, s.logger)
		}
		cursor = uuid.NullUUID{UUID: parsed, Valid: true}
	}

	limit := pageLimit(payload.Limit)
	rows, err := repo.New(s.db).ListUserSessionIssuerCimdClientsByIssuerID(ctx, repo.ListUserSessionIssuerCimdClientsByIssuerIDParams{
		ProjectID:           *authCtx.ProjectID,
		OrganizationID:      authCtx.ActiveOrganizationID,
		UserSessionIssuerID: issuerID,
		Cursor:              cursor,
		LimitValue:          limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list user session issuer cimd clients").LogError(ctx, s.logger)
	}

	items := make([]*types.UserSessionIssuerCimdClient, 0, len(rows))
	for _, row := range rows {
		items = append(items, userSessionIssuerCimdClientView(row))
	}

	var nextCursor *string
	if len(rows) == int(limit) {
		nextCursor = conv.PtrEmpty(rows[len(rows)-1].ID.String())
	}

	return &gen.ListUserSessionIssuerCimdClientsResult{Items: items, NextCursor: nextCursor}, nil
}

// Gets a single custom CIMD URL entry by id.
func (s *Service) GetUserSessionIssuerCimdClient(ctx context.Context, payload *gen.GetUserSessionIssuerCimdClientPayload) (*types.UserSessionIssuerCimdClient, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid id").LogError(ctx, s.logger)
	}

	row, err := repo.New(s.db).GetUserSessionIssuerCimdClientByID(ctx, repo.GetUserSessionIssuerCimdClientByIDParams{
		ID:             id,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "user session issuer cimd client not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get user session issuer cimd client").LogError(ctx, s.logger)
	}

	return userSessionIssuerCimdClientView(row), nil
}

// Removes a custom CIMD URL from an issuer.
func (s *Service) DeleteUserSessionIssuerCimdClient(ctx context.Context, payload *gen.DeleteUserSessionIssuerCimdClientPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid id").LogError(ctx, s.logger)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	row, err := txRepo.DeleteUserSessionIssuerCimdClient(ctx, repo.DeleteUserSessionIssuerCimdClientParams{
		ID:             id,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "user session issuer cimd client not found")
		}
		return oops.E(oops.CodeUnexpected, err, "delete user session issuer cimd client").LogError(ctx, logger)
	}

	issuer, err := txRepo.GetUserSessionIssuerByID(ctx, repo.GetUserSessionIssuerByIDParams{
		ID:             row.UserSessionIssuerID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get user session issuer").LogError(ctx, logger)
	}

	if err := s.audit.LogUserSessionIssuerCimdClientRemove(ctx, dbtx, audit.LogUserSessionIssuerCimdClientRemoveEvent{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             *authCtx.ProjectID,
		Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:      authCtx.Email,
		ActorSlug:             nil,
		CimdClientURN:         urn.NewUserSessionIssuerCimdClient(row.ID),
		ClientIDMetadataURI:   row.ClientIDMetadataUri,
		CimdClientSnapshot:    userSessionIssuerCimdClientView(row),
		UserSessionIssuerURN:  urn.NewUserSessionIssuer(issuer.ID),
		UserSessionIssuerSlug: issuer.Slug,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log user session issuer cimd client remove").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return nil
}

func userSessionIssuerCimdClientView(row repo.UserSessionIssuerCimdClient) *types.UserSessionIssuerCimdClient {
	projectID := ""
	if row.ProjectID.Valid {
		projectID = row.ProjectID.UUID.String()
	}

	return &types.UserSessionIssuerCimdClient{
		ID:                  row.ID.String(),
		ProjectID:           projectID,
		UserSessionIssuerID: row.UserSessionIssuerID.String(),
		ClientIDMetadataURI: row.ClientIDMetadataUri,
		CreatedAt:           row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:           row.UpdatedAt.Time.Format(time.RFC3339),
	}
}
