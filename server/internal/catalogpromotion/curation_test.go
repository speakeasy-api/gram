package catalogpromotion

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/catalogpromotion/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func curateCandidate(t *testing.T, f promotionFixture, request CurationRequest) (Candidate, error) {
	t.Helper()
	tx := testenv.BeginTx(t, t.Context(), f.db)
	defer o11y.NoLogDefer(func() error {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), receiptTimeout)
		defer cancel()
		return tx.Rollback(ctx)
	})
	result, err := f.service.CurateTx(t.Context(), tx, request, "user:synthetic-staff")
	if err != nil {
		return Candidate{}, err
	}
	if err := tx.Commit(t.Context()); err != nil {
		return Candidate{}, fmt.Errorf("commit curation test: %w", err)
	}
	return result, nil
}

func TestCandidateCurationPersistsAndRequiresExplicitRestoration(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"reject", "archive"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			f := newPromotionFixture(t)
			original, err := f.service.GetCandidate(t.Context(), f.target)
			require.NoError(t, err)
			request := CurationRequest{OrganizationID: f.target.OrganizationID, PromotionID: uuid.MustParse(original.ID), CurationRevision: original.CurationRevision, Action: action, RetryKey: "curate-once"}
			hidden, err := curateCandidate(t, f, request)
			require.NoError(t, err)
			require.NotEqual(t, original.CurationRevision, hidden.CurationRevision)
			require.True(t, hidden.Eligible, "health evidence is separate from staff disposition")
			replay, err := curateCandidate(t, f, request)
			require.NoError(t, err)
			require.Equal(t, hidden.CurationRevision, replay.CurationRevision)
			auditAction := audit.ActionCatalogPromotionReject
			if action == "archive" {
				auditAction = audit.ActionCatalogPromotionArchive
			}
			count, err := audittest.AuditLogCountByAction(t.Context(), f.db, auditAction)
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
			f.service.now = func() time.Time { return time.Now().UTC().Add(time.Minute) }
			refreshed, err := f.service.GetCandidate(t.Context(), f.target)
			require.NoError(t, err)
			require.Equal(t, hidden.Status, refreshed.Status)
			require.Equal(t, hidden.CurationRevision, refreshed.CurationRevision)
			require.Equal(t, hidden.CheckedAt, refreshed.CheckedAt)
			require.Nil(t, refreshed.SuggestedMetadata)
			targets, err := repo.New(f.db).DiscoveryTargets(t.Context(), repo.DiscoveryTargetsParams{Now: conv.ToPGTimestamptz(f.service.now()), ExcludedOrganizationID: "excluded-synthetic"})
			require.NoError(t, err)
			require.Empty(t, targets)
			_, err = repo.New(f.db).ClaimDiscovery(t.Context(), repo.ClaimDiscoveryParams{OrganizationID: f.target.OrganizationID, ProjectID: f.target.ProjectID, McpServerID: f.target.MCPServerID, Now: conv.ToPGTimestamptz(f.service.now()), NextAttemptAt: conv.ToPGTimestamptz(f.service.now().Add(time.Hour))})
			require.ErrorIs(t, err, pgx.ErrNoRows)
			_, err = f.promote(t)
			require.ErrorIs(t, err, ErrConflict)
			rows, _, err := f.service.ListCandidates(t.Context(), "", 10, "")
			require.NoError(t, err)
			require.Empty(t, rows)
			rows, _, err = f.service.ListCandidates(t.Context(), "", 10, hidden.Status)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			request.Action = "restore"
			request.RetryKey = "restore-once"
			_, err = curateCandidate(t, f, request)
			require.ErrorIs(t, err, ErrStale)
			request.CurationRevision = hidden.CurationRevision
			restored, err := curateCandidate(t, f, request)
			require.NoError(t, err)
			require.Equal(t, "candidate", restored.Status)
			require.False(t, restored.Eligible)
			// A healthy observation started before curation cannot revive restored evidence.
			_, err = repo.New(f.db).StoreEvidence(t.Context(), repo.StoreEvidenceParams{OrganizationID: f.target.OrganizationID, ProjectID: uuid.NullUUID{UUID: f.target.ProjectID, Valid: true}, McpServerID: uuid.NullUUID{UUID: f.target.MCPServerID, Valid: true}, RemoteMcpServerID: uuid.NullUUID{UUID: uuid.MustParse(original.RemoteMCPServerID), Valid: true}, SourceRevision: original.SourceRevision, Eligible: true, Reason: "eligible", ObservedSuccesses: 20, ObservedFailures: 0, ObservedDays: 2, CheckedAt: conv.ToPGTimestamptz(f.health.now)})
			require.ErrorIs(t, err, pgx.ErrNoRows)

			_, err = f.promote(t)
			require.ErrorIs(t, err, ErrStale, "old publication approval cannot survive reject and restore")
			fresh, err := f.service.GetCandidate(t.Context(), f.target)
			require.NoError(t, err)
			require.True(t, fresh.Eligible)
			f.request.CurationRevision = fresh.CurationRevision
			_, err = f.promote(t)
			require.NoError(t, err)
			request.CurationRevision = fresh.CurationRevision
			request.Action = "reject"
			request.RetryKey = "after-publication"
			_, err = curateCandidate(t, f, request)
			require.ErrorIs(t, err, ErrConflict)
		})
	}
}

