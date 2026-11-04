package chatplan_test

import (
	"context"
	"log"
	"os"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	s "github.com/speakeasy-api/gram/server/internal/authz/schema"
	"github.com/speakeasy-api/gram/server/internal/chat/chatplan"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

// ownershipModel declares chat ownership the way the authorization design does: the
// author reads their chat, and the owner of a live agent reads the sessions
// that agent produced with no human attached. The production chat type has
// no facts yet, so the adapter is proven against this model.
func ownershipModel(t *testing.T) *authz.RegistryEvaluator {
	t.Helper()
	reg, err := s.Compile(s.Config{Roles: nil, WriteSurfaces: []string{"access"}, Conditions: nil, RetiredRuntimeScopes: nil},
		s.NewType("agent", s.WrittenBy("access"),
			s.Attr("owner", s.FactUser),
			s.Attr("deleted", s.FactBool),
			s.Attr("owner_reassignment_required_at", s.FactNullableTimestamp),
			s.Guard("live", s.All(s.IsFalse("deleted"), s.IsNull("owner_reassignment_required_at"))),
			s.Grant("reader", s.StoredAs("agent:read"), s.Describe("Read agents")),
			s.Permission("read", s.Rel("reader"), s.Checks("agent:read")),
			s.Permission("is_owner", s.When("live", s.Is("owner"))),
		),
		s.NewType("chat", s.WrittenBy("access"),
			s.Attr("internal_user_id", s.FactNullableString),
			s.Attr("external_user_id", s.FactNullableString),
			s.Attr("agent", "agent"),
			s.Derived("author", s.NonEmptyPrincipal(s.FactUser, "internal_user_id")),
			s.Guard("agent_only", s.All(
				s.IsNull("internal_user_id"),
				s.NullOrEmpty("external_user_id"),
				s.Present("agent"),
			)),
			s.Grant("reader", s.StoredAs("chat:read"), s.Describe("Read session transcripts")),
			s.Grant("writer", s.StoredAs("chat:write"), s.Describe("Change sessions")),
			s.Permission("read_grant", s.Union(s.Rel("reader"), s.Rel("writer")), s.Checks("chat:read")),
			s.Permission("write_grant", s.Rel("writer"), s.Checks("chat:write")),
			s.Permission("read", s.Union(
				s.Rel("reader"), s.Rel("writer"), s.Is("author"),
				s.When("agent_only", s.Via("agent", "is_owner")),
			)),
			s.Permission("write", s.Union(s.Rel("writer"), s.Is("author"))),
			s.Permission("read_unless_writer", s.Minus(s.Perm("read"), s.Rel("writer"))),
		),
	)
	require.NoError(t, err)
	evaluator, err := authz.NewRegistryEvaluator(reg)
	require.NoError(t, err)
	return evaluator
}

type fixture struct {
	conn      *pgxpool.Pool
	orgID     string
	projectID uuid.UUID
	chats     map[string]uuid.UUID
}

const (
	actorID = "user_actor"
	otherID = "user_other"
)

func seed(t *testing.T) fixture {
	t.Helper()
	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "chatplantest")
	require.NoError(t, err)

	org := func(id string) uuid.UUID {
		_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
			ID: id, Name: "Plan Org", Slug: id, WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
		})
		require.NoError(t, err)
		for _, user := range []string{actorID, otherID} {
			_, err := orgrepo.New(conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
				OrganizationID: id, UserID: conv.ToPGText(user),
			})
			require.NoError(t, err)
		}
		project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
			Name: "Plan Project", Slug: "plan-" + uuid.NewString()[:8], OrganizationID: id,
		})
		require.NoError(t, err)
		return project.ID
	}
	orgID := "org-" + uuid.NewString()[:8]
	foreignOrgID := "org-" + uuid.NewString()[:8]
	projectID := org(orgID)
	foreignProjectID := org(foreignOrgID)
	otherProjectID, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "Other Project", Slug: "plan-" + uuid.NewString()[:8], OrganizationID: orgID,
	})
	require.NoError(t, err)

	agent := func(organizationID string, project uuid.UUID, owner, name, state string) uuid.UUID {
		created, err := agentrepo.New(conn).CreateAgent(ctx, agentrepo.CreateAgentParams{
			OrganizationID: organizationID, OwnerUserID: owner,
			ProjectID: uuid.NullUUID{UUID: project, Valid: true}, Name: name,
		})
		require.NoError(t, err)
		switch state {
		case "deleted":
			_, err = conn.Exec(ctx, "UPDATE agents SET deleted_at = clock_timestamp() WHERE id = $1", created.ID) //nolint:glint // notestingrawsql: seed a deleted agent row without the service's cascade
		case "latched":
			_, err = conn.Exec(ctx, "UPDATE agents SET owner_reassignment_required_at = clock_timestamp(), owner_reassignment_reason = 'owner_removed' WHERE id = $1", created.ID) //nolint:glint // notestingrawsql: seed a latched agent row directly
		}
		require.NoError(t, err)
		return created.ID
	}
	owned := agent(orgID, projectID, actorID, "owned", "live")
	deleted := agent(orgID, projectID, actorID, "deleted", "deleted")
	latched := agent(orgID, projectID, actorID, "latched", "latched")
	foreign := agent(orgID, projectID, otherID, "foreign", "live")
	otherTenant := agent(foreignOrgID, foreignProjectID, actorID, "other tenant", "live")

	chats := map[string]uuid.UUID{}
	chat := func(name string, project uuid.UUID, user, external *string, agentID *uuid.UUID, removed bool) {
		id := uuid.New()
		var agentArg uuid.NullUUID
		if agentID != nil {
			agentArg = uuid.NullUUID{UUID: *agentID, Valid: true}
		}
		//nolint:glint // notestingrawsql: seed identity combinations (empty strings, an agent beside a human, deleted rows) that no single SQLc method writes
		_, err := conn.Exec(ctx, `INSERT INTO chats (id, project_id, organization_id, user_id, external_user_id, agent_id, title, deleted_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, CASE WHEN $8 THEN clock_timestamp() END)`, id, project, orgID, user, external, agentArg, name, removed)
		require.NoError(t, err)
		chats[name] = id
	}
	str := func(v string) *string { return &v }
	chat("authored", projectID, str(actorID), nil, nil, false)
	chat("authored with agent", projectID, str(actorID), nil, &foreign, false)
	chat("other author", projectID, str(otherID), nil, nil, false)
	chat("other author with owned agent", projectID, str(otherID), nil, &owned, false)
	chat("empty user with owned agent", projectID, str(""), nil, &owned, false)
	chat("external", projectID, nil, str("customer@example.com"), nil, false)
	chat("external with owned agent", projectID, nil, str("customer@example.com"), &owned, false)
	chat("empty external with owned agent", projectID, nil, str(""), &owned, false)
	chat("owned agent", projectID, nil, nil, &owned, false)
	chat("deleted agent", projectID, nil, nil, &deleted, false)
	chat("latched agent", projectID, nil, nil, &latched, false)
	chat("foreign agent", projectID, nil, nil, &foreign, false)
	chat("other tenant agent", projectID, nil, nil, &otherTenant, false)
	chat("anonymous", projectID, nil, nil, nil, false)
	chat("deleted chat", projectID, str(actorID), nil, nil, true)
	chat("other project", otherProjectID.ID, str(actorID), nil, nil, false)

	return fixture{conn: conn, orgID: orgID, projectID: projectID, chats: chats}
}

