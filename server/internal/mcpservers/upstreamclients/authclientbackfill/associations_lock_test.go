package authclientbackfill

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// observeOAuthLinks pauses after the first link insert to expose transactions
// that acquire another client's lock after they already hold a link's FK lock.
type observeOAuthLinks struct {
	pgx.Tx
	links   *atomic.Int32
	reached chan<- struct{}
	release <-chan struct{}
}

func (tx observeOAuthLinks) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	tag, err := tx.Tx.Exec(ctx, query, args...) //nolint:glint // notestingrawsql: lock instrumentation delegates dynamically intercepted SQLc statements.
	if err != nil {
		return tag, fmt.Errorf("execute observed query: %w", err)
	}
	if strings.HasPrefix(query, "-- name: LinkAuthClientToServer ") && tx.links.Add(1) == 1 && tx.reached != nil {
		tx.reached <- struct{}{}
		select {
		case <-tx.release:
		case <-ctx.Done():
			return tag, fmt.Errorf("wait after OAuth link: %w", ctx.Err())
		}
	}
	return tag, nil
}

func TestSyncServerAssociations_PrelocksSharedOAuthClientsAcrossProjects(t *testing.T) {
	t.Parallel()
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing_clients_%t", existing), func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			other := f.inProject()
			server, otherServer := f.server(), other.server()
			var registrations, typed []uuid.UUID
			for range 2 {
				issuer, client := uuid.New(), uuid.New()
				require.NoError(t, f.q.SeedRemoteSessionIssuerFixture(t.Context(), SeedRemoteSessionIssuerFixtureParams{ID: issuer, ProjectID: uuid.NullUUID{}, Slug: "shared-" + issuer.String(), Issuer: "https://issuer.example.test/" + issuer.String()}))
				require.NoError(t, f.q.SeedRemoteSessionClientFixture(t.Context(), SeedRemoteSessionClientFixtureParams{ID: client, ProjectID: uuid.NullUUID{}, OrganizationID: pgtype.Text{String: f.orgID, Valid: true}, RemoteSessionIssuerID: issuer, ClientID: "shared-" + client.String(), CredentialOwner: "subject"}))
				registrations = append(registrations, client)
				id := OAuthClientID(f.orgID, client, TypeRemoteSession)
				if existing {
					var err error
					id, err = f.q.SeedAuthClientFixture(t.Context(), SeedAuthClientFixtureParams{OrganizationID: f.orgID, ProjectID: uuid.NullUUID{}, ClientType: TypeRemoteSession, Source: "backfill", RemoteSessionClientID: uuid.NullUUID{UUID: client, Valid: true}})
					require.NoError(t, err)
				}
				typed = append(typed, id)
			}
			// UUIDv7 association order is deliberately reversed across projects.
			f.associate(server, registrations[0])
			f.associate(server, registrations[1])
			other.associate(otherServer, registrations[1])
			other.associate(otherServer, registrations[0])
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			first, second := testenv.BeginTx(t, ctx, f.pool), testenv.BeginTx(t, ctx, f.pool)
			require.NoError(t, LockEMAWriterScope(ctx, first, f.orgID, []uuid.UUID{f.projectID}))
			require.NoError(t, LockEMAWriterScope(ctx, second, f.orgID, []uuid.UUID{other.projectID}), "OAuth-only shared registrations do not serialize the project scopes")
			firstPID, secondPID := first.Conn().PgConn().PID(), second.Conn().PgConn().PID()
			reached, release := make(chan struct{}, 1), make(chan struct{})
			defer close(release)
			var firstLinks, secondLinks atomic.Int32
			firstDone, secondDone := make(chan error, 1), make(chan error, 1)
			go func() {
				err := SyncServerAssociations(ctx, observeOAuthLinks{Tx: first, links: &firstLinks, reached: reached, release: release}, f.orgID, server)
				if err == nil {
					err = first.Commit(ctx)
				}
				firstDone <- err
			}()
			select {
			case <-reached:
			case <-ctx.Done():
				t.Fatal("first sync did not reach its first link")
			}
			go func() {
				err := SyncServerAssociations(ctx, observeOAuthLinks{Tx: second, links: &secondLinks}, f.orgID, otherServer)
				if err == nil {
					err = second.Commit(ctx)
				}
				secondDone <- err
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				err := f.pool.QueryRow(ctx, "SELECT $1::int = ANY(pg_blocking_pids($2::int))", firstPID, secondPID).Scan(&waiting) //nolint:glint // notestingrawsql: observes live backend lock instrumentation, not application data.
				return err == nil && waiting
			}, 5*time.Second, 10*time.Millisecond, "second sync never blocked on the shared OAuth scope")
			require.Zero(t, secondLinks.Load(), "second sync must wait before any link insertion, not after taking an opposing client FK lock")
			release <- struct{}{}
			require.NoError(t, <-firstDone)
			require.NoError(t, <-secondDone)
			require.ElementsMatch(t, typed, f.links(server))
			require.ElementsMatch(t, typed, f.links(otherServer))
		})
	}
}
