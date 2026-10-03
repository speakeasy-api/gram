package assistantidentity_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	policyrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
	"github.com/stretchr/testify/require"
)

type failingExecutionDB struct{ err error }

func (db failingExecutionDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return nil, db.err
}

func TestValidateExecutionNormalizesAuthorityNotInfrastructure(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"issuer keys", "issuer deleted", "binding missing", "binding tombstone"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			id := f.provision(t)
			ceiling, err := testIdentityService.SnapshotCeiling(t.Context(), f.db, id)
			require.NoError(t, err)
			e := assistantidentity.Execution{Version: 1, Identity: id, Issuer: testIdentityService.Issuer(), ThreadID: uuid.New(), EventID: "test-event", Mode: assistantidentity.ExecutionWorkload, Ceiling: ceiling}
			require.NoError(t, testIdentityService.ValidateExecution(t.Context(), f.db, e))
			wrongIssuer := e
			wrongIssuer.Issuer = "https://other.example"
			require.ErrorIs(t, testIdentityService.ValidateExecution(t.Context(), f.db, wrongIssuer), assistantidentity.ErrInvalidIdentity)
			stale := e
			stale.Identity.TriggerGeneration++
			require.ErrorIs(t, testIdentityService.ValidateExecution(t.Context(), f.db, stale), assistantidentity.ErrInvalidIdentity)
			missing := e
			missing.Identity.AssistantID = uuid.New()
			require.ErrorIs(t, testIdentityService.ValidateExecution(t.Context(), f.db, missing), assistantidentity.ErrInvalidIdentity)

			for _, failure := range []error{context.Canceled, context.DeadlineExceeded, errors.New("database connection unavailable")} {
				err := testIdentityService.ValidateExecution(t.Context(), failingExecutionDB{err: failure}, e)
				require.ErrorIs(t, err, failure)
				require.NotErrorIs(t, err, assistantidentity.ErrInvalidIdentity)
			}
			switch name {
			case "issuer keys":
				q := policyrepo.New(f.db)
				issuer, err := q.GetWorkloadIssuer(t.Context(), policyrepo.GetWorkloadIssuerParams{OrganizationID: f.org, ProjectID: uuid.NullUUID{UUID: f.project, Valid: true}, ID: id.IssuerID})
				require.NoError(t, err)
				_, err = q.UpdateWorkloadIssuer(t.Context(), policyrepo.UpdateWorkloadIssuerParams{OrganizationID: f.org, ProjectID: issuer.ProjectID, ID: issuer.ID, Name: issuer.Name, Description: issuer.Description, Tags: issuer.Tags, JwksUri: "https://other.example/keys"})
				require.NoError(t, err)
			case "issuer deleted":
				require.NoError(t, repo.New(f.db).FixtureDeleteIssuer(t.Context(), repo.FixtureDeleteIssuerParams{OrganizationID: f.org, IssuerID: id.IssuerID}))
			case "binding missing":
				e.Identity.TriggerID = uuid.New()
			case "binding tombstone":
				require.NoError(t, repo.New(f.db).TombstoneTriggerBinding(t.Context(), repo.TombstoneTriggerBindingParams{OrganizationID: f.org, ProjectID: f.project, TriggerID: f.trigger}))
			}
			require.ErrorIs(t, testIdentityService.ValidateExecution(t.Context(), f.db, e), assistantidentity.ErrInvalidIdentity)
		})
	}
}
