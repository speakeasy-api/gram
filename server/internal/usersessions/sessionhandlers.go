package usersessions

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/gatewayscope"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/sessionfilter"
)

// loadUpstreamsForSessions resolves the outbound leg for a page of sessions in
// one query, keyed by the (subject_urn, user_session_issuer_id) pair both
// tables carry. One round trip for the page rather than one per row: a page of
// 50 sessions would otherwise be 51 queries.
//
// Distinct pairs are collected first because a subject commonly holds several
// sessions against the same issuer — one per MCP client — and every one of them
// shares the same upstream tokens.
func (s *Service) loadUpstreamsForSessions(ctx context.Context, projectID uuid.UUID, organizationID string, rows []repo.ListUserSessionsByProjectIDRow) (map[mv.UpstreamKey][]*types.UserSessionUpstream, error) {
	if len(rows) == 0 {
		return map[mv.UpstreamKey][]*types.UserSessionUpstream{}, nil
	}

	seen := make(map[mv.UpstreamKey]struct{}, len(rows))
	subjectURNs := make([]string, 0, len(rows))
	issuerIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		key := mv.UpstreamKey{
			SubjectURN:          row.SubjectUrn.String(),
			UserSessionIssuerID: row.UserSessionIssuerID,
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		subjectURNs = append(subjectURNs, key.SubjectURN)
		issuerIDs = append(issuerIDs, key.UserSessionIssuerID)
	}

	upstreamRows, err := repo.New(s.db).ListRemoteSessionUpstreamsForSubjects(ctx, repo.ListRemoteSessionUpstreamsForSubjectsParams{
		SubjectUrns: subjectURNs,
		IssuerIds:   issuerIDs,
		// Scopes on the user_session_issuer's tenancy, with org-level clients
		// additionally pinned to the caller's organization so a stray
		// cross-tenant binding row can never surface a foreign credential.
		ProjectID:      projectID,
		OrganizationID: organizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list remote session upstreams").LogError(ctx, s.logger)
	}

	return mv.BuildUserSessionUpstreamIndex(upstreamRows), nil
}

// loadWorkloadLabelsForSessions resolves the workloads behind a page of
// sessions in one query. Workloads are keyed on (workload issuer, subject)
// parsed through the session subject, never on the subject alone.
func (s *Service) loadWorkloadLabelsForSessions(ctx context.Context, projectID uuid.UUID, organizationID string, rows []repo.ListUserSessionsByProjectIDRow) (map[mv.WorkloadKey]*types.UserSessionWorkload, error) {
	seen := make(map[mv.WorkloadKey]struct{})
	issuerIDs := make([]uuid.UUID, 0)
	subjects := make([]string, 0)
	for _, row := range rows {
		key, ok := mv.WorkloadKeyForSession(row.SubjectUrn)
		if !ok {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		issuerIDs = append(issuerIDs, key.WorkloadIssuerID)
		subjects = append(subjects, key.ExternalSubject)
	}
	if len(issuerIDs) == 0 {
		return map[mv.WorkloadKey]*types.UserSessionWorkload{}, nil
	}

	queries := repo.New(s.db)
	labelRows, err := queries.ListWorkloadSessionLabels(ctx, repo.ListWorkloadSessionLabelsParams{
		WorkloadIssuerIds: issuerIDs,
		Subjects:          subjects,
		OrganizationID:    organizationID,
		ProjectID:         projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list workload session labels").LogError(ctx, s.logger)
	}

	admissionRows, err := queries.ListWorkloadSessionAdmissions(ctx, repo.ListWorkloadSessionAdmissionsParams{
		WorkloadIssuerIds: issuerIDs,
		Subjects:          subjects,
		OrganizationID:    organizationID,
		ProjectID:         projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list workload session admissions").LogError(ctx, s.logger)
	}

	return mv.BuildUserSessionWorkloadIndex(labelRows, admissionRows), nil
}

// Lists issued sessions; keyset paginated by id (descending).
// refresh_token_hash is excluded from the projection.
func (s *Service) ListUserSessions(ctx context.Context, payload *gen.ListUserSessionsPayload) (*gen.ListUserSessionsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	limit := pageLimit(payload.Limit)
	cursor, err := parseCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, s.logger)
	}

	filter, err := sessionfilter.Parse(sessionfilter.Input{
		Status:                     payload.Status,
		SubjectURN:                 payload.SubjectUrn,
		IssuerID:                   payload.UserSessionIssuerID,
		ClientID:                   payload.ClientID,
		Search:                     payload.Search,
		Identities:                 payload.Identities,
		ExcludeIdentities:          payload.ExcludeIdentities,
		IdentityTypes:              payload.IdentityTypes,
		ExcludeIdentityTypes:       payload.ExcludeIdentityTypes,
		Servers:                    payload.Servers,
		ExcludeServers:             payload.ExcludeServers,
		Clients:                    payload.Clients,
		ExcludeClients:             payload.ExcludeClients,
		ClientRegistrations:        payload.ClientRegistrations,
		ExcludeClientRegistrations: payload.ExcludeClientRegistrations,
		LastUsedWithin:             payload.LastUsedWithin,
		LastUsedOlderThan:          payload.LastUsedOlderThan,
		LastUsedNever:              conv.PtrValOr(payload.LastUsedNever, false),
	})
	if err != nil {
		return nil, fmt.Errorf("parse session filter: %w", err)
	}
	if payload.MetaMcpServerID != nil {
		if filter.Gateway, err = gatewayscope.Resolve(ctx, s.db, *authCtx.ProjectID, *payload.MetaMcpServerID); err != nil {
			return nil, fmt.Errorf("scope sessions to gateway: %w", err)
		}
	}

	agentSubjectURNs, err := s.readableAgentSubjectsNamed(ctx, filter.Term)
	if err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListUserSessionsByProjectID(ctx, filter.ListParams(*authCtx.ProjectID, authCtx.ActiveOrganizationID, agentSubjectURNs, cursor, limit))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list user sessions").LogError(ctx, s.logger)
	}

	upstreams, err := s.loadUpstreamsForSessions(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, rows)
	if err != nil {
		return nil, err
	}

	workloads, err := s.loadWorkloadLabelsForSessions(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, rows)
	if err != nil {
		return nil, err
	}

	items := mv.BuildUserSessionListView(rows, upstreams, workloads)

	var nextCursor *string
	if len(rows) >= int(limit) {
		c := rows[len(rows)-1].ID.String()
		nextCursor = &c
	}

	return &gen.ListUserSessionsResult{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// readableAgentSubjectsNamed lists the subjects of the agents the caller may
// read whose names contain term. A blank term names none.
func (s *Service) readableAgentSubjectsNamed(ctx context.Context, term string) ([]string, error) {
	if term == "" {
		return []string{}, nil
	}
	agentIDs, err := s.agents.ReadableAgentIDsNamed(ctx, s.db, term)
	if _, ok := errors.AsType[*oops.ShareableError](err); ok {
		return nil, fmt.Errorf("search agent names: %w", err)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "search agent names").LogError(ctx, s.logger)
	}
	subjects := make([]string, 0, len(agentIDs))
	for _, id := range agentIDs {
		subjects = append(subjects, urn.NewAgentSubject(id).String())
	}
	return subjects, nil
}

// QueryFacets counts each filter field's values over the sessions the list
// would show for the same filters.
func (s *Service) QueryFacets(ctx context.Context, payload *gen.QueryFacetsPayload) (*gen.UserSessionFilterFacets, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	filter, err := sessionfilter.Parse(sessionfilter.Input{
		Status:                     payload.Status,
		SubjectURN:                 payload.SubjectUrn,
		IssuerID:                   payload.UserSessionIssuerID,
		ClientID:                   payload.ClientID,
		Search:                     payload.Search,
		Identities:                 payload.Identities,
		ExcludeIdentities:          payload.ExcludeIdentities,
		IdentityTypes:              payload.IdentityTypes,
		ExcludeIdentityTypes:       payload.ExcludeIdentityTypes,
		Servers:                    payload.Servers,
		ExcludeServers:             payload.ExcludeServers,
		Clients:                    payload.Clients,
		ExcludeClients:             payload.ExcludeClients,
		ClientRegistrations:        payload.ClientRegistrations,
		ExcludeClientRegistrations: payload.ExcludeClientRegistrations,
		LastUsedWithin:             payload.LastUsedWithin,
		LastUsedOlderThan:          payload.LastUsedOlderThan,
		LastUsedNever:              conv.PtrValOr(payload.LastUsedNever, false),
	})
	if err != nil {
		return nil, fmt.Errorf("parse session filter: %w", err)
	}
	if payload.MetaMcpServerID != nil {
		if filter.Gateway, err = gatewayscope.Resolve(ctx, s.db, *authCtx.ProjectID, *payload.MetaMcpServerID); err != nil {
			return nil, fmt.Errorf("scope sessions to gateway: %w", err)
		}
	}
	fieldSearch, err := sessionfilter.ParseFacetSearch(payload.Field, payload.FieldSearch)
	if err != nil {
		return nil, fmt.Errorf("parse facet search: %w", err)
	}

	agentSubjectURNs, err := s.readableAgentSubjectsNamed(ctx, filter.Term)
	if err != nil {
		return nil, err
	}
	if fieldSearch.Field == sessionfilter.FieldIdentity {
		if fieldSearch.AgentSubjectURNs, err = s.readableAgentSubjectsNamed(ctx, fieldSearch.Term); err != nil {
			return nil, err
		}
	}

	rows, err := repo.New(s.db).ListUserSessionFilterFacets(ctx, filter.FacetParams(*authCtx.ProjectID, authCtx.ActiveOrganizationID, agentSubjectURNs, fieldSearch))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count user session filter values").LogError(ctx, s.logger)
	}

	return buildUserSessionFilterFacets(rows, filter), nil
}

// ListFacets returns available facet values for the user session feed filters.
func (s *Service) ListFacets(ctx context.Context, _ *gen.ListFacetsPayload) (*gen.ListUserSessionFacetsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	q := repo.New(s.db)
	clients, err := q.ListUserSessionClientFacets(ctx, repo.ListUserSessionClientFacetsParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list client facets").LogError(ctx, s.logger)
	}
	users, err := q.ListUserSessionUserFacets(ctx, repo.ListUserSessionUserFacetsParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list user facets").LogError(ctx, s.logger)
	}
	servers, err := q.ListUserSessionServerFacets(ctx, repo.ListUserSessionServerFacetsParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list server facets").LogError(ctx, s.logger)
	}

	clientOpts := make([]*gen.UserSessionFacetOption, len(clients))
	for i, r := range clients {
		clientOpts[i] = &gen.UserSessionFacetOption{Value: r.Value, DisplayName: r.DisplayName, Count: r.Count}
	}
	userOpts := make([]*gen.UserSessionFacetOption, len(users))
	for i, r := range users {
		userOpts[i] = &gen.UserSessionFacetOption{Value: r.Value, DisplayName: r.DisplayName, Count: r.Count}
	}
	serverOpts := make([]*gen.UserSessionFacetOption, len(servers))
	for i, r := range servers {
		serverOpts[i] = &gen.UserSessionFacetOption{Value: r.Value, DisplayName: r.DisplayName, Count: r.Count}
	}

	return &gen.ListUserSessionFacetsResult{
		Clients: clientOpts,
		Users:   userOpts,
		Servers: serverOpts,
	}, nil
}

// Soft-deletes the session and pushes its jti into the revocation cache
// so the access token stops validating before its TTL expires.
func (s *Service) RevokeUserSession(ctx context.Context, payload *gen.RevokeUserSessionPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid session id").LogError(ctx, s.logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	revoked, err := txRepo.RevokeUserSession(ctx, repo.RevokeUserSessionParams{
		ID:             id,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "user session not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "revoke user session").LogError(ctx, logger)
	}

	if err := s.audit.LogUserSessionRevoke(ctx, dbtx, audit.LogUserSessionRevokeEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		UserSessionURN:   urn.NewUserSession(revoked.ID),
		Principal:        revoked.SubjectUrn,
		Jti:              revoked.Jti,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log user session revocation").LogError(ctx, logger)
	}

	// Tombstone the subject's upstream grants in the same transaction; the
	// RFC 7009 pushes wait until it commits.
	revokedUpstream, err := s.revoker.SoftDeleteSubjectSessions(ctx, dbtx, revoked.SubjectUrn, revoked.UserSessionIssuerID, *authCtx.ProjectID, authCtx.ActiveOrganizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "revoke upstream remote sessions").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	// Push the jti into the revocation cache after the DB commit so a cached
	// jti always corresponds to a soft-deleted row. Cache-write failure is
	// surfaced as Unexpected — the row is gone but the access token would keep
	// validating until expiry, which is the case the cache exists to prevent.
	pushErr := s.chatSessions.RevokeToken(ctx, revoked.Jti)

	// Strictly after the jti push, and attempted even when it failed. The
	// upstream call is a synchronous round trip to a third party, so running it
	// first would hold Speakeasy's own access token valid for the length of someone
	// else's timeout — trading a prompt local revocation for a best-effort
	// remote one. The two are independent controls, so a cache outage must not
	// also cost the upstream revocation.
	s.revoker.RevokeAllDetached(ctx, revokedUpstream)

	if pushErr != nil {
		return oops.E(oops.CodeUnexpected, pushErr, "push jti into revocation cache").LogError(ctx, logger)
	}

	return nil
}

