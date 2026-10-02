package remotesessions_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
)

func TestPreparationDCRPersistenceFailureRecordsIndeterminate(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"client insert", "grant update", "binding completion"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			var posts atomic.Int32
			registration := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				posts.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"client_id":"persistence-review-client","client_secret":"synthetic-secret","token_endpoint_auth_method":"client_secret_basic","grant_types":["urn:ietf:params:oauth:grant-type:jwt-bearer"]}`))
			}))
			t.Cleanup(registration.Close)
			ctx, ti, in, interactive := preparationDCRFixture(t, registration.URL)
			// Each fixture owns an isolated database. Fail one completion write, but
			// allow the independently committed pending-claim recovery update.
			var ddl string
			switch stage {
			case "client insert":
				ddl = `ALTER TABLE remote_session_clients ADD CONSTRAINT test_completion_failure CHECK (client_id <> 'persistence-review-client')`
			case "grant update":
				ddl = `ALTER TABLE remote_session_clients ADD CONSTRAINT test_completion_failure CHECK (client_id <> 'persistence-review-client' OR grant_types IS NULL)`
			case "binding completion":
				ddl = `ALTER TABLE remote_session_ema_bindings ADD CONSTRAINT test_completion_failure CHECK (state <> 'ready')`
			}
			_, err := ti.conn.Exec(ctx, ddl) //nolint:glint // notestingrawsql: DDL fault injection in an isolated per-test database; SQLc cannot add test-only constraints.
			require.NoError(t, err)
			result, err := ti.service.PrepareIdentityChaining(ctx, in)
			require.Error(t, err)
			require.NotNil(t, result)
			require.Equal(t, "indeterminate", result.State)
			require.False(t, result.Retryable)
			require.Equal(t, uuid.Nil, result.ClientID)
			auth, _ := contextvalues.GetAuthContext(ctx)
			q := repo.New(ti.conn)
			binding, err := q.GetEMABinding(ctx, repo.GetEMABindingParams{
				ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID,
				UserSessionIssuerID: in.UserSessionIssuerID, RemoteSessionIssuerID: in.RemoteSessionIssuerID, Resource: in.Resource,
			})
			require.NoError(t, err)
			require.Equal(t, "indeterminate", binding.State.String)
			require.Equal(t, result.Generation, binding.Generation)
			require.True(t, binding.ClaimID.Valid)
			require.False(t, binding.RemoteSessionClientID.Valid)
			restarted := restartPreparationService(t, ti)
			for range 2 {
				status, err := restarted.ReadIdentityChaining(ctx, in)
				require.NoError(t, err)
				require.Equal(t, "indeterminate", status.State)
				retry, err := restarted.PrepareIdentityChaining(ctx, in)
				require.NoError(t, err)
				require.Equal(t, "indeterminate", retry.State)
				require.False(t, retry.Retryable)
			}
			require.Equal(t, int32(1), posts.Load())
			assertPreparationInteractiveUntouched(t, ctx, ti, interactive, in.UserSessionIssuerID)

			// Recovery is a claim-scoped CAS, not an unconditional state overwrite.
			args := repo.MarkEMAClaimIndeterminateParams{ID: binding.ID, ProjectID: binding.ProjectID, OrganizationID: binding.OrganizationID, Generation: binding.Generation, ClaimID: binding.ClaimID}
			binding.State = conv.ToPGText("in_progress")
			_, err = q.SetEMABinding(ctx, repo.SetEMABindingParams{ID: binding.ID, ProjectID: binding.ProjectID, OrganizationID: binding.OrganizationID, Generation: binding.Generation, ExpectedGeneration: binding.Generation, State: binding.State, GrantSource: binding.GrantSource, RequestedScopes: binding.RequestedScopes, ClaimID: binding.ClaimID, ClaimedAt: binding.ClaimedAt})
			require.NoError(t, err)
			stale := args
			stale.Generation++
			rows, err := q.MarkEMAClaimIndeterminate(ctx, stale)
			require.NoError(t, err)
			require.Zero(t, rows)
			stale = args
			stale.ClaimID = conv.ToNullUUID(uuid.New())
			rows, err = q.MarkEMAClaimIndeterminate(ctx, stale)
			require.NoError(t, err)
			require.Zero(t, rows)
			rows, err = q.MarkEMAClaimIndeterminate(ctx, args)
			require.NoError(t, err)
			require.EqualValues(t, 1, rows)
			rows, err = q.MarkEMAClaimIndeterminate(ctx, args)
			require.NoError(t, err)
			require.Zero(t, rows)
		})
	}
}
