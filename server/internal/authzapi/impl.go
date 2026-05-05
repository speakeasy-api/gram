package authzapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/authz"
	srv "github.com/speakeasy-api/gram/server/gen/http/authz/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	chrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/authzapi/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	chConn driver.Conn
	auth   *auth.Auth
	authz  *authz.Engine
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, chConn driver.Conn, sessions *sessions.Manager, authzEngine *authz.Engine) *Service {
	logger = logger.With(attr.SlogComponent("authzapi"))

	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/authzapi"),
		logger: logger,
		db:     db,
		chConn: chConn,
		auth:   auth.New(logger, db, sessions, authzEngine),
		authz:  authzEngine,
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
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) ListChallenges(ctx context.Context, payload *gen.ListChallengesPayload) (*gen.ListChallengesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(authCtx.ActiveOrganizationID),
		attr.UserID(authCtx.UserID),
	)

	outcome := ""
	if payload.Outcome != nil {
		outcome = *payload.Outcome
	}
	principalURN := ""
	if payload.PrincipalUrn != nil {
		principalURN = *payload.PrincipalUrn
	}
	scopeFilter := ""
	if payload.Scope != nil {
		scopeFilter = *payload.Scope
	}
	projectID := ""
	if payload.ProjectID != nil {
		projectID = *payload.ProjectID
	}

	filters := chrepo.ChallengeListFilters{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		Outcome:        outcome,
		PrincipalURN:   principalURN,
		Scope:          scopeFilter,
		Limit:          uint64(payload.Limit),  //nolint:gosec // Goa validates 1..200
		Offset:         uint64(payload.Offset), //nolint:gosec // Goa validates >= 0
	}

	chQueries := chrepo.New(s.chConn)

	// Query CH for challenge events and count in parallel.
	challenges, err := chQueries.ListChallenges(ctx, filters)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list challenges from clickhouse").Log(ctx, s.logger)
	}

	total, err := chQueries.CountChallenges(ctx, filters)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count challenges from clickhouse").Log(ctx, s.logger)
	}

	if len(challenges) == 0 {
		return &gen.ListChallengesResult{Challenges: []*gen.AuthzChallenge{}, Total: int(total)}, nil //nolint:gosec // count fits int
	}

	// Batch-lookup resolutions from PG.
	challengeIDs := make([]string, len(challenges))
	for i, c := range challenges {
		challengeIDs[i] = c.ID
	}

	resolutions, err := repo.New(s.db).ListChallengeResolutions(ctx, repo.ListChallengeResolutionsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ChallengeIds:   challengeIDs,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list challenge resolutions").Log(ctx, s.logger)
	}
	resolutionMap := make(map[string]repo.AuthzChallengeResolution, len(resolutions))
	for _, r := range resolutions {
		resolutionMap[r.ChallengeID] = r
	}

	// Apply resolved filter post-join if requested.
	if payload.Resolved != nil {
		wantResolved := *payload.Resolved
		filtered := challenges[:0]
		for _, c := range challenges {
			_, hasResolution := resolutionMap[c.ID]
			if hasResolution == wantResolved {
				filtered = append(filtered, c)
			}
		}
		challenges = filtered
	}

	// Batch-lookup user photos from PG.
	userIDs := make([]string, 0, len(challenges))
	seen := make(map[string]bool)
	for _, c := range challenges {
		if c.UserID != nil && *c.UserID != "" && !seen[*c.UserID] {
			userIDs = append(userIDs, *c.UserID)
			seen[*c.UserID] = true
		}
	}

	type userInfo struct {
		Email    string
		PhotoURL *string
	}
	userMap := make(map[string]userInfo, len(userIDs))
	if len(userIDs) > 0 {
		users, err := usersrepo.New(s.db).GetUsersByIDs(ctx, userIDs)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to batch-fetch users for challenge enrichment", attr.SlogError(err))
		} else {
			for _, u := range users {
				var photoURL *string
				if u.PhotoUrl.Valid {
					photoURL = &u.PhotoUrl.String
				}
				info := userInfo{
					Email:    u.Email,
					PhotoURL: photoURL,
				}
				userMap[u.ID] = info
			}
		}
	}

	// Build response.
	result := make([]*gen.AuthzChallenge, 0, len(challenges))
	for _, c := range challenges {
		roleSlugs := c.RoleSlugs
		if roleSlugs == nil {
			roleSlugs = []string{}
		}

		var (
			pProjectID      *string
			pResourceKind   *string
			pResourceID     *string
			pUserEmail      *string
			pPhotoURL       *string
			pResolvedAt     *string
			pResolutionType *string
			pResolvedBy     *string
			pResolutionSlug *string
		)

		if c.ProjectID != "" {
			pProjectID = &c.ProjectID
		}
		if c.ResourceKind != "" {
			pResourceKind = &c.ResourceKind
		}
		if c.ResourceID != "" {
			pResourceID = &c.ResourceID
		}

		// Enrich with user data.
		if c.UserID != nil {
			if info, ok := userMap[*c.UserID]; ok {
				pUserEmail = &info.Email
				pPhotoURL = info.PhotoURL
			}
		}
		// Fall back to CH email if PG lookup didn't have it.
		if pUserEmail == nil && c.UserEmail != nil && *c.UserEmail != "" {
			pUserEmail = c.UserEmail
		}

		// Enrich with resolution data.
		if r, ok := resolutionMap[c.ID]; ok {
			resolvedAt := r.CreatedAt.Time.Format(time.RFC3339)
			pResolvedAt = &resolvedAt
			pResolutionType = &r.ResolutionType
			pResolvedBy = &r.ResolvedBy
			if r.RoleSlug.Valid {
				pResolutionSlug = &r.RoleSlug.String
			}
		}

		result = append(result, &gen.AuthzChallenge{
			ID:                  c.ID,
			Timestamp:           c.Timestamp,
			OrganizationID:      c.OrganizationID,
			ProjectID:           pProjectID,
			PrincipalUrn:        c.PrincipalURN,
			PrincipalType:       c.PrincipalType,
			UserEmail:           pUserEmail,
			PhotoURL:            pPhotoURL,
			Operation:           c.Operation,
			Outcome:             c.Outcome,
			Reason:              c.Reason,
			Scope:               c.Scope,
			ResourceKind:        pResourceKind,
			ResourceID:          pResourceID,
			RoleSlugs:           roleSlugs,
			EvaluatedGrantCount: int(c.EvaluatedGrantCount),
			MatchedGrantCount:   int(c.MatchedGrantCount), //nolint:gosec // small number
			ResolvedAt:          pResolvedAt,
			ResolutionType:      pResolutionType,
			ResolvedBy:          pResolvedBy,
			ResolutionRoleSlug:  pResolutionSlug,
		})
	}

	return &gen.ListChallengesResult{
		Challenges: result,
		Total:      int(total), //nolint:gosec // count fits int
	}, nil
}

