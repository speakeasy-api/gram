package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth/deviceagenttoken"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// Reassigning a workload after admission must not pair the admitted agent's
// grants with another agent's identity.
func TestDeviceAgentToken_ActsAsTheAgentAdmissionGranted(t *testing.T) {
	t.Parallel()

	f := newDeviceAgentGrantFixture(t)
	w := f.exchangeDeviceAgent(t, f.assertion(t, f.tokenURL))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	token, ok := resp["access_token"].(string)
	require.True(t, ok)

	other := createConsentAgent(t, f.ctx, f.ti, f.fx, "Agent assigned after admission")
	reassignAfterAdmission := func(ctx context.Context, db *pgxpool.Pool) (authz.WorkloadSessionAdmission, error) {
		admission, err := runtimepolicy.AdmitWorkloadSession(ctx, db)
		if err != nil {
			return admission, fmt.Errorf("admit workload session: %w", err)
		}
		unassignWorkloadAgent(t, ctx, f.ti, f.assignmentID)
		assignAgentToWorkload(t, ctx, f.ti, f.fx.orgID, f.issuerID, f.subject, other.ID)
		return admission, nil
	}
	engine := authz.NewEngine(testenv.NewLogger(t), f.ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), authz.EngineOpts{
		AdmitPrincipalCredential:         runtimepolicy.AdmitPrincipalCredential,
		AdmitPrincipalCredentialWithDBTX: runtimepolicy.AdmitPrincipalCredentialWithDBTX,
		AdmitWorkloadSession:             reassignAfterAdmission,
		DevMode:                          false,
	})

	callerIssuer, err := sharedCallerIssuer()
	require.NoError(t, err)
	admitted, agent, err := deviceagenttoken.New(callerIssuer, f.ti.conn).Admit(t.Context(), token, engine)
	require.NoError(t, err)
	require.Equal(t, f.agentID.String(), agent.Principal.ID, "the request acts as the admitted agent, not the one assigned afterwards")

	effective, ok := contextvalues.EffectiveAgent(admitted)
	require.True(t, ok)
	require.Equal(t, f.agentID.String(), effective.ID)
}
