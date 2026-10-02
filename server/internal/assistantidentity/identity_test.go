package assistantidentity_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestLegacyUpgradeAndConcurrentRetries(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	result, err := testIdentityService.Resolve(t.Context(), f.db, f.org, f.project, f.assistant, f.trigger)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.NeverConfigured, result.State)
	const workers = 6
	identities := make(chan assistantidentity.Binding, workers)
	failures := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			var binding assistantidentity.Binding
			err := inTx(t, f.db, func(tx pgx.Tx) error {
				var err error
				binding, err = testIdentityService.Upgrade(t.Context(), tx, assistantidentity.ProvisionParams{OrganizationID: f.org, ProjectID: f.project, AssistantID: f.assistant, ActorUserID: f.actor})
				if err != nil {
					return fmt.Errorf("fixture operation: %w", err)
				}
				return nil
			})
			if err != nil {
				failures <- err
				return
			}
			identities <- binding
		})
	}
	wg.Wait()
	close(identities)
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	var first assistantidentity.Binding
	for binding := range identities {
		if first.AgentID == uuid.Nil {
			first = binding
		}
		require.Equal(t, first, binding)
	}
	require.NotEqual(t, uuid.Nil, first.AgentID)
	id := f.provision(t)
	require.Equal(t, first.AgentID, id.AgentID)
	counts, err := repo.New(f.db).FixtureAuthorityCounts(t.Context(), f.org)
	require.NoError(t, err)
	require.Equal(t, int64(1), counts.Agents)
	require.Equal(t, int64(1), counts.Triggers)
	snapshot, err := testIdentityService.SnapshotCeiling(t.Context(), f.db, id)
	require.NoError(t, err)
	require.JSONEq(t, `{"requested":[],"effective":[]}`, string(snapshot.Policy))
}

func TestProvisionRollsBackEveryAuthorityWrite(t *testing.T) {
	t.Parallel()
	for _, table := range []string{"agents", "principal_grants", "assistant_agent_bindings", "audit_logs", "workload_issuers", "workload_identity_admissions", "workload_agent_assignments", "trigger_workload_bindings"} {
		t.Run(table, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			server := f.attachMCP(t)
			f.grant(t, urn.NewPrincipal(urn.PrincipalTypeUser, f.actor), authz.ScopeMCPRead, server.String())
			err := inTx(t, f.db, func(tx pgx.Tx) error {
				_, err := testIdentityService.Provision(t.Context(), failureTx{Tx: tx, table: table}, assistantidentity.ProvisionParams{OrganizationID: f.org, ProjectID: f.project, AssistantID: f.assistant, ActorUserID: f.actor})
				if err != nil {
					return fmt.Errorf("fixture operation: %w", err)
				}
				return nil
			})
			require.ErrorContains(t, err, "injected identity write failure")
			counts, err := repo.New(f.db).FixtureAuthorityCounts(t.Context(), f.org)
			require.NoError(t, err)
			require.Equal(t, repo.FixtureAuthorityCountsRow{}, counts)
		})
	}
}