func (s *Service) ResolveChallenge(ctx context.Context, payload *gen.ResolveChallengePayload) (*gen.ChallengeResolution, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(authCtx.ActiveOrganizationID),
		attr.UserID(authCtx.UserID),
	)

	// Validate: role_assigned requires role_slug.
	if payload.ResolutionType == "role_assigned" && (payload.RoleSlug == nil || *payload.RoleSlug == "") {
		return nil, oops.E(oops.CodeBadRequest, nil, "role_slug is required when resolution_type is role_assigned").Log(ctx, s.logger)
	}
	if payload.ResolutionType == "dismissed" && payload.RoleSlug != nil && *payload.RoleSlug != "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "role_slug must be empty when resolution_type is dismissed").Log(ctx, s.logger)
	}

	resolvedBy := fmt.Sprintf("user:%s", authCtx.UserID)

	resourceKind := ""
	if payload.ResourceKind != nil {
		resourceKind = *payload.ResourceKind
	}
	resourceID := ""
	if payload.ResourceID != nil {
		resourceID = *payload.ResourceID
	}

	row, err := repo.New(s.db).InsertChallengeResolution(ctx, repo.InsertChallengeResolutionParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ChallengeID:    payload.ChallengeID,
		PrincipalUrn:   payload.PrincipalUrn,
		Scope:          payload.Scope,
		ResourceKind:   resourceKind,
		ResourceID:     resourceID,
		ResolutionType: payload.ResolutionType,
		RoleSlug:       conv.PtrToPGText(payload.RoleSlug),
		ResolvedBy:     resolvedBy,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "challenge already resolved").Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "insert challenge resolution").Log(ctx, s.logger)
	}

	var (
		pResResourceKind *string
		pResResourceID   *string
		pResRoleSlug     *string
	)
	if row.ResourceKind != "" {
		pResResourceKind = &row.ResourceKind
	}
	if row.ResourceID != "" {
		pResResourceID = &row.ResourceID
	}
	if row.RoleSlug.Valid {
		pResRoleSlug = &row.RoleSlug.String
	}

	return &gen.ChallengeResolution{
		ID:             row.ID.String(),
		OrganizationID: row.OrganizationID,
		ChallengeID:    row.ChallengeID,
		PrincipalUrn:   row.PrincipalUrn,
		Scope:          row.Scope,
		ResourceKind:   pResResourceKind,
		ResourceID:     pResResourceID,
		ResolutionType: row.ResolutionType,
		RoleSlug:       pResRoleSlug,
		ResolvedBy:     row.ResolvedBy,
		CreatedAt:      row.CreatedAt.Time.Format(time.RFC3339),
	}, nil
}
