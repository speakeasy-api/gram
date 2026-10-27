package workloadpolicy_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	srv "github.com/speakeasy-api/gram/server/gen/http/workload_identities/server"
	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy"
)

func listJoinTokens(t *testing.T, ctx context.Context, ti *testInstance, agentID *string, cursor *string, limit int) *gen.WorkloadDeviceJoinTokenList {
	t.Helper()
	res, err := ti.service.ListDeviceJoinTokens(ctx, &gen.ListDeviceJoinTokensPayload{
		TokenID:          nil,
		AgentID:          agentID,
		Cursor:           cursor,
		Limit:            limit,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	return res
}

func revokeJoinToken(ctx context.Context, ti *testInstance, id string) (*gen.WorkloadDeviceJoinTokenSummary, error) {
	res, err := ti.service.RevokeDeviceJoinToken(ctx, &gen.RevokeDeviceJoinTokenPayload{
		ID:               id,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	if err != nil {
		return nil, fmt.Errorf("revoke join token: %w", err)
	}
	return res, nil
}

// onlyJoinTokenID returns the id of the one token the organization holds for
// an agent in fixtures with exactly one issuance.
func onlyJoinTokenID(t *testing.T, ctx context.Context, ti *testInstance, agentID uuid.UUID) string {
	t.Helper()
	res := listJoinTokens(t, ctx, ti, new(agentID.String()), nil, 50)
	require.Len(t, res.Items, 1)
	return res.Items[0].ID
}

func TestListDeviceJoinTokens_NewestFirstAndFilteredByAgent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	first := newAgent(t, ctx, ti, "first")
	second := newAgent(t, ctx, ti, "second")

	createJoinToken(t, ctx, ti, first)
	createJoinToken(t, ctx, ti, second)
	createJoinToken(t, ctx, ti, first)

	all := listJoinTokens(t, ctx, ti, nil, nil, 50)
	require.Len(t, all.Items, 3)
	require.Nil(t, all.NextCursor)
	require.Equal(t, first.String(), all.Items[0].AgentID)
	require.Equal(t, second.String(), all.Items[1].AgentID)
	require.Equal(t, first.String(), all.Items[2].AgentID)
	for i := 1; i < len(all.Items); i++ {
		previous, err := time.Parse(time.RFC3339Nano, all.Items[i-1].CreatedAt)
		require.NoError(t, err)
		current, err := time.Parse(time.RFC3339Nano, all.Items[i].CreatedAt)
		require.NoError(t, err)
		require.False(t, previous.Before(current))
		if previous.Equal(current) {
			require.Greater(t, all.Items[i-1].ID, all.Items[i].ID)
		}
	}
	for _, item := range all.Items {
		require.Equal(t, "active", item.Status)
		require.Equal(t, ti.userID, item.CreatedByUserID)
		require.Nil(t, item.ConsumedAt)
		require.Nil(t, item.ConsumedBySubject)
		require.Nil(t, item.RevokedAt)
	}

	firstOnly := listJoinTokens(t, ctx, ti, new(first.String()), nil, 50)
	require.Len(t, firstOnly.Items, 2)
	for _, item := range firstOnly.Items {
		require.Equal(t, first.String(), item.AgentID)
	}

	none := listJoinTokens(t, ctx, ti, new(uuid.NewString()), nil, 50)
	require.Empty(t, none.Items)
}

func TestListDeviceJoinTokens_Paginates(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	agentID := newAgent(t, ctx, ti, "paged")
	for range 4 {
		createJoinToken(t, ctx, ti, agentID)
	}

	seen := map[string]bool{}
	var cursor *string
	pages := 0
	for {
		page := listJoinTokens(t, ctx, ti, nil, cursor, 2)
		pages++
		for _, item := range page.Items {
			require.False(t, seen[item.ID], "a token appeared on two pages")
			seen[item.ID] = true
		}
		if page.NextCursor == nil {
			break
		}
		cursor = page.NextCursor
	}
	require.Len(t, seen, 4)
	require.Equal(t, 2, pages, "an exact final page must not advertise another page")

	_, err := ti.service.ListDeviceJoinTokens(ctx, &gen.ListDeviceJoinTokensPayload{
		TokenID:          nil,
		AgentID:          nil,
		Cursor:           new("not-a-uuid"),
		Limit:            2,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestListDeviceJoinTokens_DerivesEachStatus(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	redeemedAgent := newAgent(t, ctx, ti, "redeemed")
	revokedAgent := newAgent(t, ctx, ti, "revoked")
	expiredAgent := newAgent(t, ctx, ti, "expired")
	activeAgent := newAgent(t, ctx, ti, "active")

	reg, err := registerDevice(ctx, ti, createJoinToken(t, ctx, ti, redeemedAgent), publicJWK(t, newDeviceKey(t).Public()))
	require.NoError(t, err)

	createJoinToken(t, ctx, ti, revokedAgent)
	_, err = revokeJoinToken(ctx, ti, onlyJoinTokenID(t, ctx, ti, revokedAgent))
	require.NoError(t, err)

	// The fixture expires every token in the organization, so the active one
	// is created after it.
	createJoinToken(t, ctx, ti, expiredAgent)
	_, err = testrepo.New(ti.conn).ExpireWorkloadDeviceJoinTokensFixture(ctx, ti.orgID)
	require.NoError(t, err)
	createJoinToken(t, ctx, ti, activeAgent)

	byAgent := map[string]*gen.WorkloadDeviceJoinTokenSummary{}
	for _, item := range listJoinTokens(t, ctx, ti, nil, nil, 50).Items {
		byAgent[item.AgentID] = item
	}
	require.Len(t, byAgent, 4)

	redeemed := byAgent[redeemedAgent.String()]
	require.Equal(t, "redeemed", redeemed.Status, "a redeemed token stays redeemed once its expiry passes")
	require.NotNil(t, redeemed.ConsumedAt)
	require.Equal(t, reg.DeviceSubject, *redeemed.ConsumedBySubject)
	require.Nil(t, redeemed.RevokedAt)

	revoked := byAgent[revokedAgent.String()]
	require.Equal(t, "revoked", revoked.Status, "a revoked token stays revoked once its expiry passes")
	require.NotNil(t, revoked.RevokedAt)
	require.Nil(t, revoked.ConsumedAt)

	require.Equal(t, "expired", byAgent[expiredAgent.String()].Status)
	require.Equal(t, "active", byAgent[activeAgent.String()].Status)
}

func TestListDeviceJoinTokens_NeverReturnsTheTokenOrItsHash(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	agentID := newAgent(t, ctx, ti, "secretive")
	token := createJoinToken(t, ctx, ti, agentID)

	// Encoded as the HTTP response body is, so the assertion covers the wire.
	encoded, err := json.Marshal(srv.NewListDeviceJoinTokensResponseBody(listJoinTokens(t, ctx, ti, nil, nil, 50)))
	require.NoError(t, err)
	require.NotContains(t, string(encoded), token)
	require.NotContains(t, string(encoded), workloadpolicy.HashDeviceJoinToken(token))

	revoked, err := revokeJoinToken(ctx, ti, onlyJoinTokenID(t, ctx, ti, agentID))
	require.NoError(t, err)
	encoded, err = json.Marshal(srv.NewRevokeDeviceJoinTokenResponseBody(revoked))
	require.NoError(t, err)
	require.NotContains(t, string(encoded), token)
	require.NotContains(t, string(encoded), workloadpolicy.HashDeviceJoinToken(token))
}

func TestListDeviceJoinTokens_RequiresWorkloadRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	createJoinToken(t, ctx, ti, newAgent(t, ctx, ti, "listed"))

	payload := &gen.ListDeviceJoinTokensPayload{TokenID: nil, AgentID: nil, Cursor: nil, Limit: 50, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}

	_, err := ti.service.ListDeviceJoinTokens(withScopes(t, ctx, ti, authz.ScopeAgentAuthorize), payload)
	requireOopsCode(t, err, oops.CodeForbidden)

	res, err := ti.service.ListDeviceJoinTokens(withScopes(t, ctx, ti, authz.ScopeWorkloadRead), payload)
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
}

// The organization tier is the only one tokens live at, so listing does not
// depend on whether the caller names a project.
func TestListDeviceJoinTokens_IsScopedToTheOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	createJoinToken(t, ctx, ti, newAgent(t, ctx, ti, "mine"))

	otherCtx, other := newTestService(t)
	createJoinToken(t, otherCtx, other, newAgent(t, otherCtx, other, "theirs"))

	require.Len(t, listJoinTokens(t, ctx, ti, nil, nil, 50).Items, 1)
	require.Len(t, listJoinTokens(t, withoutProject(t, ctx), ti, nil, nil, 50).Items, 1)
}

func TestRevokeDeviceJoinToken_StopsRegistrationAndIsAudited(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	agentID := newAgent(t, ctx, ti, "revocable")
	token := createJoinToken(t, ctx, ti, agentID)
	id := onlyJoinTokenID(t, ctx, ti, agentID)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadDeviceJoinTokenRevoke)
	require.NoError(t, err)

	revoked, err := revokeJoinToken(ctx, ti, id)
	require.NoError(t, err)
	require.Equal(t, id, revoked.ID)
	require.Equal(t, "revoked", revoked.Status)
	require.NotNil(t, revoked.RevokedAt)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadDeviceJoinTokenRevoke)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionWorkloadDeviceJoinTokenRevoke)
	require.NoError(t, err)
	require.Equal(t, id, entry.SubjectID)
	require.Equal(t, "revocable", entry.SubjectDisplay)
	require.Equal(t, ti.userID, entry.ActorID)
	snapshot, err := audittest.DecodeAuditData(entry.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, agentID.String(), snapshot["agent_id"])

	_, err = registerDevice(ctx, ti, token, publicJWK(t, newDeviceKey(t).Public()))
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestRevokeDeviceJoinToken_RefusesARedeemedToken(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	agentID := newAgent(t, ctx, ti, "already-enrolled")
	_, err := registerDevice(ctx, ti, createJoinToken(t, ctx, ti, agentID), publicJWK(t, newDeviceKey(t).Public()))
	require.NoError(t, err)

	_, err = revokeJoinToken(ctx, ti, onlyJoinTokenID(t, ctx, ti, agentID))
	requireOopsCode(t, err, oops.CodeFailedPrecondition)
	require.ErrorContains(t, err, "already redeemed")
}

func TestRevokeDeviceJoinToken_RefusesASecondRevocation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	agentID := newAgent(t, ctx, ti, "revoked-twice")
	createJoinToken(t, ctx, ti, agentID)
	id := onlyJoinTokenID(t, ctx, ti, agentID)

	_, err := revokeJoinToken(ctx, ti, id)
	require.NoError(t, err)
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadDeviceJoinTokenRevoke)
	require.NoError(t, err)

	_, err = revokeJoinToken(ctx, ti, id)
	requireOopsCode(t, err, oops.CodeFailedPrecondition)
	require.ErrorContains(t, err, "already revoked")

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadDeviceJoinTokenRevoke)
	require.NoError(t, err)
	require.Equal(t, before, after, "a refused revocation records nothing")
}

