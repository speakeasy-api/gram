package builtinmcp

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/builtinmcp/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	issuerrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestValidateUserSessionIssuerTargetLiveness(t *testing.T) {
	t.Parallel()
	container, clone, err := testenv.NewTestPostgres(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })

	for _, organizationIssuer := range []bool{false, true} {
		for _, deleteFirst := range []bool{false, true} {
			name := "project"
			if organizationIssuer {
				name = "organization"
			}
			if deleteFirst {
				name += "/delete-first"
			} else {
				name += "/validate-first"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
				defer cancel()
				db, err := clone(t, "issuer_liveness")
				require.NoError(t, err)
				const org = "org_builtin_liveness"
				_, err = orgrepo.New(db).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{ID: org, Name: "Example", Slug: "liveness"})
				require.NoError(t, err)
				project, err := projectrepo.New(db).CreateProject(ctx, projectrepo.CreateProjectParams{OrganizationID: org, Name: "Target", Slug: "target"})
				require.NoError(t, err)
				issuers := issuerrepo.New(db)
				issuer, err := issuers.CreateUserSessionIssuer(ctx, issuerrepo.CreateUserSessionIssuerParams{ProjectID: project.ID, Slug: "project-issuer", AuthnChallengeMode: "shared", SessionDuration: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true}})
				require.NoError(t, err)
				issuerID := issuer.ID
				if organizationIssuer {
					issuer, err := issuers.CreateOrganizationUserSessionIssuer(ctx, issuerrepo.CreateOrganizationUserSessionIssuerParams{OrganizationID: pgtype.Text{String: org, Valid: true}, Slug: "organization-issuer", AuthnChallengeMode: "shared", SessionDuration: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true}})
					require.NoError(t, err)
					issuerID = issuer.ID
				}
				params := repo.ValidateUserSessionIssuerParams{ID: issuerID, ProjectID: project.ID, OrganizationID: org}
				validation, err := db.Begin(ctx) //nolint:glint // notestingrawsql: hold validation locks while a second transaction deletes the target
				require.NoError(t, err)
				defer func() { _ = validation.Rollback(context.Background()) }()
				deletion, err := db.Begin(ctx) //nolint:glint // notestingrawsql: hold deletion open while a second transaction validates the target
				require.NoError(t, err)
				defer func() { _ = deletion.Rollback(context.Background()) }()

				deleteTarget := func() error {
					_, err := projectrepo.New(deletion).DeleteProject(ctx, project.ID)
					if err != nil {
						return fmt.Errorf("delete target: %w", err)
					}
					return nil
				}
				validate := func() error {
					_, err := repo.New(validation).ValidateUserSessionIssuer(ctx, params)
					if err != nil {
						return fmt.Errorf("validate issuer: %w", err)
					}
					return nil
				}
				first, second := validation, deletion
				var next func() error
				if deleteFirst {
					require.NoError(t, deleteTarget())
					first, second, next = deletion, validation, validate
				} else {
					require.NoError(t, validate())
					next = deleteTarget
				}
				result := make(chan error, 1)
				go func() { result <- next() }()
				// Observe an actual database lock wait, not a sleep or scheduler ordering.
				require.Eventually(t, func() bool {
					blocked, err := testrepo.New(db).RolloutBlockedBackends(ctx, int32(first.Conn().PgConn().PID()))
					if err != nil {
						return false
					}
					for _, pid := range blocked {
						if pid.Valid && pid.Int32 == int32(second.Conn().PgConn().PID()) {
							return true
						}
					}
					return false
				}, 5*time.Second, 10*time.Millisecond)
				require.NoError(t, first.Commit(ctx))
				select {
				case err := <-result:
					if deleteFirst {
						require.ErrorIs(t, err, pgx.ErrNoRows, "validation rechecks liveness after waiting for deletion")
					} else {
						require.NoError(t, err, "deletion proceeds only after validation transaction commits")
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				require.NoError(t, second.Commit(ctx))
				_, err = repo.New(db).ValidateUserSessionIssuer(ctx, params)
				require.ErrorIs(t, err, pgx.ErrNoRows, "a deleted target cannot accept even an organization-wide issuer")
			})
		}
	}
}
