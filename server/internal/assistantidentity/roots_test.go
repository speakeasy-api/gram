package assistantidentity_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
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
			if _, err := q.LockAssistant(t.Context(), repo.LockAssistantParams{OrganizationID: f.org, ProjectID: f.project, AssistantID: target}); err != nil {
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
		expected := assistantidentity.ErrInvalidIdentity
		if target == next {
			expected = assistantidentity.ErrBrokenMapping
		}
		require.ErrorIs(t, testIdentityService.Validate(t.Context(), f.db, first), expected)
	}
}

func TestTombstonesWithdrawBindingAtomically(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"root", "assistant"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			id := f.provision(t)
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
			require.NoError(t, testIdentityService.Validate(t.Context(), f.db, id))
			require.NoError(t, inTx(t, f.db, mutate))
			require.ErrorIs(t, testIdentityService.Validate(t.Context(), f.db, id), assistantidentity.ErrInvalidIdentity)
		})
	}
}

func TestRootBindingRejectsNoncanonicalAssistantTarget(t *testing.T) {
	t.Parallel()
	for _, spelling := range []string{"uppercase", "compact", "braced"} {
		t.Run(spelling, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.provision(t)
			target := f.assistant.String()
			switch spelling {
			case "uppercase":
				target = strings.ToUpper(target)
			case "compact":
				target = strings.ReplaceAll(target, "-", "")
			case "braced":
				target = "{" + target + "}"
			}
			root := uuid.New()
			q := repo.New(f.db)
			require.NoError(t, q.FixtureCreateRoot(t.Context(), repo.FixtureCreateRootParams{ID: root, OrganizationID: f.org, ProjectID: f.project, DefinitionSlug: "cron", TargetRef: target}))
			before, err := q.FixtureAuthorityCounts(t.Context(), f.org)
			require.NoError(t, err)
			err = inTx(t, f.db, func(tx pgx.Tx) error {
				return testIdentityService.BindRootTrigger(t.Context(), tx, f.org, f.project, root)
			})
			require.ErrorIs(t, err, assistantidentity.ErrBrokenMapping)
			after, err := q.FixtureAuthorityCounts(t.Context(), f.org)
			require.NoError(t, err)
			require.Equal(t, before, after, "invalid target must not allocate authority")
			require.NoError(t, q.FixtureRetargetTrigger(t.Context(), repo.FixtureRetargetTriggerParams{OrganizationID: f.org, ProjectID: f.project, TriggerID: root, TargetRef: f.assistant.String()}))
			require.NoError(t, inTx(t, f.db, func(tx pgx.Tx) error {
				return testIdentityService.BindRootTrigger(t.Context(), tx, f.org, f.project, root)
			}))
			result, err := testIdentityService.Resolve(t.Context(), f.db, f.org, f.project, f.assistant, root)
			require.NoError(t, err)
			require.Equal(t, assistantidentity.Active, result.State)
		})
	}
}