func (f fixture) names(ids []uuid.UUID) []string {
	var names []string
	for name, id := range f.chats {
		if slices.Contains(ids, id) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

func repeatableRead(t *testing.T, conn *pgxpool.Pool) pgx.Tx {
	t.Helper()
	tx, err := conn.BeginTx(t.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead}) //nolint:glint // notestingrawsql: the repeatable-read snapshot is what the listing is tested in
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	return tx
}

// pointVisible decides every live chat of the project one at a time, with
// facts read from the same rows the plan filters, the way a point read would.
func pointVisible(t *testing.T, tx pgx.Tx, f fixture, evaluator *authz.RegistryEvaluator, set authz.PolicySet, permission string) []uuid.UUID {
	t.Helper()
	//nolint:glint // notestingrawsql: read the facts independently of the adapter under test, in the same snapshot
	rows, err := tx.Query(t.Context(), `SELECT c.id, c.user_id, c.external_user_id, a.id, a.owner_user_id, a.deleted, a.owner_reassignment_required_at IS NOT NULL
FROM chats c
LEFT JOIN agents a ON a.id = c.agent_id AND a.organization_id = c.organization_id
WHERE c.organization_id = $1 AND c.project_id = $2 AND c.deleted IS FALSE`, f.orgID, f.projectID)
	require.NoError(t, err)
	defer rows.Close()

	text := func(v *string) authz.Fact {
		if v == nil {
			return authz.AbsentFact()
		}
		return authz.StringFact(*v)
	}
	var visible []uuid.UUID
	for rows.Next() {
		var (
			id                    uuid.UUID
			user, external, owner *string
			agentID               uuid.NullUUID
			deleted, latched      *bool
		)
		require.NoError(t, rows.Scan(&id, &user, &external, &agentID, &owner, &deleted, &latched))
		agent := authz.AbsentFact()
		if agentID.Valid {
			reassignment := authz.AbsentFact()
			if *latched {
				reassignment = authz.PresentFact()
			}
			agent = authz.RelatedFact(authz.RelatedResource{ID: agentID.UUID.String(), Dimensions: nil, Facts: map[string]authz.Fact{
				"owner": text(owner), "deleted": authz.BoolFact(*deleted), "owner_reassignment_required_at": reassignment,
			}})
		}
		decision, err := evaluator.Decide(set, authz.PermissionRequest{
			Permission:      s.PermissionRef{Type: "chat", Permission: permission},
			ResourceID:      id.String(),
			Dimensions:      nil,
			StrictMatch:     false,
			RepresentedUser: "",
			Actor:           urn.Principal{},
			Facts:           map[string]authz.Fact{"internal_user_id": text(user), "external_user_id": text(external), "agent": agent},
			RequiredFacts:   nil,
		})
		require.NoError(t, err)
		if decision.Allowed {
			visible = append(visible, id)
		}
	}
	require.NoError(t, rows.Err())
	return visible
}

func plan(t *testing.T, tx pgx.Tx, f fixture, evaluator *authz.RegistryEvaluator, set authz.PolicySet, permission string) authz.AccessPlan {
	t.Helper()
	compiled, err := evaluator.Plan(set, s.PermissionRef{Type: "chat", Permission: permission}, chatplan.Adapter,
		authz.PlanTenant{OrganizationID: f.orgID, ProjectID: f.projectID.String()})
	require.NoError(t, err)
	bound, err := chatplan.Bind(t.Context(), tx, compiled)
	require.NoError(t, err)
	return bound
}

func grant(principal, scope, id string) authz.Grant {
	return authz.Grant{
		PrincipalUrn: principal,
		Scope:        authz.Scope(scope),
		Selector:     authz.Selector{authz.SelectorKeyResourceKind: "chat", authz.SelectorKeyResourceID: id},
		Inferred:     false,
	}
}

func userSet(user string, grants ...authz.Grant) authz.PolicySet {
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, user)
	return authz.PolicySet{
		Actor:           actor,
		Policies:        []authz.Policy{{Principal: "live[0]", Grants: grants}},
		Captured:        nil,
		RepresentedUser: user,
		Represented:     nil,
	}
}