func TestPolicyIsConfiguredActorSubsetAndFrozen(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.attachMCP(t)
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, f.actor)
	f.grant(t, actor, authz.ScopeMCPRead, "*")
	f.grant(t, actor, authz.ScopeSkillRead, "*")
	f.grant(t, actor, authz.ScopeOrgAdmin, "*")
	id := f.provision(t)
	before, err := testIdentityService.SnapshotCeiling(t.Context(), f.db, id)
	require.NoError(t, err)
	var decoded runtimepolicy.DelegatedPolicy
	require.NoError(t, json.Unmarshal(before.Policy, &decoded))
	require.NotEmpty(t, decoded.Effective)
	for _, grant := range decoded.Effective {
		require.Equal(t, server.String(), grant.Selector.ResourceID())
		require.Contains(t, []authz.Scope{authz.ScopeMCPRead, authz.ScopeMCPConnect}, grant.Scope)
	}
	frozen := string(before.Policy)
	f.grant(t, urn.NewPrincipal(urn.PrincipalTypeAgent, id.AgentID.String()), authz.ScopeMCPRead, "*")
	after, err := testIdentityService.SnapshotCeiling(t.Context(), f.db, id)
	require.NoError(t, err)
	require.Equal(t, frozen, string(after.Policy))
	require.Equal(t, before.Digest, after.Digest, "unconfigured expansion cannot widen a ceiling")
	require.Equal(t, runtimepolicy.CurrentDelegatedPolicyVersion, before.EncodingVersion)
	secondServer := f.attachMCP(t)
	expanded, err := testIdentityService.SnapshotCeiling(t.Context(), f.db, id)
	require.NoError(t, err)
	require.Contains(t, string(expanded.Policy), secondServer.String())
	require.NotEqual(t, before.Digest, expanded.Digest)
	require.NotEqual(t, frozen, string(expanded.Policy), "a later snapshot reflects newly configured capabilities without changing the captured policy")

}

func TestExplicitDenyCannotBecomeAssistantGrant(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.attachMCP(t)
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, f.actor)
	f.grant(t, actor, authz.ScopeMCPRead, "*")
	f.grant(t, actor, authz.ScopeMCPBlockedRead, server.String())
	f.grant(t, actor, authz.ScopeMCPBlockedConnect, server.String())
	id := f.provision(t)
	snapshot, err := testIdentityService.SnapshotCeiling(t.Context(), f.db, id)
	require.NoError(t, err)
	require.JSONEq(t, `{"requested":[],"effective":[]}`, string(snapshot.Policy))
}

func TestCreatorAndConsentProvenanceRemainDistinct(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	upgrader := "explicit-upgrader"
	require.NoError(t, repo.New(f.db).FixtureCreateUser(t.Context(), repo.FixtureCreateUserParams{ID: upgrader, Email: "upgrader@example.com"}))
	require.NoError(t, repo.New(f.db).FixtureCreateMembership(t.Context(), repo.FixtureCreateMembershipParams{OrganizationID: f.org, UserID: conv.ToPGText(upgrader)}))
	server := f.attachMCP(t)
	f.grant(t, urn.NewPrincipal(urn.PrincipalTypeUser, f.actor), authz.ScopeMCPRead, "*")
	require.NoError(t, inTx(t, f.db, func(tx pgx.Tx) error {
		_, err := testIdentityService.Upgrade(t.Context(), tx, assistantidentity.ProvisionParams{OrganizationID: f.org, ProjectID: f.project, AssistantID: f.assistant, ActorUserID: upgrader})
		if err != nil {
			return fmt.Errorf("fixture operation: %w", err)
		}
		return nil
	}))
	id := f.provision(t)
	owner, err := repo.New(f.db).FixtureAgentOwner(t.Context(), repo.FixtureAgentOwnerParams{OrganizationID: f.org, AgentID: id.AgentID})
	require.NoError(t, err)
	require.Equal(t, f.actor, owner)
	provenance, err := repo.New(f.db).FixtureProvisioningMetadata(t.Context(), repo.FixtureProvisioningMetadataParams{OrganizationID: f.org, SubjectID: f.assistant.String()})
	require.NoError(t, err)
	var metadata map[string]any
	require.NoError(t, json.Unmarshal(provenance, &metadata))
	require.Equal(t, f.actor, metadata["creator_user_id"])
	require.Equal(t, upgrader, metadata["consent_user_id"])
	require.Equal(t, upgrader, metadata["provisioning_actor_user_id"])
	snapshot, err := testIdentityService.SnapshotCeiling(t.Context(), f.db, id)
	require.NoError(t, err)
	require.NotContains(t, string(snapshot.Policy), server.String(), "creator policy must not replace actor consent")
}

