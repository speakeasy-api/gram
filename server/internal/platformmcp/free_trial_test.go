package platformmcp

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

func TestPendingFreeTrialLinksTheStartTrialPage(t *testing.T) {
	t.Parallel()

	base, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	pending := pendingFreeTrial(base)
	require.NotNil(t, pending)
	require.Equal(t, "https://app.example.test/start-trial", pending.StartURL)
	require.Equal(t, "This organization's free trial has not started yet. It starts once an organization admin adds a card at https://app.example.test/start-trial.", pending.Message)

	require.Nil(t, pendingFreeTrial(nil), "the notice exists to give the user the link")

	var status *FreeTrialStatus
	require.Nil(t, status.Pending(t.Context(), "org-1"), "an unconfigured status reports no pending trial")
}

func TestFreeTrialStatusNeedsFlagsAndASafeDashboardURL(t *testing.T) {
	t.Parallel()

	flags := &feature.InMemory{}
	safe, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	insecure, err := url.Parse("http://app.example.test")
	require.NoError(t, err)

	require.Nil(t, NewPostgresReader(testenv.NewLogger(t), nil).WithFreeTrialStatus(flags, safe, nil).freeTrial, "no database")

	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_free_trial_config")
	require.NoError(t, err)
	require.Nil(t, NewPostgresReader(testenv.NewLogger(t), conn).WithFreeTrialStatus(nil, safe, nil).freeTrial, "no flags")
	require.Nil(t, NewPostgresReader(testenv.NewLogger(t), conn).WithFreeTrialStatus(flags, insecure, nil).freeTrial, "unsafe dashboard URL")
	require.NotNil(t, NewPostgresReader(testenv.NewLogger(t), conn).WithFreeTrialStatus(flags, safe, nil).freeTrial)
}

func TestWhoamiCardOffersStartTrialOnlyWhilePending(t *testing.T) {
	t.Parallel()

	output := WhoamiOutput{User: WhoamiUser{Email: "ada@example.test"}, Roles: []WhoamiRole{}}
	require.Nil(t, whoamiCard(output).Action)

	output.FreeTrial = &PendingFreeTrial{StartURL: "https://app.example.test/start-trial", Message: "pending"}
	card := whoamiCard(output)
	require.Equal(t, &cardAction{Label: "Start free trial", URL: "https://app.example.test/start-trial"}, card.Action)
}

func TestFreeTrialStatusReportsOnlyOrganizationsAwaitingACard(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_free_trial_status")
	require.NoError(t, err)

	seedOrganization := func(t *testing.T, whitelisted bool) string {
		t.Helper()
		id := "org_" + uuid.NewString()
		_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
			ID: id, Name: "Free trial organization", Slug: "trial-" + uuid.NewString()[:8], WorkosID: pgtype.Text{},
			Whitelisted: pgtype.Bool{Bool: whitelisted, Valid: true},
		})
		require.NoError(t, err)
		return id
	}
	pending := seedOrganization(t, false)
	whitelisted := seedOrganization(t, true)
	trialing := seedOrganization(t, false)
	_, err = trialsrepo.New(conn).StartTrial(ctx, trialsrepo.StartTrialParams{Tier: "enterprise", StartForDays: 14, OrganizationID: trialing})
	require.NoError(t, err)
	flagOff := seedOrganization(t, false)

	flags := &feature.InMemory{}
	for _, id := range []string{pending, whitelisted, trialing} {
		flags.SetFlag(feature.FlagSelfServeTrialRequiresPaymentMethod, id, true)
	}
	dashboardURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	reader := NewPostgresReader(testenv.NewLogger(t), conn).WithFreeTrialStatus(flags, dashboardURL, nil)

	got := reader.freeTrial.Pending(ctx, pending)
	require.Equal(t, &PendingFreeTrial{
		StartURL: "https://app.example.test/start-trial",
		Message:  "This organization's free trial has not started yet. It starts once an organization admin adds a card at https://app.example.test/start-trial.",
	}, got)
	require.Nil(t, reader.freeTrial.Pending(ctx, whitelisted), "a whitelisted organization is already inside the product")
	require.Nil(t, reader.freeTrial.Pending(ctx, trialing), "an organization with a trial lifecycle does not wait for a card")
	require.Nil(t, reader.freeTrial.Pending(ctx, flagOff), "the rollout flag gates the notice")
}

func TestWhoamiAndPlatformContextReportPendingFreeTrial(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_free_trial_tools")
	require.NoError(t, err)
	principal, _ := seedRegistrationLifecycle(t, ctx, conn)
	seedAccessMember(t, ctx, conn, principal.OrganizationID, principal.UserID, "caller@example.test")

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagSelfServeTrialRequiresPaymentMethod, principal.OrganizationID, true)
	dashboardURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	reader := NewPostgresReader(testenv.NewLogger(t), conn).WithFreeTrialStatus(flags, dashboardURL, nil)

	whoami, err := NewWhoamiService(conn).WithFreeTrialStatus(reader.freeTrial).Whoami(ctx, principal)
	require.NoError(t, err)
	require.NotNil(t, whoami.FreeTrial)
	require.Equal(t, "https://app.example.test/start-trial", whoami.FreeTrial.StartURL)

	unconfigured, err := NewWhoamiService(conn).Whoami(ctx, principal)
	require.NoError(t, err)
	require.Nil(t, unconfigured.FreeTrial)
	encoded, err := json.Marshal(unconfigured)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "free_trial", "an organization not awaiting a card shows no free trial field")

	registrar := newRegistrar(newTestMCPServer())
	registerGetPlatformContextTool(registrar, reader.freeTrial)
	assistant := principal
	assistant.ConnectionID, assistant.Generation = "", ""
	assistant.ClientID = AssistantClientID
	assistant.Surface = SurfaceProjectAssistant
	result, err := descriptorByName(t, registrar, "get_platform_context").Invoke(ContextWithPrincipal(ctx, assistant), json.RawMessage(`{}`))
	require.NoError(t, err)
	platformContext, ok := result.(PlatformContext)
	require.True(t, ok)
	require.Equal(t, whoami.FreeTrial, platformContext.FreeTrial)
}