func TestCandidateCurationOwnershipAndAtomicity(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	c, err := f.service.GetCandidate(t.Context(), f.target)
	require.NoError(t, err)
	request := CurationRequest{OrganizationID: "wrong-synthetic-org", PromotionID: uuid.MustParse(c.ID), CurationRevision: c.CurationRevision, Action: "reject", RetryKey: "once"}
	_, err = curateCandidate(t, f, request)
	require.ErrorIs(t, err, ErrNotFound)
	request.OrganizationID = f.target.OrganizationID
	tx := testenv.BeginTx(t, t.Context(), f.db)
	_, err = f.service.CurateTx(t.Context(), tx, request, "user:synthetic-staff")
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(t.Context()))
	stored, err := repo.New(f.db).PromotionRowFixture(t.Context(), repo.PromotionRowFixtureParams{ProjectID: uuid.NullUUID{UUID: f.target.ProjectID, Valid: true}, McpServerID: uuid.NullUUID{UUID: f.target.MCPServerID, Valid: true}})
	require.NoError(t, err)
	require.Equal(t, "active", stored.CurationStatus)
	count, err := audittest.AuditLogCountByAction(t.Context(), f.db, audit.ActionCatalogPromotionReject)
	require.NoError(t, err)
	require.Zero(t, count)
	hidden, err := curateCandidate(t, f, request)
	require.NoError(t, err)
	request.Action = "archive"
	_, err = curateCandidate(t, f, request)
	require.ErrorIs(t, err, ErrConflict, "retry keys cannot change approved action")
	request.Action = "reject"
	tx = testenv.BeginTx(t, t.Context(), f.db)
	_, err = f.service.CurateTx(t.Context(), tx, request, "user:other-synthetic-staff")
	require.ErrorIs(t, err, ErrConflict, "receipt belongs to the original staff actor")
	require.NoError(t, tx.Rollback(t.Context()))
	require.NoError(t, repo.New(f.db).DeletePromotionProjectFixture(t.Context(), f.target.ProjectID))
	request.Action = "restore"
	request.RetryKey = "after-delete"
	request.CurationRevision = hidden.CurationRevision
	_, err = curateCandidate(t, f, request)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestCandidatePaginationNewestFirstStableAcrossTiesAndInsertions(t *testing.T) {
	t.Parallel()
	f := newPromotionFixture(t)
	q := repo.New(f.db)
	original, err := f.service.GetCandidate(t.Context(), f.target)
	require.NoError(t, err)
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, q.SetCandidateCreatedAtFixture(t.Context(), repo.SetCandidateCreatedAtFixtureParams{OrganizationID: f.target.OrganizationID, ProjectID: uuid.NullUUID{UUID: f.target.ProjectID, Valid: true}, ID: uuid.MustParse(original.ID), CreatedAt: conv.ToPGTimestamptz(stamp)}))
	ids := []string{original.ID}
	for i := range 3 {
		project, err := projectrepo.New(f.db).CreateProject(t.Context(), projectrepo.CreateProjectParams{Name: "Synthetic page", Slug: fmt.Sprintf("synthetic-page-%d", i), OrganizationID: f.target.OrganizationID})
		require.NoError(t, err)
		source, err := q.SeedPromotionSourceFixture(t.Context(), repo.SeedPromotionSourceFixtureParams{ProjectID: project.ID, Url: "https://catalog.example.com/mcp", CreatedAt: conv.ToPGTimestamptz(f.health.now.Add(-7 * 24 * time.Hour))})
		require.NoError(t, err)
		c, err := f.service.GetCandidate(t.Context(), Target{OrganizationID: f.target.OrganizationID, ProjectID: project.ID, MCPServerID: source})
		require.NoError(t, err)
		require.NoError(t, q.SetCandidateCreatedAtFixture(t.Context(), repo.SetCandidateCreatedAtFixtureParams{OrganizationID: f.target.OrganizationID, ProjectID: uuid.NullUUID{UUID: project.ID, Valid: true}, ID: uuid.MustParse(c.ID), CreatedAt: conv.ToPGTimestamptz(stamp)}))
		ids = append(ids, c.ID)
	}
	slices.Sort(ids)
	slices.Reverse(ids)
	first, next, err := f.service.ListCandidates(t.Context(), "", 2, "all")
	require.NoError(t, err)
	require.Len(t, first, 2)
	require.NotEmpty(t, next)
	require.Equal(t, ids[0], first[0].ID)
	require.Equal(t, ids[1], first[1].ID)
	// A new record between page requests must not shift or duplicate the remainder.
	insertedProject, err := projectrepo.New(f.db).CreateProject(t.Context(), projectrepo.CreateProjectParams{Name: "Synthetic insertion", Slug: "synthetic-insertion", OrganizationID: f.target.OrganizationID})
	require.NoError(t, err)
	insertedSource, err := q.SeedPromotionSourceFixture(t.Context(), repo.SeedPromotionSourceFixtureParams{ProjectID: insertedProject.ID, Url: "https://catalog.example.com/mcp", CreatedAt: conv.ToPGTimestamptz(f.health.now.Add(-7 * 24 * time.Hour))})
	require.NoError(t, err)
	_, err = f.service.GetCandidate(t.Context(), Target{OrganizationID: f.target.OrganizationID, ProjectID: insertedProject.ID, MCPServerID: insertedSource})
	require.NoError(t, err)
	second, end, err := f.service.ListCandidates(t.Context(), next, 2, "all")
	require.NoError(t, err)
	require.Empty(t, end)
	require.Len(t, second, 2)
	require.Equal(t, ids[2], second[0].ID)
	require.Equal(t, ids[3], second[1].ID)
	_, _, err = f.service.ListCandidates(t.Context(), next, 2, "candidate")
	require.ErrorIs(t, err, ErrInvalid, "cursor pins selected filter")
	// Moving an older UUID to a newer timestamp proves ordering is timestamp-first.
	require.NoError(t, q.SetCandidateCreatedAtFixture(t.Context(), repo.SetCandidateCreatedAtFixtureParams{OrganizationID: f.target.OrganizationID, ProjectID: uuid.NullUUID{UUID: f.target.ProjectID, Valid: true}, ID: uuid.MustParse(original.ID), CreatedAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour))}))
	newest, _, err := f.service.ListCandidates(t.Context(), "", 1, "active")
	require.NoError(t, err)
	require.Equal(t, original.ID, newest[0].ID)
}