func TestRevokeDeviceJoinToken_RefusesUnknownAndForeignTokens(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := revokeJoinToken(ctx, ti, uuid.NewString())
	requireOopsCode(t, err, oops.CodeNotFound)

	otherCtx, other := newTestService(t)
	otherAgent := newAgent(t, otherCtx, other, "foreign")
	createJoinToken(t, otherCtx, other, otherAgent)
	_, err = revokeJoinToken(ctx, ti, onlyJoinTokenID(t, otherCtx, other, otherAgent))
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = revokeJoinToken(ctx, ti, "not-a-uuid")
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestRevokeDeviceJoinToken_RequiresAuthorizingTheAgent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	agentID := newAgent(t, ctx, ti, "guarded")
	createJoinToken(t, ctx, ti, agentID)
	id := onlyJoinTokenID(t, ctx, ti, agentID)

	other := asUser(t, ctx, ti.userID+"-other")

	// workload:read cannot revoke, even for the agent's owner.
	_, err := revokeJoinToken(withScopes(t, ctx, ti, authz.ScopeWorkloadRead), ti, id)
	requireOopsCode(t, err, oops.CodeForbidden)

	// Another person needs agent:authorize as well as workload:write.
	_, err = revokeJoinToken(authztest.WithExactGrants(t, other, authz.NewGrant(authz.ScopeWorkloadWrite, ti.orgID)), ti, id)
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = revokeJoinToken(authztest.WithExactGrants(t, other, authz.NewGrant(authz.ScopeAgentAuthorize, agentID.String())), ti, id)
	requireOopsCode(t, err, oops.CodeForbidden)

	revoked, err := revokeJoinToken(authztest.WithExactGrants(t, other,
		authz.NewGrant(authz.ScopeWorkloadWrite, ti.orgID),
		authz.NewGrant(authz.ScopeAgentAuthorize, agentID.String()),
	), ti, id)
	require.NoError(t, err)
	require.Equal(t, "revoked", revoked.Status)
}

