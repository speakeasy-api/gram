package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/stretchr/testify/require"
)

func TestATEOrdinaryWorkloadDoesNotRequireActorClaims(t *testing.T) {
	t.Parallel()
	for _, subject := range []string{"ordinary-subject", "actor/foo/name", "actor/foo/spk-kit-not-managed", "actor/foo/spk-kit-ABCDEF0123456789ABCDEF0123456789-0123456789abcdef"} {
		require.NoError(t, admitATEWorkloadGrantInTx(t.Context(), nil, workloadTenancy{}, presentedWorkload{subject: subject}))
	}
}

func TestATEReservedActorClassification(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	name := "spk-kit-" + strings.ReplaceAll(id.String(), "-", "") + "-0123456789abcdef"
	subject := "actor/preview/" + name
	parsed, managed := ateManagedRuntimeID(subject)
	require.True(t, managed)
	require.Equal(t, id, parsed)
	for _, ordinary := range []string{"actor/foo/name", "actor//" + name, subject + "/extra", subject + "x", "worker/preview/" + name, strings.Replace(subject, "abcdef", "ABCDEF", 1)} {
		_, managed := ateManagedRuntimeID(ordinary)
		require.False(t, managed, ordinary)
	}
	// Reserved subjects cannot fall back to ordinary admission even with no
	// tenancy (and therefore no database from which to establish a binding).
	require.ErrorIs(t, admitATEWorkloadGrantInTx(t.Context(), nil, workloadTenancy{}, presentedWorkload{subject: subject}), errWorkloadNotAdmitted)
}

func TestATEWorkloadAgentLifecycle(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		agent  agentrepo.Agent
		active bool
	}{
		{name: "active", agent: agentrepo.Agent{}, active: true},
		{name: "suspended", agent: agentrepo.Agent{SuspendedAt: pgtype.Timestamptz{Valid: true}}, active: false},
		{name: "revoked", agent: agentrepo.Agent{RevokedAt: pgtype.Timestamptz{Valid: true}}, active: false},
		{name: "deleted", agent: agentrepo.Agent{Deleted: true, DeletedAt: pgtype.Timestamptz{Valid: true}}, active: false},
		{name: "owner reassignment", agent: agentrepo.Agent{OwnerReassignmentRequiredAt: pgtype.Timestamptz{Valid: true}}, active: false},
	} {
		t.Run(tc.name, func(t *testing.T) { t.Parallel(); require.Equal(t, tc.active, ateWorkloadAgentActive(tc.agent)) })
	}
}