// TestPlanMatchesPointDecisions holds the SQL listing to the evaluator: for
// every caller, the rows the plan admits are exactly the rows a point
// decision allows.
func TestPlanMatchesPointDecisions(t *testing.T) {
	t.Parallel()

	f := seed(t)
	evaluator := ownershipModel(t)
	named := func(names ...string) []authz.Grant {
		var grants []authz.Grant
		for _, name := range names {
			grants = append(grants, grant("user:"+actorID, "chat:read", f.chats[name].String()))
		}
		return grants
	}
	captured := userSet(actorID, grant("role:organization:role_1", "chat:read", "*"))
	captured.Captured = []authz.Policy{{Principal: "captured[0]", Grants: named("anonymous")}}
	agentActor := urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString())

	cases := []struct {
		name       string
		set        authz.PolicySet
		permission string
		want       []string
	}{
		{
			name: "member without grants sees authored and owned agent sessions", set: userSet(actorID), permission: "read",
			want: []string{"authored", "authored with agent", "empty external with owned agent", "owned agent"},
		},
		{
			name: "another member sees their own", set: userSet(otherID), permission: "read",
			want: []string{"foreign agent", "other author", "other author with owned agent"},
		},
		{
			name: "project-wide reader sees every live session", set: userSet(actorID, grant("role:organization:role_1", "chat:read", "*")), permission: "read",
			want: []string{
				"anonymous", "authored", "authored with agent", "deleted agent", "empty external with owned agent",
				"empty user with owned agent", "external", "external with owned agent", "foreign agent", "latched agent",
				"other author", "other author with owned agent", "other tenant agent", "owned agent",
			},
		},
		{
			name: "named grants add exactly those sessions", set: userSet(actorID, named("anonymous", "deleted agent")...), permission: "read",
			want: []string{"anonymous", "authored", "authored with agent", "deleted agent", "empty external with owned agent", "owned agent"},
		},
		{
			name: "a captured policy narrows a project-wide reader", set: captured, permission: "read",
			want: []string{"anonymous", "authored", "authored with agent", "empty external with owned agent", "owned agent"},
		},
		{
			name: "owning an agent permits no write", set: userSet(actorID), permission: "write",
			want: []string{"authored", "authored with agent"},
		},
		{
			name: "a difference removes the subtracted sessions", set: userSet(actorID, grant("user:"+actorID, "chat:write", f.chats["authored"].String())), permission: "read_unless_writer",
			want: []string{"authored with agent", "empty external with owned agent", "owned agent"},
		},
		{
			name: "grant-only permission", set: userSet(actorID, named("external")...), permission: "read_grant",
			want: []string{"external"},
		},
		{
			name: "an agent actor owns nothing",
			set: authz.PolicySet{
				Actor: agentActor, Policies: []authz.Policy{{Principal: "live[0]", Grants: nil}},
				Captured: nil, RepresentedUser: "", Represented: nil,
			},
			permission: "read",
			want:       nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tx := repeatableRead(t, f.conn)
			bound := plan(t, tx, f, evaluator, tc.set, tc.permission)
			page, err := chatplan.ListVisible(t.Context(), tx, bound, 100, 0)
			require.NoError(t, err)
			listed := make([]uuid.UUID, 0, len(page.Rows))
			for _, row := range page.Rows {
				listed = append(listed, row.ID)
			}

			point := pointVisible(t, tx, f, evaluator, tc.set, tc.permission)
			require.ElementsMatch(t, point, listed, "the listing must admit exactly what point decisions allow")
			require.Equal(t, tc.want, f.names(listed))
			if len(listed) > 0 {
				require.Equal(t, int64(len(listed)), page.Total)
			}
			total, err := chatplan.CountVisible(t.Context(), tx, bound)
			require.NoError(t, err)
			require.Equal(t, int64(len(listed)), total)
		})
	}
}

