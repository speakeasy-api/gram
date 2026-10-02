package assistantidentity_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	sessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestUpgradeNeverMintsContinuationWorkloads(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	q := repo.New(f.db)
	wake := uuid.New()
	require.NoError(t, q.FixtureCreateRoot(t.Context(), repo.FixtureCreateRootParams{ID: wake, OrganizationID: f.org, ProjectID: f.project, DefinitionSlug: "wake", TargetRef: f.assistant.String()}))
	_, err := testIdentityService.Resolve(t.Context(), f.db, f.org, f.project, f.assistant, wake)
	require.ErrorIs(t, err, assistantidentity.ErrInvalidIdentity)
	first := f.provision(t)
	second := f.provision(t)
	require.Equal(t, first, second)
	counts, err := q.FixtureAuthorityCounts(t.Context(), f.org)
	require.NoError(t, err)
	require.Equal(t, int64(1), counts.Triggers)
	require.Equal(t, int64(1), counts.Admissions)
	require.Equal(t, int64(1), counts.Assignments)
	_, err = q.GetTriggerBinding(t.Context(), repo.GetTriggerBindingParams{PlatformIssuer: "https://platform.example.invalid", PlatformJwksUri: "https://platform.example.invalid/.well-known/jwks.json", OrganizationID: f.org, ProjectID: f.project, TriggerID: wake})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	err = inTx(t, f.db, func(tx pgx.Tx) error {
		return testIdentityService.BindRootTrigger(t.Context(), tx, f.org, f.project, wake)
	})
	require.ErrorIs(t, err, assistantidentity.ErrInvalidIdentity)
	_, err = testIdentityService.Resolve(t.Context(), f.db, f.org, f.project, f.assistant, wake)
	require.ErrorIs(t, err, assistantidentity.ErrBrokenMapping)
	// Even a corrupted historic root must not validate as a continuation.
	require.NoError(t, q.FixtureSetTriggerDefinition(t.Context(), repo.FixtureSetTriggerDefinitionParams{OrganizationID: f.org, ProjectID: f.project, TriggerID: f.trigger, DefinitionSlug: "wake"}))
	result, err := testIdentityService.Resolve(t.Context(), f.db, f.org, f.project, f.assistant, f.trigger)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Tombstoned, result.State)
	require.ErrorIs(t, testIdentityService.Validate(t.Context(), f.db, first), assistantidentity.ErrInvalidIdentity)
}

func TestRetargetPreservesSubjectAndRetiresOldGeneration(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	first := f.provision(t)
	q := repo.New(f.db)
	next, legacy := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{next, legacy} {
		require.NoError(t, q.FixtureCreateAssistant(t.Context(), repo.FixtureCreateAssistantParams{ID: id, OrganizationID: f.org, ProjectID: f.project, Creator: conv.ToPGText(f.actor)}))
	}
	require.NoError(t, inTx(t, f.db, func(tx pgx.Tx) error {
		_, err := testIdentityService.Provision(t.Context(), tx, assistantidentity.ProvisionParams{OrganizationID: f.org, ProjectID: f.project, AssistantID: next, ActorUserID: f.actor})
		if err != nil {
			return fmt.Errorf("provision retarget fixture: %w", err)
		}
		return nil
	}))
	for _, target := range []uuid.UUID{next, legacy} {
		require.NoError(t, inTx(t, f.db, func(tx pgx.Tx) error {
			q := repo.New(tx)
			if _, err := q.LockProject(t.Context(), repo.LockProjectParams{OrganizationID: f.org, ProjectID: f.project}); err != nil {
				return fmt.Errorf("lock retarget fixture: %w", err)
			}
			if err := q.FixtureRetargetTrigger(t.Context(), repo.FixtureRetargetTriggerParams{OrganizationID: f.org, ProjectID: f.project, TriggerID: f.trigger, TargetRef: target.String()}); err != nil {
				return fmt.Errorf("retarget fixture: %w", err)
			}
			return testIdentityService.RetargetRootTrigger(t.Context(), tx, f.org, f.project, f.trigger)
		}))
		result, err := testIdentityService.Resolve(t.Context(), f.db, f.org, f.project, target, f.trigger)
		require.NoError(t, err)
		if target == next {
			require.Equal(t, assistantidentity.Active, result.State)
			require.Equal(t, first.Subject, result.Identity.Subject)
			require.Equal(t, first.IssuerID, result.Identity.IssuerID)
			require.Equal(t, first.TriggerGeneration+1, result.Identity.TriggerGeneration)
			require.NotEqual(t, first.AgentID, result.Identity.AgentID)
		} else {
			require.Equal(t, assistantidentity.Tombstoned, result.State)
		}
		require.Error(t, testIdentityService.Validate(t.Context(), f.db, first))
	}
}

func sessionForIdentity(t *testing.T, f fixture, id assistantidentity.Identity) uuid.UUID {
	t.Helper()
	q := sessionsrepo.New(f.db)
	issuer, err := q.CreateUserSessionIssuer(t.Context(), sessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:      f.project,
		OrganizationID: conv.ToPGText(f.org), Slug: "identity-sessions-" + uuid.NewString(), AuthnChallengeMode: "interactive",
		SessionDuration: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	session, err := q.CreateUserSession(t.Context(), sessionsrepo.CreateUserSessionParams{
		UserSessionIssuerID: issuer.ID, SubjectUrn: urn.NewWorkloadSubject(id.IssuerID, id.Subject), Jti: uuid.NewString(),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, RefreshExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	})
	require.NoError(t, err)
	return session.ID
}

func TestTombstonesRevokeCurrentSessionsAtomically(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"root", "assistant"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			id := f.provision(t)
			session := sessionForIdentity(t, f, id)
			mutate := func(tx pgx.Tx) error {
				switch operation {
				case "root":
					return assistantidentity.TombstoneTrigger(t.Context(), tx, f.org, f.project, f.trigger)
				default:
					return assistantidentity.TombstoneAssistant(t.Context(), tx, f.org, f.project, f.assistant)
				}
			}
			err := inTx(t, f.db, func(tx pgx.Tx) error {
				if err := mutate(tx); err != nil {
					return err
				}
				return errInjected
			})
			require.ErrorIs(t, err, errInjected)
			q := sessionsrepo.New(f.db)
			_, err = q.GetUserSessionByID(t.Context(), sessionsrepo.GetUserSessionByIDParams{ID: session, OrganizationID: f.org})
			require.NoError(t, err, "rollback must restore current authority")
			require.NoError(t, testIdentityService.Validate(t.Context(), f.db, id))
			require.NoError(t, inTx(t, f.db, mutate))
			_, err = q.GetUserSessionByID(t.Context(), sessionsrepo.GetUserSessionByIDParams{ID: session, OrganizationID: f.org})
			require.ErrorIs(t, err, pgx.ErrNoRows)
			require.ErrorIs(t, testIdentityService.Validate(t.Context(), f.db, id), assistantidentity.ErrInvalidIdentity)
		})
	}
}