// buildUserSessionFilterFacets groups facet rows by field. selected lists each
// field's chosen values; one the counts did not return is appended with a zero
// count and no label, so a value supplied in a link is never named.
func buildUserSessionFilterFacets(rows []repo.ListUserSessionFilterFacetsRow, filter sessionfilter.Filter) *gen.UserSessionFilterFacets {
	var unattributed *int64
	facets := map[string]*gen.UserSessionFilterFacet{}
	for _, field := range []string{"identity", "identity_type", "mcp_server", "client", "client_registration"} {
		facets[field] = &gen.UserSessionFilterFacet{Values: []*gen.UserSessionFilterFacetValue{}, DistinctCount: 0}
	}
	for _, row := range rows {
		if row.Field == sessionfilter.FieldUnattributed && filter.Gateway.ResourceIdentity.Valid {
			unattributed = new(row.Count)
		}
		facet, ok := facets[row.Field]
		if !ok {
			continue
		}
		facet.DistinctCount = row.DistinctCount
		facet.Values = append(facet.Values, &gen.UserSessionFilterFacetValue{
			Value:         row.Value,
			DisplayName:   conv.PtrEmpty(row.Label),
			Count:         row.Count,
			MatchesSearch: row.MatchesSearch,
		})
	}
	for field, facet := range facets {
		for _, value := range filter.Selected(field) {
			if !slices.ContainsFunc(facet.Values, func(v *gen.UserSessionFilterFacetValue) bool { return v.Value == value }) {
				facet.Values = append(facet.Values, &gen.UserSessionFilterFacetValue{Value: value, DisplayName: nil, Count: 0, MatchesSearch: false})
			}
		}
	}
	return &gen.UserSessionFilterFacets{
		Identity:           facets["identity"],
		IdentityType:       facets["identity_type"],
		McpServer:          facets["mcp_server"],
		Client:             facets["client"],
		ClientRegistration: facets["client_registration"],
		UnattributedCount:  unattributed,
	}
}
