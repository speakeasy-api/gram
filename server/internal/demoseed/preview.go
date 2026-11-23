package demoseed

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
)

// PreviewRunner is the one account end-to-end tests sign in as on a pull
// request preview: a member of the preview's test organization and of nothing
// else, outside the fictional roster, never platform staff. No identity
// provider knows it and its address is on a reserved domain that receives no
// mail, so it cannot log in, be invited, or have memberships synced to it.
type PreviewRunner struct {
	ID    string
	Email string
	Name  string
}

// PreviewRunner is the runner of this Spec's organization.
func (s Spec) PreviewRunner() PreviewRunner {
	return PreviewRunner{
		ID:    s.UserPrefix + "e2e-runner",
		Email: "e2e-runner" + s.EmailDomain,
		Name:  "End-to-end test runner",
	}
}

// CurrentDatabase is the name of the database db is connected to, as the
// database reports it.
func CurrentDatabase(ctx context.Context, db *pgxpool.Pool) (string, error) {
	name, err := repo.New(db).PreviewCurrentDatabase(ctx)
	if err != nil {
		return "", fmt.Errorf("read current database: %w", err)
	}
	return name, nil
}

// RunPreviewFixtures adds what a preview's test organization needs on top of
// the seeded data: its role grants, the grants and settings the local
// development organization also gets, and the runner as a member with the Admin
// role. Call it only after Run has completed with spec: the seed deletes the
// organization's memberships.
//
// Everything is written in one transaction and every write is scoped to spec's
// organization, except the runner's own user row. The membership is written
// last, so a failed run leaves an organization nobody can sign in to. Unlike
// RunLocalFixtures it makes nobody platform staff, creates no API key and
// writes no registry or catalog rows.
func RunPreviewFixtures(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, cache *redis.Client, spec Spec) error {
	logger = logger.With(attr.SlogComponent("demoseed.preview"), attr.SlogOrganizationID(spec.OrgID))
	runner := spec.PreviewRunner()
	workosID := "workos_" + runner.ID
	projectID := uuid.MustParse(spec.ProjectID())

	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin preview fixtures transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			logger.ErrorContext(ctx, "roll back preview fixtures", attr.SlogError(err))
		}
	}()

	q := repo.New(tx)
	for _, step := range []struct {
		name string
		run  func() error
	}{
		// A preview's database is a clone of its environment's, which has the
		// built-in roles. Their absence is a refusal: the grant seeder below
		// would otherwise create them, a write that reaches every organization.
		{"built-in roles", func() error {
			for _, slug := range []string{authz.SystemRoleAdmin, authz.SystemRoleMember} {
				if _, err := q.OrgGlobalRoleURN(ctx, slug); err != nil {
					return fmt.Errorf("built-in role %q: %w", slug, err)
				}
			}
			return nil
		}},
		// Ensure, not seed: the seed's tool block on the global Admin role
		// would otherwise count as Admin being provisioned, and the runner
		// (an Admin) would hold no project or organization grant at all.
		{"system role grants", func() error { return authz.EnsureSystemRoleGrantsTx(ctx, tx, spec.OrgID) }},
		{"personal auth client default", func() error { return authz.SeedPersonalAuthClientDefaultTx(ctx, tx, spec.OrgID) }},
		{"grant session visibility", func() error {
			return q.LocalGrantSessionVisibility(ctx, repo.LocalGrantSessionVisibilityParams{OrganizationID: spec.OrgID, UserID: runner.ID})
		}},
		{"grant Channels administration", func() error {
			if _, err := tx.Exec(ctx, localChannelsAccessSQL, spec.OrgID, runner.ID, spec.ProjectID()); err != nil {
				return fmt.Errorf("grant Channels access: %w", err)
			}
			return nil
		}},
		{"grant tool log visibility", func() error {
			return q.LocalGrantToolLogVisibility(ctx, repo.LocalGrantToolLogVisibilityParams{OrganizationID: spec.OrgID, UserID: runner.ID})
		}},
		{"enable platform mcp", func() error { return q.LocalEnablePlatformMCPFeature(ctx, spec.OrgID) }},
		{"approve tool calls", func() error {
			return q.LocalAddToolApprover(ctx, repo.LocalAddToolApproverParams{OrganizationID: spec.OrgID, PolicyID: uuid.MustParse(spec.FixedUUID(approvalPolicyFixedID)), Approver: "user:" + runner.ID})
		}},
		{"default environment", func() error {
			return q.LocalUpsertEnvironment(ctx, repo.LocalUpsertEnvironmentParams{ID: uuid.MustParse(spec.FixedUUID("0000-4000-a000-000000007004")), OrganizationID: spec.OrgID, ProjectID: projectID})
		}},
		{"runner", func() error {
			rows, err := q.PreviewUpsertRunner(ctx, repo.PreviewUpsertRunnerParams{ID: runner.ID, Email: runner.Email, DisplayName: runner.Name, WorkosID: workosID})
			switch {
			case err != nil:
				return fmt.Errorf("upsert runner: %w", err)
			case rows != 1:
				return errors.New("the runner's id belongs to another user")
			}
			return nil
		}},
		{"runner membership", func() error {
			return q.LocalAdoptDeveloper(ctx, repo.LocalAdoptDeveloperParams{OrganizationID: spec.OrgID, UserID: runner.ID, WorkosUserID: workosID})
		}},
	} {
		if err := step.run(); err != nil {
			return fmt.Errorf("preview fixture %q: %w", step.name, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit preview fixtures: %w", err)
	}

	bustLocalCaches(ctx, logger, cache, spec.OrgID, runner.ID)
	logger.InfoContext(ctx, "preview fixtures applied")
	return nil
}
