//nolint:glint // SQL regression tests require isolated ownership and lifecycle fixtures.
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

	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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
	project, otherProject, issuer, otherIssuer, userIssuer := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec := func(t *testing.T, sql string, args ...any) {
		t.Helper()
		_, err := conn.Exec(ctx, sql, args...)
		require.NoError(t, err)
	}
	exec(t, `INSERT INTO organization_metadata (id,name,slug) VALUES ($1,'Test organization','ema-test'),($2,'Other organization','ema-other')`, org, otherOrg)
	exec(t, `INSERT INTO projects (id,organization_id,name,slug) VALUES ($1,$2,'Test project','ema-test'),($3,$2,'Other project','ema-other')`, project, org, otherProject)
	exec(t, `INSERT INTO remote_session_issuers (id,slug,issuer) VALUES ($1,'ema-test','https://issuer.example.test'),($2,'ema-other','https://other.example.test')`, issuer, otherIssuer)
	exec(t, `INSERT INTO user_session_issuers (id,organization_id,slug,authn_challenge_mode,session_duration) VALUES ($1,$2,'ema-test','interactive',interval '1 hour')`, userIssuer, org)
	client := func(projectID any, organizationID any, issuerID uuid.UUID, deleted bool) uuid.UUID {
		t.Helper()
		id := uuid.New()
		exec(t, `INSERT INTO remote_session_clients (id,project_id,organization_id,remote_session_issuer_id,client_id,deleted_at) VALUES ($1,$2,$3,$4,$5,CASE WHEN $6 THEN clock_timestamp() ELSE NULL END)`, id, projectID, organizationID, issuerID, id.String(), deleted)
		return id
	}
	projectClient := client(project, nil, issuer, false)
	orgClient := client(nil, org, issuer, false)
	globalClient := client(nil, nil, issuer, false)
	foreignProjectClient := client(otherProject, org, issuer, false)
	foreignOrgClient := client(nil, otherOrg, issuer, false)
	wrongIssuerClient := client(nil, nil, otherIssuer, false)
	deletedClient := client(nil, nil, issuer, true)
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
				_, err = conn.Exec(ctx, `INSERT INTO projects (id,organization_id,name,slug) VALUES ($1,$2,'Deletion test',$3)`, projectID, org, projectID.String())
				require.NoError(t, err)
				key := repo.EnsureEMABindingParams{ProjectID: projectID, OrganizationID: org, UserSessionIssuerID: userIssuer, RemoteSessionIssuerID: issuer, Resource: "https://resource.example.test/"}
				var params repo.SetEMABindingParams
				if operation == "set" {
					require.NoError(t, q.EnsureEMABinding(ctx, key))
					row, err := q.GetEMABinding(ctx, repo.GetEMABindingParams(key))
					require.NoError(t, err)
					params = repo.SetEMABindingParams{ID: row.ID, ProjectID: projectID, OrganizationID: org, ExpectedGeneration: row.Generation, Generation: row.Generation + 1, State: text("ready"), GrantSource: text("manual"), RemoteSessionClientID: uuid.NullUUID{UUID: globalClient, Valid: true}, RequestedScopes: []string{}}
				}
				deletion, err := conn.Begin(ctx)
				require.NoError(t, err)
				defer func() { _ = deletion.Rollback(context.Background()) }()
				// Match the project lifecycle lock, then soft-delete before commit.
				_, err = deletion.Exec(ctx, `SELECT id FROM projects WHERE id=$1 AND organization_id=$2 FOR UPDATE`, projectID, org)
				require.NoError(t, err)
				_, err = deletion.Exec(ctx, `UPDATE projects SET deleted_at=clock_timestamp() WHERE id=$1 AND organization_id=$2`, projectID, org)
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
					var blocked bool
					err := conn.QueryRow(ctx, `SELECT cardinality(pg_blocking_pids($1)) > 0`, pid).Scan(&blocked)
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