func TestTenantAndMissingMappingFailClosed(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.provision(t)
	_, err := testIdentityService.Resolve(t.Context(), f.db, "another-organization", f.project, f.assistant, f.trigger)
	require.ErrorIs(t, err, assistantidentity.ErrNotFound)
	_, err = testIdentityService.Resolve(t.Context(), f.db, f.org, uuid.New(), f.assistant, f.trigger)
	require.ErrorIs(t, err, assistantidentity.ErrNotFound)
	_, err = testIdentityService.Resolve(t.Context(), f.db, f.org, f.project, f.assistant, uuid.New())
	require.ErrorIs(t, err, assistantidentity.ErrBrokenMapping)
	stale := id
	stale.AssistantGeneration++
	require.ErrorIs(t, testIdentityService.Validate(t.Context(), f.db, stale), assistantidentity.ErrInvalidIdentity)
	stale = id
	stale.TriggerGeneration++
	require.ErrorIs(t, testIdentityService.Validate(t.Context(), f.db, stale), assistantidentity.ErrInvalidIdentity)
	stale = id
	stale.TriggerGeneration++
	_, err = testIdentityService.SnapshotCeiling(t.Context(), f.db, stale)
	require.ErrorIs(t, err, assistantidentity.ErrInvalidIdentity)
}

func TestLiveAuthorityDependenciesAndHardDeletes(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"suspend", "owner barrier", "owner delete", "membership loss", "trigger pause", "issuer delete", "admission", "assignment", "hard assistant", "hard agent", "hard trigger", "hard issuer", "hard project"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			id := f.provision(t)
			q := repo.New(f.db)
			ctx := t.Context()
			var err error
			switch change {
			case "suspend":
				err = q.FixtureSuspendAgent(ctx, repo.FixtureSuspendAgentParams{OrganizationID: f.org, AgentID: id.AgentID})
			case "owner barrier":
				err = q.FixtureLatchOwner(ctx, repo.FixtureLatchOwnerParams{OrganizationID: f.org, AgentID: id.AgentID})
			case "owner delete":
				err = q.FixtureDeleteOwner(ctx, f.actor)
			case "membership loss":
				err = q.FixtureWithdrawMembership(ctx, repo.FixtureWithdrawMembershipParams{OrganizationID: f.org, UserID: conv.ToPGText(f.actor)})
			case "trigger pause":
				err = q.FixtureSetTriggerStatus(ctx, repo.FixtureSetTriggerStatusParams{OrganizationID: f.org, ProjectID: f.project, TriggerID: f.trigger, Status: "paused"})
			case "issuer delete":
				err = q.FixtureWithdrawIssuer(ctx, repo.FixtureWithdrawIssuerParams{OrganizationID: f.org, IssuerID: id.IssuerID})
			case "admission":
				err = q.RevokeAdmission(ctx, repo.RevokeAdmissionParams{OrganizationID: f.org, ProjectID: uuid.NullUUID{UUID: f.project, Valid: true}, IssuerID: id.IssuerID, Subject: id.Subject})
			case "assignment":
				err = q.FixtureWithdrawAssignment(ctx, repo.FixtureWithdrawAssignmentParams{OrganizationID: f.org, IssuerID: id.IssuerID, Subject: id.Subject})
			case "hard assistant":
				err = q.FixtureDeleteAssistant(ctx, repo.FixtureDeleteAssistantParams{OrganizationID: f.org, ProjectID: f.project, AssistantID: f.assistant})
			case "hard agent":
				err = q.FixtureDeleteAgent(ctx, repo.FixtureDeleteAgentParams{OrganizationID: f.org, AgentID: id.AgentID})
			case "hard trigger":
				err = q.FixtureDeleteTrigger(ctx, repo.FixtureDeleteTriggerParams{OrganizationID: f.org, ProjectID: f.project, TriggerID: f.trigger})
			case "hard issuer":
				err = q.FixtureDeleteIssuer(ctx, repo.FixtureDeleteIssuerParams{OrganizationID: f.org, IssuerID: id.IssuerID})
			case "hard project":
				err = q.FixtureDeleteProject(ctx, repo.FixtureDeleteProjectParams{OrganizationID: f.org, ProjectID: f.project})
			}
			require.NoError(t, err)
			require.Error(t, testIdentityService.Validate(ctx, f.db, id))
			resolution, err := testIdentityService.Resolve(ctx, f.db, f.org, f.project, f.assistant, f.trigger)
			require.NoError(t, err)
			require.NotEqual(t, assistantidentity.Active, resolution.State)
			require.NotEqual(t, assistantidentity.NeverConfigured, resolution.State)
			require.Nil(t, resolution.Identity)
			original, err := q.GetAssistantBinding(ctx, repo.GetAssistantBindingParams{OrganizationID: f.org, ProjectID: f.project, AssistantID: f.assistant})
			require.NoError(t, err)
			require.Equal(t, id.AgentID, original.OriginalAgentID)
		})
	}
}

