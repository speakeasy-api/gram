package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestSetEMABindingSQL(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	container, clone, err := testenv.NewTestPostgres(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })
	conn, err := clone(t, "ema_binding_sql")
	require.NoError(t, err)
	q := repo.New(conn)
	const org = "org_ema_sql_test"
	const otherOrg = "org_ema_sql_other"
	project, otherProject, issuer, otherIssuer := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	fixtures := testrepo.New(conn)
	for _, id := range []string{org, otherOrg} {
		require.NoError(t, fixtures.SeedDelegationLoaderOrganizationFixture(ctx, testrepo.SeedDelegationLoaderOrganizationFixtureParams{OrganizationID: id, Name: id, Slug: id}))
	}
	for _, id := range []uuid.UUID{project, otherProject} {
		_, err := fixtures.CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{ID: id, OrganizationID: org, Name: "Test project", Slug: id.String()})
		require.NoError(t, err)
	}
	for _, id := range []uuid.UUID{issuer, otherIssuer} {
		require.NoError(t, fixtures.SeedDelegationLoaderIssuerFixture(ctx, testrepo.SeedDelegationLoaderIssuerFixtureParams{ID: id, Slug: id.String(), Issuer: "https://issuer.example.test/" + id.String()}))
	}
	userIssuer, err := fixtures.InsertOrganizationTierUserSessionIssuerFixture(ctx, testrepo.InsertOrganizationTierUserSessionIssuerFixtureParams{OrganizationID: conv.ToPGText(org), Slug: "ema-test", AuthnChallengeMode: "interactive", SessionDuration: pgtype.Interval{Microseconds: 3600000000, Valid: true}})
	require.NoError(t, err)
	client := func(projectID uuid.NullUUID, organizationID pgtype.Text, issuerID uuid.UUID, deleted bool) uuid.UUID {
		t.Helper()
		id := uuid.New()
		require.NoError(t, fixtures.SeedLifecycleBindingClientFixture(ctx, testrepo.SeedLifecycleBindingClientFixtureParams{ID: id, ProjectID: projectID, OrganizationID: organizationID, RemoteSessionIssuerID: issuerID, ClientID: id.String(), Deleted: deleted}))
		return id
	}
	projectClient := client(conv.ToNullUUID(project), pgtype.Text{}, issuer, false)
	orgClient := client(uuid.NullUUID{}, conv.ToPGText(org), issuer, false)
	globalClient := client(uuid.NullUUID{}, pgtype.Text{}, issuer, false)
	foreignProjectClient := client(conv.ToNullUUID(otherProject), conv.ToPGText(org), issuer, false)
	foreignOrgClient := client(uuid.NullUUID{}, conv.ToPGText(otherOrg), issuer, false)
	wrongIssuerClient := client(uuid.NullUUID{}, pgtype.Text{}, otherIssuer, false)
	deletedClient := client(uuid.NullUUID{}, pgtype.Text{}, issuer, true)
	text := func(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }
	// Ownership fixtures are immutable after setup. Every case gets a unique
	// binding resource; deletion cases also get their own project and pool.
	newBinding := func(t *testing.T) repo.SetEMABindingParams {
		t.Helper()
		ctx := t.Context()
		key := repo.EnsureEMABindingParams{ProjectID: project, OrganizationID: org, UserSessionIssuerID: userIssuer, RemoteSessionIssuerID: issuer, Resource: "https://resource.example.test/" + uuid.NewString()}
		require.NoError(t, q.EnsureEMABinding(ctx, key))
		row, err := q.GetEMABinding(ctx, repo.GetEMABindingParams(key))
		require.NoError(t, err)
		p := repo.SetEMABindingParams{ID: row.ID, ProjectID: project, OrganizationID: org, ExpectedGeneration: row.Generation, Generation: row.Generation + 1, State: text("ready"), GrantSource: text("manual"), RemoteSessionClientID: uuid.NullUUID{UUID: projectClient, Valid: true}, RequestedScopes: []string{}}
		_, err = q.SetEMABinding(ctx, p)
		require.NoError(t, err)
		p.ExpectedGeneration = p.Generation
		return p
	}
	t.Run("generation transitions", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			name    string
			mutate  func(*repo.SetEMABindingParams)
			allowed bool
		}{
			{"status only", func(p *repo.SetEMABindingParams) { p.State = text("configuration_required") }, true},
			{"same generation client", func(p *repo.SetEMABindingParams) { p.RemoteSessionClientID.UUID = orgClient }, false},
			{"same generation provenance", func(p *repo.SetEMABindingParams) { p.GrantSource = text("dcr") }, false},
			{"same generation null provenance", func(p *repo.SetEMABindingParams) { p.GrantSource = pgtype.Text{} }, false},
			{"same generation unlink", func(p *repo.SetEMABindingParams) { p.State = text("unlinked") }, false},
			{"next generation client", func(p *repo.SetEMABindingParams) { p.Generation++; p.RemoteSessionClientID.UUID = orgClient }, true},
			{"next generation provenance", func(p *repo.SetEMABindingParams) { p.Generation++; p.GrantSource = text("dcr") }, true},
			{"next generation unlink", func(p *repo.SetEMABindingParams) { p.Generation++; p.State = text("unlinked") }, true},
			{"skipped generation", func(p *repo.SetEMABindingParams) { p.Generation += 2 }, false},
			{"older generation", func(p *repo.SetEMABindingParams) { p.Generation-- }, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				p := newBinding(t)
				tc.mutate(&p)
				row, err := q.SetEMABinding(ctx, p)
				if !tc.allowed {
					require.ErrorIs(t, err, pgx.ErrNoRows)
					return
				}
				require.NoError(t, err)
				require.Equal(t, p.Generation, row.Generation)
				if p.State.String == "unlinked" {
					require.False(t, row.RemoteSessionClientID.Valid)
				}
			})
		}
	})
	t.Run("client ownership", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			name    string
			id      uuid.UUID
			allowed bool
		}{
			{"project", projectClient, true}, {"organization", orgClient, true}, {"global", globalClient, true},
			{"other project", foreignProjectClient, false}, {"other organization", foreignOrgClient, false},
			{"wrong issuer", wrongIssuerClient, false}, {"deleted global", deletedClient, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				p := newBinding(t)
				p.Generation++
				p.RemoteSessionClientID.UUID = tc.id
				row, err := q.SetEMABinding(ctx, p)
				if !tc.allowed {
					require.ErrorIs(t, err, pgx.ErrNoRows)
					return
				}
				require.NoError(t, err)
				require.Equal(t, tc.id, row.RemoteSessionClientID.UUID)
			})
		}
	})
	t.Run("stale completion cannot undo transition", func(t *testing.T) {
		t.Parallel()
		stale := newBinding(t)
		next := stale
		next.Generation++
		next.RemoteSessionClientID.UUID = globalClient
		next.GrantSource = text("dcr")
		_, err := q.SetEMABinding(ctx, next)
		require.NoError(t, err)
		_, err = q.SetEMABinding(ctx, stale)
		require.ErrorIs(t, err, pgx.ErrNoRows)
	})
	t.Run("relink requires next generation", func(t *testing.T) {
		t.Parallel()
		p := newBinding(t)
		p.Generation++
		p.State = text("unlinked")
		_, err := q.SetEMABinding(ctx, p)
		require.NoError(t, err)
		p.ExpectedGeneration = p.Generation
		p.State = text("ready")
		_, err = q.SetEMABinding(ctx, p)
		require.ErrorIs(t, err, pgx.ErrNoRows)
		p.Generation++
		_, err = q.SetEMABinding(ctx, p)
		require.NoError(t, err)
	})
	t.Run("project deletion serializes binding writes", func(t *testing.T) {
		t.Parallel()
		for _, operation := range []string{"ensure", "set"} {
			t.Run(operation, func(t *testing.T) {
				t.Parallel()
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				// Each lock-wait case needs a deletion transaction, a blocked
				// writer, and an observer. Separate pools prevent parallel
				// cases from exhausting the shared pool while holding locks.
				config := conn.Config()
				config.MaxConns = 3
				conn, err := pgxpool.NewWithConfig(ctx, config)
				require.NoError(t, err)
				t.Cleanup(conn.Close)
				q := repo.New(conn)
				projectID := uuid.New()
				_, err = testrepo.New(conn).CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{ID: projectID, OrganizationID: org, Name: "Deletion test", Slug: projectID.String()})
				require.NoError(t, err)
				key := repo.EnsureEMABindingParams{ProjectID: projectID, OrganizationID: org, UserSessionIssuerID: userIssuer, RemoteSessionIssuerID: issuer, Resource: "https://resource.example.test/"}
				var params repo.SetEMABindingParams
				if operation == "set" {
					require.NoError(t, q.EnsureEMABinding(ctx, key))
					row, err := q.GetEMABinding(ctx, repo.GetEMABindingParams(key))
					require.NoError(t, err)
					params = repo.SetEMABindingParams{ID: row.ID, ProjectID: projectID, OrganizationID: org, ExpectedGeneration: row.Generation, Generation: row.Generation + 1, State: text("ready"), GrantSource: text("manual"), RemoteSessionClientID: uuid.NullUUID{UUID: globalClient, Valid: true}, RequestedScopes: []string{}}
				}
				deletion := testenv.BeginTx(t, ctx, conn)
				// Match the project lifecycle lock, then soft-delete before commit.
				_, err = projectsrepo.New(deletion).LockProjectForEMADeletion(ctx, projectsrepo.LockProjectForEMADeletionParams{ProjectID: projectID, OrganizationID: org})
				require.NoError(t, err)
				_, err = projectsrepo.New(deletion).DeleteProject(ctx, projectID)
				require.NoError(t, err)
				writer, err := conn.Acquire(ctx)
				require.NoError(t, err)
				defer writer.Release()
				pid := writer.Conn().PgConn().PID()
				done := make(chan error, 1)
				go func() {
					if operation == "ensure" {
						done <- repo.New(writer).EnsureEMABinding(ctx, key)
					} else {
						_, err := repo.New(writer).SetEMABinding(ctx, params)
						done <- err
					}
				}()
				// Observe an actual lock wait instead of relying on a scheduling delay.
				require.Eventually(t, func() bool {
					blocked, err := testrepo.New(conn).IsLifecycleBackendBlockedFixture(ctx, int32(pid))
					return err == nil && blocked
				}, 5*time.Second, 10*time.Millisecond)
				require.NoError(t, deletion.Commit(ctx))
				select {
				case err := <-done:
					if operation == "ensure" {
						require.NoError(t, err)
						_, err = q.GetEMABinding(ctx, repo.GetEMABindingParams(key))
						require.ErrorIs(t, err, pgx.ErrNoRows)
					} else {
						require.ErrorIs(t, err, pgx.ErrNoRows)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			})
		}
	})

}