// TestListVisiblePaginatesInOneSnapshot shows the predicate applies before
// LIMIT and OFFSET, and that the page and total ignore rows committed after
// the plan was bound.
func TestListVisiblePaginatesInOneSnapshot(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := seed(t)
	evaluator := ownershipModel(t)
	set := userSet(actorID)

	tx := repeatableRead(t, f.conn)
	bound := plan(t, tx, f, evaluator, set, "read")

	// A session authored by the actor, committed after the snapshot.
	_, err := f.conn.Exec(ctx, "INSERT INTO chats (id, project_id, organization_id, user_id) VALUES ($1, $2, $3, $4)", uuid.New(), f.projectID, f.orgID, actorID) //nolint:glint // notestingrawsql: commit a row outside the transaction under test
	require.NoError(t, err)

	var seen []uuid.UUID
	for offset := int32(0); offset < 4; offset += 2 {
		page, err := chatplan.ListVisible(ctx, tx, bound, 2, offset)
		require.NoError(t, err)
		require.Len(t, page.Rows, 2)
		require.Equal(t, int64(4), page.Total, "the total counts visible rows, not the page")
		for _, row := range page.Rows {
			seen = append(seen, row.ID)
		}
	}
	require.Equal(t, []string{"authored", "authored with agent", "empty external with owned agent", "owned agent"}, f.names(seen))

	past, err := chatplan.ListVisible(ctx, tx, bound, 2, 10)
	require.NoError(t, err)
	require.Empty(t, past.Rows)
	require.Equal(t, int64(4), past.Total, "past the end, the total comes from the same snapshot")

	fresh := repeatableRead(t, f.conn)
	_, err = chatplan.ListVisible(ctx, fresh, bound, 2, 0)
	require.ErrorIs(t, err, authz.ErrPlanStale, "a plan bound to one snapshot does not run in another")
	rebound := plan(t, fresh, f, evaluator, set, "read")
	total, err := chatplan.CountVisible(ctx, fresh, rebound)
	require.NoError(t, err)
	require.Equal(t, int64(5), total, "a new snapshot sees the new session")
}

