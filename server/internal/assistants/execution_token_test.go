package assistants

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
)

func newTestExecutionIssuer(t *testing.T) *mcpauthz.Issuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	private, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	issuer, err := mcpauthz.New(
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})),
		testIdentityService.Issuer(), false,
	)
	require.NoError(t, err)
	return issuer
}

func TestExecutionTokenRevalidatesLiveIdentity(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "execution_token")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "execution-token")
	seedProjectRead(t, db, "user-1", project)
	core := newProvisioningCore(t, db)
	record, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Execution token", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive)
	require.NoError(t, err)
	trigger, err := core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, record.ID, record.Name)
	require.NoError(t, err)
	thread := seedThreadWithEvent(t, db, record.ID, "execution-token", "execution-token", eventStatusPending)

	resolution, err := testIdentityService.Resolve(t.Context(), db, "org-test", project, record.ID, trigger)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Active, resolution.State)
	ceiling, err := testIdentityService.SnapshotCeiling(t.Context(), db, *resolution.Identity)
	require.NoError(t, err)
	execution := assistantidentity.Execution{
		Version: assistantidentity.ExecutionVersion, Identity: *resolution.Identity, Issuer: testIdentityService.Issuer(),
		ThreadID: thread, EventID: "evt-execution-token", HumanUserID: "user-1", Ceiling: ceiling,
	}
	target := assistanttokens.ExecutionTarget{EventID: execution.EventID, OrganizationID: "org-test", ProjectID: project, AssistantID: record.ID, ThreadID: thread}
	manager := assistanttokens.New("test-secret", db, newTestAuthzEngine(t, db), newTestExecutionIssuer(t), testIdentityService)

	raw, err := manager.GenerateExecution(t.Context(), execution)
	require.NoError(t, err)
	got, err := manager.ValidateExecution(t.Context(), raw, target)
	require.NoError(t, err)
	require.Equal(t, execution.Identity, got.Identity)

	otherEvent := target
	otherEvent.EventID = "evt-other"
	_, err = manager.ValidateExecution(t.Context(), raw, otherEvent)
	require.ErrorIs(t, err, assistantidentity.ErrInvalidIdentity)

	withoutAccess := execution
	withoutAccess.HumanUserID = "user-2"
	_, err = manager.GenerateExecution(t.Context(), withoutAccess)
	require.ErrorIs(t, err, assistantidentity.ErrActorIneligible)

	_, err = agentrepo.New(db).SuspendAgent(t.Context(), agentrepo.SuspendAgentParams{OrganizationID: "org-test", ID: uuid.MustParse(*record.AgentID)})
	require.NoError(t, err)
	_, err = manager.ValidateExecution(t.Context(), raw, target)
	require.ErrorIs(t, err, assistantidentity.ErrInvalidIdentity)
}
