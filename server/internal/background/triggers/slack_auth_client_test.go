package triggers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authclients"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

func TestSlackBotTokenReadsSlackAppAuthClientAndFailsClosed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newIdentityFixture(t)

	enc := testenv.NewEncryptionClient(t)
	store := authclients.NewSlackAppCredentialStore(f.db, nil, enc)
	f.app.SetSlackAppAuthClientLoader(store)

	clientID, err := testrepo.New(f.db).CreateAuthClientFixture(ctx, testrepo.CreateAuthClientFixtureParams{
		OrganizationID: "org-trigger-test",
		ProjectID:      uuid.NullUUID{UUID: f.projectID, Valid: true},
		ClientType:     authz.AuthClientTypeSlackApp,
		Name:           "Backfill Slack App",
	})
	require.NoError(t, err)
	encBot, err := enc.Encrypt([]byte("xoxb-trigger-app-token"))
	require.NoError(t, err)
	require.NoError(t, testrepo.New(f.db).CreateAuthClientHeaderFixture(ctx, testrepo.CreateAuthClientHeaderFixtureParams{
		AuthClientID:   clientID,
		OrganizationID: "org-trigger-test",
		Name:           authclients.SlackHeaderBotToken,
		IsSecret:       true,
		Value:          pgtype.Text{String: encBot, Valid: true},
	}))

	authTrigger, err := triggerrepo.New(f.db).CreateTriggerInstance(ctx, triggerrepo.CreateTriggerInstanceParams{
		OrganizationID: "org-trigger-test",
		ProjectID:      f.projectID,
		DefinitionSlug: triggers.DefinitionSlugSlack,
		Name:           "Auth-client Slack",
		EnvironmentID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		AuthClientID:   uuid.NullUUID{UUID: clientID, Valid: true},
		TargetKind:     triggers.TargetKindAssistant,
		TargetRef:      f.assistantID.String(),
		TargetDisplay:  "Identity assistant",
		ConfigJson:     []byte(`{}`),
		Status:         triggers.StatusActive,
	})
	require.NoError(t, err)

	got, err := f.app.SlackBotTokenForTest(ctx, authTrigger.ID.String())
	require.NoError(t, err)
	require.Equal(t, "xoxb-trigger-app-token", got)

	// Soft-deleting the slack_app client fails closed.
	require.NoError(t, testrepo.New(f.db).SoftDeleteAuthClientFixture(ctx, testrepo.SoftDeleteAuthClientFixtureParams{
		ID:             clientID,
		OrganizationID: "org-trigger-test",
	}))
	_, err = f.app.SlackBotTokenForTest(ctx, authTrigger.ID.String())
	require.Error(t, err)

	// Hard-deleting sets auth_client_id NULL while environment_id is also NULL -> empty token, never falls back.
	require.NoError(t, testrepo.New(f.db).HardDeleteAuthClientFixture(ctx, testrepo.HardDeleteAuthClientFixtureParams{
		ID:             clientID,
		OrganizationID: "org-trigger-test",
	}))
	gotDeleted, err := f.app.SlackBotTokenForTest(ctx, authTrigger.ID.String())
	require.NoError(t, err)
	require.Empty(t, gotDeleted)
}
