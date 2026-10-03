package assistantidentity_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func TestBindingTransactionRejectsSameOrganizationForeignProjectAuthority(t *testing.T) {
	t.Parallel()
	for _, resource := range []string{"issuer", "agent"} {
		t.Run(resource, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			local := f.provision(t)
			q := repo.New(f.db)
			other := fixture{db: f.db, org: f.org, project: uuid.New(), assistant: uuid.New(), trigger: uuid.New(), actor: f.actor}
			project, err := projectsrepo.New(f.db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{Name: "Other identity project", Slug: "other-identity-project", OrganizationID: f.org})
			require.NoError(t, err)
			other.project = project.ID
			require.NoError(t, q.FixtureCreateAssistant(t.Context(), repo.FixtureCreateAssistantParams{ID: other.assistant, OrganizationID: other.org, ProjectID: other.project, Creator: conv.ToPGText(other.actor)}))
			require.NoError(t, q.FixtureCreateRoot(t.Context(), repo.FixtureCreateRootParams{ID: other.trigger, OrganizationID: other.org, ProjectID: other.project, DefinitionSlug: "dashboard", TargetRef: other.assistant.String()}))
			foreign := other.provision(t)
			require.NotEqual(t, local.IssuerID, foreign.IssuerID)
			require.NotEqual(t, local.AgentID, foreign.AgentID)
			// Use an unbound foreign agent so uniqueness does not mask the project FK.
			foreignAgent, err := agentrepo.New(f.db).CreateAgent(t.Context(), agentrepo.CreateAgentParams{OrganizationID: f.org, OwnerUserID: f.actor, ProjectID: uuid.NullUUID{UUID: other.project, Valid: true}, Name: "Unbound foreign agent"})
			require.NoError(t, err)
			binding, err := q.GetAssistantBinding(t.Context(), repo.GetAssistantBindingParams{CaptureSuspended: false, OrganizationID: f.org, ProjectID: f.project, AssistantID: f.assistant})
			require.NoError(t, err)
			root, unbound := uuid.New(), uuid.New()
			require.NoError(t, q.FixtureCreateAssistant(t.Context(), repo.FixtureCreateAssistantParams{ID: unbound, OrganizationID: f.org, ProjectID: f.project, Creator: conv.ToPGText(f.actor)}))
			require.NoError(t, q.FixtureCreateRoot(t.Context(), repo.FixtureCreateRootParams{ID: root, OrganizationID: f.org, ProjectID: f.project, DefinitionSlug: "cron", TargetRef: f.assistant.String()}))
			before, err := q.FixtureAuthorityCounts(t.Context(), f.org)
			require.NoError(t, err)
			// Even deliberately bypassing the service's pinned resource selection cannot
			// commit a binding with another project's issuer/agent. Earlier writes roll back.
			err = inTx(t, f.db, func(tx pgx.Tx) error {
				q := repo.New(tx)
				issuer, agent := local.IssuerID, local.AgentID
				if resource == "issuer" {
					issuer = foreign.IssuerID
				} else {
					agent = foreignAgent.ID
				}
				subject := "assistant-trigger:" + root.String()
				if err := q.CreateAdmission(t.Context(), repo.CreateAdmissionParams{OrganizationID: f.org, ProjectID: uuid.NullUUID{UUID: f.project, Valid: true}, IssuerID: issuer, Subject: subject}); err != nil {
					return fmt.Errorf("create test admission: %w", err)
				}
				if err := q.CreateAssignment(t.Context(), repo.CreateAssignmentParams{OrganizationID: f.org, IssuerID: issuer, Subject: subject, AgentID: agent}); err != nil {
					return fmt.Errorf("create test assignment: %w", err)
				}
				if resource == "agent" {
					_, err := q.CreateAssistantBinding(t.Context(), repo.CreateAssistantBindingParams{OrganizationID: f.org, ProjectID: f.project, AssistantID: unbound, AgentID: agent})
					if err != nil {
						return fmt.Errorf("bind foreign agent: %w", err)
					}
				} else {
					if err := q.CreateTriggerBinding(t.Context(), repo.CreateTriggerBindingParams{OrganizationID: f.org, ProjectID: f.project, TriggerID: root, AssistantBindingID: binding.ID, AssistantGeneration: binding.Generation, IssuerID: issuer, Subject: subject, Generation: 1}); err != nil {
						return fmt.Errorf("bind foreign issuer: %w", err)
					}
				}
				return nil
			})
			var fk *pgconn.PgError
			require.ErrorAs(t, err, &fk)
			require.Equal(t, "23503", fk.Code)
			if resource == "issuer" {
				require.Equal(t, "trigger_workload_bindings_issuer_fkey", fk.ConstraintName)
			} else {
				require.Equal(t, "assistant_agent_bindings_agent_fkey", fk.ConstraintName)
			}
			after, err := q.FixtureAuthorityCounts(t.Context(), f.org)
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.NoError(t, testIdentityService.Validate(t.Context(), f.db, local))
			// The public binding service also rejects a target from the other project.
			require.NoError(t, q.FixtureRetargetTrigger(t.Context(), repo.FixtureRetargetTriggerParams{OrganizationID: f.org, ProjectID: f.project, TriggerID: root, TargetRef: other.assistant.String()}))
			err = inTx(t, f.db, func(tx pgx.Tx) error {
				return testIdentityService.BindRootTrigger(t.Context(), tx, f.org, f.project, root)
			})
			require.ErrorIs(t, err, assistantidentity.ErrNotFound)
			after, err = q.FixtureAuthorityCounts(t.Context(), f.org)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}