func TestATEActorBinding(t *testing.T) {
	t.Parallel()
	project, assistant, runtime, issuer := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	identity := assistantidentity.RuntimeIdentity{OrganizationID: "org_test", ProjectID: project, AssistantID: assistant, AgentID: uuid.New(), BindingID: uuid.New(), BindingGeneration: 1}
	actorName := "spk-kit-" + strings.ReplaceAll(runtime.String(), "-", "") + "-0123456789abcdef"
	subject := "actor/preview/" + actorName
	var metadata ateBrokerMetadata
	data := map[string]any{"runtime_id": runtime, "assistant_id": assistant, "project_id": project, "atespace": "preview", "threads": map[string]any{uuid.NewString(): map[string]any{"actor_name": actorName, "actor_uid": "original-uid", "workload": map[string]any{"issuer_id": issuer, "subject": subject, "identity": identity}}}}
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &metadata))
	var thread ateBrokerThread
	for _, v := range metadata.Threads {
		thread = v
	}
	var claims ateBrokerClaims
	require.NoError(t, json.Unmarshal([]byte(`{"actor.ate.dev":{"atespace":"preview","actorName":"`+actorName+`","actorUID":"original-uid"}}`), &claims))
	row := assistantrepo.AssistantRuntime{ID: runtime, ProjectID: project, AssistantID: assistant, Backend: "ate", RuntimeVersion: 3, State: "active"}
	require.True(t, matchATEActorBinding(row, metadata, thread, identity, issuer, subject, claims))
	row.BackendMetadataJson = raw
	tenancy := workloadTenancy{OrganizationID: identity.OrganizationID, ProjectID: project}
	resolved, err := resolveATEStoredBinding(row, tenancy, identity.AgentID, issuer, subject, claims)
	require.NoError(t, err)
	require.Equal(t, identity, resolved)
	for _, missing := range []string{`{}`, `{"threads":{}}`, `{"threads":{"thread":{"actor_name":"` + actorName + `","actor_uid":"original-uid"}}}`, `{"threads":{"thread":{"workload":{"issuer_id":"` + issuer.String() + `","subject":"other"}}}}`, `not-json`} {
		runtimeCopy := row
		runtimeCopy.BackendMetadataJson = []byte(missing)
		_, err := resolveATEStoredBinding(runtimeCopy, tenancy, identity.AgentID, issuer, subject, claims)
		require.ErrorIs(t, err, errWorkloadNotAdmitted)
	}
	parsed, managed := ateManagedRuntimeID(subject)
	require.True(t, managed)
	require.Equal(t, runtime, parsed)
	for _, tc := range []struct {
		name   string
		change func(*assistantrepo.AssistantRuntime, *ateBrokerMetadata, *ateBrokerThread, *ateBrokerClaims)
	}{
		{"recreated actor", func(_ *assistantrepo.AssistantRuntime, _ *ateBrokerMetadata, _ *ateBrokerThread, c *ateBrokerClaims) {
			c.Actor.ActorUID = "replacement-uid"
		}},
		{"missing uid", func(_ *assistantrepo.AssistantRuntime, _ *ateBrokerMetadata, _ *ateBrokerThread, c *ateBrokerClaims) {
			c.Actor.ActorUID = ""
		}},
		{"missing actor", func(_ *assistantrepo.AssistantRuntime, _ *ateBrokerMetadata, _ *ateBrokerThread, c *ateBrokerClaims) {
			c.Actor = nil
		}},
		{"wrong atespace", func(_ *assistantrepo.AssistantRuntime, _ *ateBrokerMetadata, _ *ateBrokerThread, c *ateBrokerClaims) {
			c.Actor.Atespace = "other"
		}},
		{"wrong name", func(_ *assistantrepo.AssistantRuntime, _ *ateBrokerMetadata, _ *ateBrokerThread, c *ateBrokerClaims) {
			c.Actor.ActorName = "other"
		}},
		{"worker for actor", func(_ *assistantrepo.AssistantRuntime, _ *ateBrokerMetadata, _ *ateBrokerThread, c *ateBrokerClaims) {
			c.Worker = json.RawMessage(`{}`)
		}},
		{"ended", func(r *assistantrepo.AssistantRuntime, _ *ateBrokerMetadata, _ *ateBrokerThread, _ *ateBrokerClaims) {
			r.Ended = true
		}},
		{"deleted", func(r *assistantrepo.AssistantRuntime, _ *ateBrokerMetadata, _ *ateBrokerThread, _ *ateBrokerClaims) {
			r.Deleted = true
		}},
		{"ended timestamp", func(r *assistantrepo.AssistantRuntime, _ *ateBrokerMetadata, _ *ateBrokerThread, _ *ateBrokerClaims) {
			r.EndedAt = pgtype.Timestamptz{Valid: true}
		}},
		{"inactive", func(r *assistantrepo.AssistantRuntime, _ *ateBrokerMetadata, _ *ateBrokerThread, _ *ateBrokerClaims) {
			r.State = "stopped"
		}},
		{"wrong backend", func(r *assistantrepo.AssistantRuntime, _ *ateBrokerMetadata, _ *ateBrokerThread, _ *ateBrokerClaims) {
			r.Backend = "gke"
		}},
		{"wrong version", func(r *assistantrepo.AssistantRuntime, _ *ateBrokerMetadata, _ *ateBrokerThread, _ *ateBrokerClaims) {
			r.RuntimeVersion = 2
		}},
		{"wrong metadata runtime", func(_ *assistantrepo.AssistantRuntime, m *ateBrokerMetadata, _ *ateBrokerThread, _ *ateBrokerClaims) {
			m.RuntimeID = uuid.New()
		}},
		{"wrong issuer", func(_ *assistantrepo.AssistantRuntime, _ *ateBrokerMetadata, th *ateBrokerThread, _ *ateBrokerClaims) {
			th.Workload.IssuerID = uuid.New()
		}},
		{"wrong subject", func(_ *assistantrepo.AssistantRuntime, _ *ateBrokerMetadata, th *ateBrokerThread, _ *ateBrokerClaims) {
			th.Workload.Subject = "other"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, m, th, c := row, metadata, thread, claims
			if c.Actor != nil {
				a := *c.Actor
				c.Actor = &a
			}
			if th.Workload != nil {
				w := *th.Workload
				th.Workload = &w
			}
			tc.change(&r, &m, &th, &c)
			require.False(t, matchATEActorBinding(r, m, th, identity, issuer, subject, c))
		})
	}
}
