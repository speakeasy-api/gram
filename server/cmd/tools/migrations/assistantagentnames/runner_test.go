package assistantagentnames_test

import (
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/assistantagentnames"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRun(t *testing.T) {
	pool := newTestDB(t)
	for _, scenario := range []string{"eligible", "custom", "prefix", "unbound", "deleted-agent", "deleted-assistant", "deleted-binding", "deleted-project", "other-org", "other-project", "mismatched-org", "mismatched-project", "missing-project-ref", "already-equal"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := t.Context()
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)
			defer func() { require.NoError(t, tx.Rollback(ctx)) }()
			// Temp tables isolate the query's selection rules, including malformed
			// historical associations that current foreign keys would reject.
			_, err = tx.Exec(ctx, `
CREATE TEMP TABLE agents (id uuid, organization_id text, project_id uuid, name text, deleted boolean DEFAULT false, updated_at timestamptz);
CREATE TEMP TABLE assistants (id uuid, organization_id text, project_id uuid, name text, deleted boolean DEFAULT false);
CREATE TEMP TABLE projects (id uuid, organization_id text, deleted boolean DEFAULT false);
CREATE TEMP TABLE assistant_agent_bindings (agent_id uuid, assistant_id uuid, organization_id text, project_id uuid, project_ref_id uuid, project_ref_organization_id text, deleted boolean DEFAULT false);`)
			require.NoError(t, err)
			agentID, assistantID, projectID := uuid.New(), uuid.New(), uuid.New()
			original := "Assistant " + assistantID.String()
			_, err = tx.Exec(ctx, `INSERT INTO projects VALUES ($1, 'org-test', false)`, projectID)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, `INSERT INTO agents (id, organization_id, project_id, name) VALUES ($1, 'org-test', $2, $3)`, agentID, projectID, original)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, `INSERT INTO assistants (id, organization_id, project_id, name) VALUES ($1, 'org-test', $2, 'Helpful assistant')`, assistantID, projectID)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, `INSERT INTO assistant_agent_bindings (agent_id, assistant_id, organization_id, project_id, project_ref_id, project_ref_organization_id) VALUES ($1, $2, 'org-test', $3, $3, 'org-test')`, agentID, assistantID, projectID)
			require.NoError(t, err)
			opts := assistantagentnames.Options{OrganizationID: "org-test", ProjectID: projectID.String()}
			mutations := map[string]string{
				"custom":              "UPDATE agents SET name = 'Custom name'",
				"prefix":              "UPDATE agents SET name = name || ' custom'",
				"unbound":             "UPDATE assistant_agent_bindings SET assistant_id = NULL",
				"deleted-agent":       "UPDATE agents SET deleted = true",
				"deleted-assistant":   "UPDATE assistants SET deleted = true",
				"deleted-binding":     "UPDATE assistant_agent_bindings SET deleted = true",
				"deleted-project":     "UPDATE projects SET deleted = true",
				"mismatched-org":      "UPDATE assistants SET organization_id = 'org-other'",
				"mismatched-project":  "UPDATE assistants SET project_id = '00000000-0000-0000-0000-000000000001'",
				"missing-project-ref": "UPDATE assistant_agent_bindings SET project_ref_id = NULL",
				"already-equal":       "UPDATE assistants SET name = (SELECT name FROM agents)",
			}
			if sql, ok := mutations[scenario]; ok {
				_, err = tx.Exec(ctx, sql)
				require.NoError(t, err)
			}
			if scenario == "other-org" {
				opts.OrganizationID = "org-other"
			}
			if scenario == "other-project" {
				opts.ProjectID = uuid.NewString()
			}
			var before string
			require.NoError(t, tx.QueryRow(ctx, "SELECT name FROM agents").Scan(&before))
			expected := int64(0)
			if scenario == "eligible" {
				expected = 1
			}
			report, err := assistantagentnames.Run(ctx, tx, opts)
			require.NoError(t, err)
			require.Equal(t, expected, report.Agents)
			var after string
			require.NoError(t, tx.QueryRow(ctx, "SELECT name FROM agents").Scan(&after))
			require.Equal(t, before, after, "dry run must not change names")
			opts.Apply = true
			report, err = assistantagentnames.Run(ctx, tx, opts)
			require.NoError(t, err)
			require.Equal(t, expected, report.Agents)
			require.NoError(t, tx.QueryRow(ctx, "SELECT name FROM agents").Scan(&after))
			if expected == 1 {
				require.Equal(t, "Helpful assistant", after)
			} else {
				require.Equal(t, before, after)
			}
			report, err = assistantagentnames.Run(ctx, tx, opts)
			require.NoError(t, err)
			require.Zero(t, report.Agents, "rerun must be a no-op")
		})
	}
}