func TestRevokeDeviceJoinToken_OwnerNeedsOnlyWorkloadWrite(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	agentID := newAgent(t, ctx, ti, "owned")
	createJoinToken(t, ctx, ti, agentID)

	_, err := revokeJoinToken(withScopes(t, ctx, ti, authz.ScopeWorkloadWrite), ti, onlyJoinTokenID(t, ctx, ti, agentID))
	require.NoError(t, err)
}

func TestRevokeDeviceJoinToken_RequiresAPerson(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	agentID := newAgent(t, ctx, ti, "person-bound")
	createJoinToken(t, ctx, ti, agentID)
	id := onlyJoinTokenID(t, ctx, ti, agentID)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	noPerson := *authCtx
	noPerson.UserID = ""
	_, err := revokeJoinToken(contextvalues.SetAuthContext(ctx, &noPerson), ti, id)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCreateDeviceJoinToken_ReturnsExactIssuanceID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	agentID := newAgent(t, ctx, ti, "exact-issuance")
	created, err := ti.service.CreateDeviceJoinToken(ctx, &gen.CreateDeviceJoinTokenPayload{AgentID: agentID.String(), ExpiresInSeconds: 3600})
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	createJoinToken(t, ctx, ti, agentID)
	payload := &gen.ListDeviceJoinTokensPayload{TokenID: &created.ID, AgentID: new(agentID.String()), Limit: 1}
	page, err := ti.service.ListDeviceJoinTokens(ctx, payload)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, created.ID, page.Items[0].ID)
	require.Nil(t, page.NextCursor)
	payload.AgentID = new(uuid.NewString())
	page, err = ti.service.ListDeviceJoinTokens(ctx, payload)
	require.NoError(t, err)
	require.Empty(t, page.Items)
	foreignCtx, foreign := newTestService(t)
	payload.AgentID = nil
	page, err = foreign.service.ListDeviceJoinTokens(foreignCtx, payload)
	require.NoError(t, err)
	require.Empty(t, page.Items)
	payload.TokenID = new("not-a-token-id")
	_, err = ti.service.ListDeviceJoinTokens(ctx, payload)
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestListDeviceJoinTokens_PagesByTimeWithMixedUUIDsAndTies(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	agentID := newAgent(t, ctx, ti, "mixed-order")
	namespace := uuid.New()
	v7, err := uuid.NewV7()
	require.NoError(t, err)
	ids := []string{uuid.NewSHA1(namespace, []byte("first")).String(), uuid.NewSHA1(namespace, []byte("second")).String(), v7.String()}
	sort.Strings(ids)
	newest := time.Now().UTC().Truncate(time.Microsecond)
	for i, id := range ids {
		created, err := ti.service.CreateDeviceJoinToken(ctx, &gen.CreateDeviceJoinTokenPayload{AgentID: agentID.String(), ExpiresInSeconds: 3600})
		require.NoError(t, err)
		createdAt := newest.Add(-time.Minute)
		if i == 0 {
			createdAt = newest
		}
		affected, err := testrepo.New(ti.conn).SetDeviceJoinTokenOrderingFixture(ctx, testrepo.SetDeviceJoinTokenOrderingFixtureParams{
			OrganizationID: ti.orgID, ID: uuid.MustParse(created.ID), ReplacementID: uuid.MustParse(id), CreatedAt: pgtype.Timestamptz{Time: createdAt, Valid: true},
		})
		require.NoError(t, err)
		require.EqualValues(t, 1, affected)
	}
	// The lowest ID is newest; the two older tokens share an exact timestamp.
	expected := []string{ids[0], ids[2], ids[1]}
	var cursor *string
	for i, id := range expected {
		page := listJoinTokens(t, ctx, ti, nil, cursor, 1)
		require.Len(t, page.Items, 1)
		require.Equal(t, id, page.Items[0].ID)
		if i == len(expected)-1 {
			require.Nil(t, page.NextCursor)
		} else {
			require.NotNil(t, page.NextCursor)
		}
		cursor = page.NextCursor
	}
}