func TestAssistantPauseResumePreservesIdentityAndPausedProvisioning(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	require.NoError(t, repo.New(f.db).FixtureSetAssistantStatus(t.Context(), repo.FixtureSetAssistantStatusParams{OrganizationID: f.org, ProjectID: f.project, AssistantID: f.assistant, Status: "paused"}))
	require.NoError(t, inTx(t, f.db, func(tx pgx.Tx) error {
		_, err := testIdentityService.Provision(t.Context(), tx, assistantidentity.ProvisionParams{OrganizationID: f.org, ProjectID: f.project, AssistantID: f.assistant, ActorUserID: f.actor})
		if err != nil {
			return fmt.Errorf("fixture operation: %w", err)
		}
		return nil
	}))
	result, err := testIdentityService.Resolve(t.Context(), f.db, f.org, f.project, f.assistant, f.trigger)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Unavailable, result.State)
	require.NoError(t, repo.New(f.db).FixtureSetAssistantStatus(t.Context(), repo.FixtureSetAssistantStatusParams{OrganizationID: f.org, ProjectID: f.project, AssistantID: f.assistant, Status: "active"}))
	id := f.provision(t)
	for _, status := range []string{"paused", "active"} {
		require.NoError(t, inTx(t, f.db, func(tx pgx.Tx) error {
			if err := repo.New(tx).FixtureSetAssistantStatus(t.Context(), repo.FixtureSetAssistantStatusParams{OrganizationID: f.org, ProjectID: f.project, AssistantID: f.assistant, Status: status}); err != nil {
				return fmt.Errorf("fixture operation: %w", err)
			}
			return nil
		}))
	}
	require.NoError(t, testIdentityService.Validate(t.Context(), f.db, id))
	result, err = testIdentityService.Resolve(t.Context(), f.db, f.org, f.project, f.assistant, f.trigger)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Active, result.State)
	require.Equal(t, id, *result.Identity)
	require.Equal(t, id.AssistantGeneration, result.Identity.AssistantGeneration)
	require.Equal(t, id.TriggerGeneration, result.Identity.TriggerGeneration)
}

