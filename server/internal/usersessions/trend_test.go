package usersessions_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// trendEpoch anchors every fixed time in these tests: a UTC midnight.
var trendEpoch = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

func at(hours, minutes int) time.Time {
	return trendEpoch.Add(time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute)
}

// trendRow is one session row placed at fixed times.
type trendRow struct {
	identity    string
	selection   []byte
	refreshable bool
	credential  uuid.NullUUID
	// selfRoot stamps the row's own id as its credential, as some roots carry.
	selfRoot bool
	created  time.Time
	deleted  *time.Time
	// deadline is the authorization deadline; expires is the access token's.
	deadline, expires time.Time
}

func seedTrendRow(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID, row trendRow) uuid.UUID {
	t.Helper()

	hash := pgtype.Text{String: "", Valid: false}
	if row.refreshable {
		raw := make([]byte, 32)
		_, err := rand.Read(raw)
		require.NoError(t, err)
		hash = conv.ToPGText(base64.RawURLEncoding.EncodeToString(raw))
	}
	created, err := repo.New(ti.conn).CreateUserSession(ctx, repo.CreateUserSessionParams{
		UserSessionIssuerID:    issuerID,
		UserSessionClientID:    uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		SubjectUrn:             urn.NewUserSubject("trend-" + uuid.NewString()),
		AuthorizerUserID:       pgtype.Text{String: "", Valid: false},
		DelegatedGrants:        nil,
		DelegatedGrantsVersion: pgtype.Int4{Int32: 0, Valid: false},
		Jti:                    "jti-" + uuid.NewString(),
		RefreshTokenHash:       hash,
		RefreshExpiresAt:       pgtype.Timestamptz{Time: row.deadline, InfinityModifier: 0, Valid: true},
		ExpiresAt:              pgtype.Timestamptz{Time: row.expires, InfinityModifier: 0, Valid: true},
		ToolSelection:          row.selection,
		Resource:               pgtype.Text{String: "", Valid: false},
		ResourceIdentity:       conv.ToPGTextEmpty(row.identity),
		CredentialID:           row.credential,
		SignInID:               uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		SignInUserID:           pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	deleted := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: 0, Valid: false}
	if row.deleted != nil {
		deleted = pgtype.Timestamptz{Time: *row.deleted, InfinityModifier: 0, Valid: true}
	}
	credential := row.credential
	if row.selfRoot {
		credential = uuid.NullUUID{UUID: created.ID, Valid: true}
	}
	n, err := repo.New(ti.conn).SetUserSessionLifetimeFixture(ctx, repo.SetUserSessionLifetimeFixtureParams{
		CreatedAt:           pgtype.Timestamptz{Time: row.created, InfinityModifier: 0, Valid: true},
		DeletedAt:           deleted,
		RefreshExpiresAt:    pgtype.Timestamptz{Time: row.deadline, InfinityModifier: 0, Valid: true},
		ExpiresAt:           pgtype.Timestamptz{Time: row.expires, InfinityModifier: 0, Valid: true},
		CredentialID:        credential,
		ID:                  created.ID,
		UserSessionIssuerID: issuerID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	return created.ID
}

type bucketCounts struct{ created, revoked, active int64 }

func trendCounts(trend *gen.UserSessionTrend) []bucketCounts {
	out := make([]bucketCounts, 0, len(trend.Buckets))
	for _, b := range trend.Buckets {
		out = append(out, bucketCounts{created: b.Created, revoked: b.Revoked, active: b.Active})
	}
	return out
}

// A gateway's credentials are counted as credentials, not rows: refreshes
// continue one, a revocation before its deadline ends it, a deletion after its
// deadline is expiry, and a credential that cannot refresh ends with its token.
// Rows that are not this gateway's are left out, and those it cannot classify
// are only disclosed.
func TestGetTrend_CountsCredentialLifetimesAtFixedEdges(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuer := seedIssuer(t, ctx, ti, "trend-shared")
	gateway := seedGateway(t, ctx, ti, "Trend gateway", uuid.NullUUID{UUID: issuer, Valid: true})
	sibling := seedGateway(t, ctx, ti, "Trend sibling", uuid.NullUUID{UUID: issuer, Valid: true})
	g := gatewayIdentity(gateway)
	far := at(240, 0)
	refreshable := func(created time.Time, deleted *time.Time, deadline time.Time, credential uuid.NullUUID) trendRow {
		return trendRow{identity: g, selection: nil, refreshable: true, credential: credential, selfRoot: false, created: created, deleted: deleted, deadline: deadline, expires: created.Add(time.Hour)}
	}
	none := uuid.NullUUID{UUID: uuid.Nil, Valid: false}

	// A: issued before the window and refreshed twice inside it.
	a1 := seedTrendRow(t, ctx, ti, issuer, refreshable(at(-2, 0), new(at(1, 30)), far, none))
	lineageA := uuid.NullUUID{UUID: a1, Valid: true}
	seedTrendRow(t, ctx, ti, issuer, refreshable(at(1, 30), new(at(3, 15)), far, lineageA))
	seedTrendRow(t, ctx, ti, issuer, refreshable(at(3, 15), nil, far, lineageA))
	// B: issued exactly on an hour and revoked exactly on a later one.
	seedTrendRow(t, ctx, ti, issuer, refreshable(at(2, 0), new(at(4, 0)), far, none))
	// C: reaches its authorization deadline without being deleted.
	seedTrendRow(t, ctx, ti, issuer, refreshable(at(0, 30), nil, at(2, 30), none))
	// D: deleted after its deadline, which is expiry, not revocation.
	seedTrendRow(t, ctx, ti, issuer, refreshable(at(0, 10), new(at(1, 30)), at(1, 0), none))
	// E: cannot refresh, so it ends with its token though its stored
	// authorization deadline is later.
	seedTrendRow(t, ctx, ti, issuer, trendRow{identity: g, selection: nil, refreshable: false, credential: none, selfRoot: false, created: at(3, 0), deleted: nil, deadline: far, expires: at(3, 30)})
	// F: a root that carries its own id as its credential.
	selfRoot := refreshable(at(5, 0), new(at(5, 10)), far, none)
	selfRoot.selfRoot = true
	seedTrendRow(t, ctx, ti, issuer, selfRoot)
	// Issued exactly at the window end: outside the window.
	seedTrendRow(t, ctx, ti, issuer, refreshable(at(6, 0), nil, far, none))

	// Not counted: a sibling gateway on the same issuer, a legacy row named
	// only by its consent selection, and a row that recorded no gateway.
	seedTrendRow(t, ctx, ti, issuer, refreshable(at(2, 0), nil, far, none).withIdentity(gatewayIdentity(sibling)))
	catalog, err := json.Marshal(map[string]string{"resource": g})
	require.NoError(t, err)
	legacy := refreshable(at(1, 0), nil, far, none).withIdentity("")
	legacy.selection = catalog
	seedTrendRow(t, ctx, ti, issuer, legacy)
	seedTrendRow(t, ctx, ti, issuer, refreshable(at(1, 0), nil, far, none).withIdentity(""))
	// A row in another project naming this gateway.
	other := seedSiblingProject(t, ctx, ti, "trend-foreign")
	seedTrendRow(t, ctx, ti, other.issuerID, refreshable(at(2, 0), nil, far, none))

	trend, err := ti.service.GetTrend(ctx, &gen.GetTrendPayload{
		MetaMcpServerID: gateway.ID.String(), From: at(0, 0).Format(time.RFC3339), To: at(6, 0).Format(time.RFC3339), Bucket: "hour",
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, []bucketCounts{
		{created: 2, revoked: 0, active: 3}, // C and D issued; A carried in
		{created: 0, revoked: 0, active: 2}, // D expires at 01:00; A's refresh is neither
		{created: 1, revoked: 0, active: 2}, // B issued at 02:00; C expires
		{created: 1, revoked: 0, active: 2}, // E issued and ended with its token; A refreshed
		{created: 0, revoked: 1, active: 1}, // B revoked at 04:00
		{created: 1, revoked: 1, active: 1}, // the self-stamped root issued and revoked
	}, trendCounts(trend))
	require.Equal(t, []int64{1, 5, 2, 1}, []int64{trend.ActiveAtStart, trend.Created, trend.Revoked, trend.ActiveAtEnd})
	require.Equal(t, trend.Buckets[len(trend.Buckets)-1].Active, trend.ActiveAtEnd, "the summary is the last bucket's stock")
	require.EqualValues(t, 1, trend.ExcludedLegacyRows)
	require.NotNil(t, trend.UnattributedRows)
	require.EqualValues(t, 1, *trend.UnattributedRows)
	require.Equal(t, at(0, 0).Format(time.RFC3339Nano), trend.Buckets[0].Start)
	require.Equal(t, at(6, 0).Format(time.RFC3339Nano), trend.Buckets[5].End)

	// A window off the hour clips its first and last buckets to the window,
	// keeping fixed UTC edges inside it.
	clipped, err := ti.service.GetTrend(ctx, &gen.GetTrendPayload{
		MetaMcpServerID: gateway.ID.String(), From: at(0, 20).Format(time.RFC3339), To: at(4, 0).Format(time.RFC3339), Bucket: "hour",
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, clipped.Buckets, 4)
	require.Equal(t, []string{at(0, 20).Format(time.RFC3339Nano), at(1, 0).Format(time.RFC3339Nano)}, []string{clipped.Buckets[0].Start, clipped.Buckets[0].End})
	require.Equal(t, []int64{2, 3, 0, 2}, []int64{clipped.ActiveAtStart, clipped.Created, clipped.Revoked, clipped.ActiveAtEnd}, "D was already issued at 00:20, C, B and E are issued inside, and B's revocation at the window end falls outside")
	require.Equal(t, []bucketCounts{{created: 1, revoked: 0, active: 3}, {created: 0, revoked: 0, active: 2}, {created: 1, revoked: 0, active: 2}, {created: 1, revoked: 0, active: 2}}, trendCounts(clipped))

	// Daily buckets over the same history, and a quiet window that keeps the
	// stock through empty buckets.
	daily, err := ti.service.GetTrend(ctx, &gen.GetTrendPayload{
		MetaMcpServerID: gateway.ID.String(), From: at(-24, 0).Format(time.RFC3339), To: at(48, 0).Format(time.RFC3339), Bucket: "day",
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, []bucketCounts{{created: 1, revoked: 0, active: 1}, {created: 6, revoked: 2, active: 2}, {created: 0, revoked: 0, active: 2}}, trendCounts(daily), "A, then every root issued on the epoch day, with A and the 06:00 root live after it")
	quiet, err := ti.service.GetTrend(ctx, &gen.GetTrendPayload{
		MetaMcpServerID: gateway.ID.String(), From: at(100, 0).Format(time.RFC3339), To: at(103, 0).Format(time.RFC3339), Bucket: "hour",
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, []bucketCounts{{0, 0, 2}, {0, 0, 2}, {0, 0, 2}}, trendCounts(quiet))
	require.Zero(t, quiet.Created+quiet.Revoked)
	require.EqualValues(t, 1, quiet.ExcludedLegacyRows, "the legacy row is still usable, so still disclosed")
}

func (r trendRow) withIdentity(identity string) trendRow {
	r.identity = identity
	return r
}

// The trend reads the project as the session list does and the gateway as
// connection health does: a gateway the caller may not read is not found,
// alike with a missing or foreign one. log:read is not needed.
func TestGetTrend_RequiresProjectAndGatewayRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuer := seedIssuer(t, ctx, ti, "trend-authz")
	gateway := seedGateway(t, ctx, ti, "Trend authz gateway", uuid.NullUUID{UUID: issuer, Valid: true})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectRead := authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String())
	gatewayRead := authz.NewGrant(authz.ScopeMCPRead, gateway.ID.String())
	read := func(grants []authz.Grant, gatewayID, from, to, bucket string) error {
		_, err := ti.service.GetTrend(authztest.WithExactGrants(t, ctx, grants...), &gen.GetTrendPayload{
			MetaMcpServerID: gatewayID, From: from, To: to, Bucket: bucket, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		})
		if err != nil {
			return fmt.Errorf("get trend: %w", err)
		}
		return nil
	}
	from, to := at(0, 0).Format(time.RFC3339), at(6, 0).Format(time.RFC3339)
	code := func(err error) oops.Code {
		t.Helper()
		shareable, ok := errors.AsType[*oops.ShareableError](err)
		require.True(t, ok, "%v", err)
		return shareable.Code
	}

	require.NoError(t, read([]authz.Grant{projectRead, gatewayRead}, gateway.ID.String(), from, to, "hour"))
	require.NoError(t, read([]authz.Grant{projectRead, gatewayRead}, strings.ToUpper(gateway.ID.String()), from, to, "hour"), "an uppercase id still meets the exact grant")
	require.Equal(t, oops.CodeForbidden, code(read([]authz.Grant{gatewayRead}, gateway.ID.String(), from, to, "hour")), "no project:read")
	require.Equal(t, oops.CodeNotFound, code(read([]authz.Grant{projectRead}, gateway.ID.String(), from, to, "hour")), "no read on the gateway")
	require.Equal(t, oops.CodeNotFound, code(read([]authz.Grant{projectRead, gatewayRead}, uuid.NewString(), from, to, "hour")), "no such gateway")
	other := seedSiblingProject(t, ctx, ti, "trend-authz-foreign")
	foreign, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: other.projectID, Name: "Foreign gateway",
		UserSessionIssuerID: uuid.NullUUID{UUID: other.issuerID, Valid: true},
	})
	require.NoError(t, err)
	foreignGateway := foreign.ID
	require.Equal(t, oops.CodeNotFound, code(read([]authz.Grant{projectRead, authz.NewGrant(authz.ScopeMCPRead, foreignGateway.String())}, foreignGateway.String(), from, to, "hour")), "another project's gateway")
	for name, window := range map[string][3]string{
		"reversed":   {to, from, "hour"},
		"empty":      {from, from, "hour"},
		"too long":   {from, at(31*24+1, 0).Format(time.RFC3339), "hour"},
		"bad bucket": {from, to, "minute"},
		"bad time":   {"yesterday", to, "hour"},
	} {
		require.Equal(t, oops.CodeBadRequest, code(read([]authz.Grant{projectRead, gatewayRead}, gateway.ID.String(), window[0], window[1], window[2])), name)
	}
}