func TestBindRequiresOneSnapshot(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := seed(t)
	evaluator := ownershipModel(t)
	compiled, err := evaluator.Plan(userSet(actorID), s.PermissionRef{Type: "chat", Permission: "read"}, chatplan.Adapter,
		authz.PlanTenant{OrganizationID: f.orgID, ProjectID: f.projectID.String()})
	require.NoError(t, err)

	readCommitted, err := f.conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}) //nolint:glint // notestingrawsql: a read-committed transaction is the refused case
	require.NoError(t, err)
	t.Cleanup(func() { _ = readCommitted.Rollback(context.Background()) })
	_, err = chatplan.Bind(ctx, readCommitted, compiled)
	require.ErrorIs(t, err, chatplan.ErrSnapshotIsolation)

	other := authz.PlanAdapter{Name: "chats.other", Table: chatplan.Adapter.Table}
	foreign, err := evaluator.Plan(userSet(actorID), s.PermissionRef{Type: "chat", Permission: "read"}, other,
		authz.PlanTenant{OrganizationID: f.orgID, ProjectID: f.projectID.String()})
	require.NoError(t, err)
	tx := repeatableRead(t, f.conn)
	bound, err := chatplan.Bind(ctx, tx, foreign)
	require.NoError(t, err)
	_, err = chatplan.CountVisible(ctx, tx, bound)
	require.ErrorIs(t, err, authz.ErrPlanStale, "a plan for %s does not run on %s", other.Name, chatplan.Adapter.Name)

	agents, err := evaluator.Plan(userSet(actorID), s.PermissionRef{Type: "agent", Permission: "read"}, chatplan.Adapter,
		authz.PlanTenant{OrganizationID: f.orgID, ProjectID: f.projectID.String()})
	require.NoError(t, err)
	agents, err = chatplan.Bind(ctx, tx, agents)
	require.NoError(t, err)
	_, err = chatplan.CountVisible(ctx, tx, agents)
	require.ErrorIs(t, err, authz.ErrPlanStale, "a plan for another type does not run on chats")

	chats, err := chatplan.Bind(ctx, tx, compiled)
	require.NoError(t, err)
	_, err = chatplan.ListVisible(ctx, tx, chats, 0, 0)
	require.Error(t, err, "an empty page is refused rather than reporting a zero total")
}