func TestRootExplicitResumeAndPermanentAssistantTombstone(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.provision(t)
	require.NoError(t, inTx(t, f.db, func(tx pgx.Tx) error {
		return assistantidentity.TombstoneTrigger(t.Context(), tx, f.org, f.project, f.trigger)
	}))
	err := inTx(t, f.db, func(tx pgx.Tx) error {
		return testIdentityService.BindRootTrigger(t.Context(), tx, f.org, f.project, f.trigger)
	})
	require.ErrorIs(t, err, assistantidentity.ErrTombstoned)
	require.NoError(t, inTx(t, f.db, func(tx pgx.Tx) error {
		return testIdentityService.RetargetRootTrigger(t.Context(), tx, f.org, f.project, f.trigger)
	}))
	resumed, err := testIdentityService.Resolve(t.Context(), f.db, f.org, f.project, f.assistant, f.trigger)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Active, resumed.State)
	require.Equal(t, id.Subject, resumed.Identity.Subject)
	require.Equal(t, id.TriggerGeneration+1, resumed.Identity.TriggerGeneration)
	require.ErrorIs(t, testIdentityService.Validate(t.Context(), f.db, id), assistantidentity.ErrInvalidIdentity)
	require.NoError(t, inTx(t, f.db, func(tx pgx.Tx) error {
		return assistantidentity.TombstoneAssistant(t.Context(), tx, f.org, f.project, f.assistant)
	}))
	err = inTx(t, f.db, func(tx pgx.Tx) error {
		_, err := testIdentityService.Upgrade(t.Context(), tx, assistantidentity.ProvisionParams{OrganizationID: f.org, ProjectID: f.project, AssistantID: f.assistant, ActorUserID: f.actor})
		if err != nil {
			return fmt.Errorf("fixture operation: %w", err)
		}
		return nil
	})
	require.ErrorIs(t, err, assistantidentity.ErrTombstoned)
	live, err := repo.New(f.db).FixtureLiveAssignmentCount(t.Context(), repo.FixtureLiveAssignmentCountParams{OrganizationID: f.org, AgentID: id.AgentID})
	require.NoError(t, err)
	require.Zero(t, live)
}

func TestOrdinaryIssuerRenewalAndUntrustedActor(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.provision(t)
	require.NoError(t, repo.New(f.db).FixtureWithdrawIssuer(t.Context(), repo.FixtureWithdrawIssuerParams{OrganizationID: f.org, IssuerID: id.IssuerID}))
	root := uuid.New()
	require.NoError(t, repo.New(f.db).FixtureCreateRoot(t.Context(), repo.FixtureCreateRootParams{ID: root, OrganizationID: f.org, ProjectID: f.project, DefinitionSlug: "schedule", TargetRef: f.assistant.String()}))
	err := inTx(t, f.db, func(tx pgx.Tx) error {
		return testIdentityService.BindRootTrigger(t.Context(), tx, f.org, f.project, root)
	})
	require.NoError(t, err)
	require.Error(t, testIdentityService.Validate(t.Context(), f.db, id), "renewed trust cannot revive the old mapping")
	require.NoError(t, repo.New(f.db).FixtureWithdrawMembership(t.Context(), repo.FixtureWithdrawMembershipParams{OrganizationID: f.org, UserID: conv.ToPGText(f.actor)}))
	err = inTx(t, f.db, func(tx pgx.Tx) error {
		_, err := testIdentityService.Provision(t.Context(), tx, assistantidentity.ProvisionParams{OrganizationID: f.org, ProjectID: f.project, AssistantID: f.assistant, ActorUserID: f.actor})
		if err != nil {
			return fmt.Errorf("fixture operation: %w", err)
		}
		return nil
	})
	require.ErrorIs(t, err, assistantidentity.ErrActorIneligible)
}

func TestOperationalErrorsRemainErrors(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := testIdentityService.Resolve(ctx, f.db, f.org, f.project, f.assistant, f.trigger)
	require.Error(t, err)
	require.Empty(t, result.State)
}

func TestUnrepresentableToolExclusionDropsBroadCapability(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.attachMCP(t)
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, f.actor)
	f.grant(t, actor, authz.ScopeMCPRead, "*")
	selector := authz.NewSelector(authz.ScopeMCPBlockedConnect, server.String())
	selector[authz.SelectorKeyTool] = "denied-tool"
	encoded, err := json.Marshal(selector)
	require.NoError(t, err)
	_, err = accessrepo.New(f.db).InsertPrincipalGrantIfAbsent(t.Context(), accessrepo.InsertPrincipalGrantIfAbsentParams{OrganizationID: f.org, PrincipalUrn: actor, Scope: string(authz.ScopeMCPBlockedConnect), Selectors: encoded})
	require.NoError(t, err)
	id := f.provision(t)
	ceiling, err := testIdentityService.SnapshotCeiling(t.Context(), f.db, id)
	require.NoError(t, err)
	require.JSONEq(t, `{"requested":[],"effective":[]}`, string(ceiling.Policy))
}
