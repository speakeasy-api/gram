package assistants

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authclients"
	"github.com/speakeasy-api/gram/server/internal/authz"
	envrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

type stubImageFetcher struct {
	images map[string]*slackapi.ImageFile

	// mu guards calls: fetchInlineImageParts downloads concurrently.
	mu    sync.Mutex
	calls []string
}

func (s *stubImageFetcher) FetchImageFile(_ context.Context, fileID string, _ string) (*slackapi.ImageFile, error) {
	s.mu.Lock()
	s.calls = append(s.calls, fileID)
	s.mu.Unlock()
	img, ok := s.images[fileID]
	if !ok {
		return nil, errors.New("file not found")
	}
	return img, nil
}

func stubImage(id string, size int) *slackapi.ImageFile {
	return &slackapi.ImageFile{
		FileID:   id,
		Name:     id + ".png",
		Title:    "",
		MimeType: "image/png",
		Data:     bytes.Repeat([]byte{0xAB}, size),
	}
}

func TestImageFileCandidatesFiltersAndCaps(t *testing.T) {
	t.Parallel()

	files := []slackFilePayload{
		{ID: "F1", Name: "", Title: "", Mimetype: "application/pdf", Size: 10, URLPrivateDownload: ""},
		{ID: "F2", Name: "", Title: "", Mimetype: "image/png", Size: 10, URLPrivateDownload: ""},
		{ID: "F3", Name: "", Title: "", Mimetype: "image/jpeg", Size: 10, URLPrivateDownload: ""},
		{ID: "F4", Name: "", Title: "", Mimetype: "image/gif", Size: 10, URLPrivateDownload: ""},
		{ID: "F5", Name: "", Title: "", Mimetype: "image/webp", Size: 10, URLPrivateDownload: ""},
		{ID: "F6", Name: "", Title: "", Mimetype: "image/png", Size: 10, URLPrivateDownload: ""},
		{ID: "", Name: "", Title: "", Mimetype: "image/png", Size: 10, URLPrivateDownload: ""},
	}

	got := imageFileCandidates(files)
	// Non-images and id-less entries drop; the trailing (newest) four stay.
	require.Len(t, got, maxTurnInlineImages)
	require.Equal(t, "F3", got[0].ID)
	require.Equal(t, "F6", got[3].ID)

	require.Empty(t, imageFileCandidates([]slackFilePayload{
		{ID: "F1", Name: "", Title: "", Mimetype: "text/plain", Size: 1, URLPrivateDownload: ""},
	}))
}

func TestFetchInlineImagePartsBuildsDataURIs(t *testing.T) {
	t.Parallel()

	fetcher := &stubImageFetcher{
		images: map[string]*slackapi.ImageFile{
			"F1": stubImage("F1", 16),
			"F2": stubImage("F2", 16),
		},
		calls: nil,
	}
	files := []slackFilePayload{
		{ID: "F1", Name: "", Title: "", Mimetype: "image/png", Size: 16, URLPrivateDownload: ""},
		{ID: "F2", Name: "", Title: "", Mimetype: "image/png", Size: 16, URLPrivateDownload: ""},
	}

	parts := fetchInlineImageParts(t.Context(), testenv.NewLogger(t), fetcher, "tok", files)
	require.Len(t, parts, 2)
	for _, part := range parts {
		require.Equal(t, contentPartTypeImageURL, part.Type)
		require.NotNil(t, part.ImageURL)
		require.Contains(t, part.ImageURL.URL, "data:image/png;base64,")
	}
}

func TestFetchInlineImagePartsSkipsFailuresAndContinues(t *testing.T) {
	t.Parallel()

	fetcher := &stubImageFetcher{
		images: map[string]*slackapi.ImageFile{"F2": stubImage("F2", 16)},
		calls:  nil,
	}
	files := []slackFilePayload{
		{ID: "F1", Name: "", Title: "", Mimetype: "image/png", Size: 16, URLPrivateDownload: ""},
		{ID: "F2", Name: "", Title: "", Mimetype: "image/png", Size: 16, URLPrivateDownload: ""},
	}

	parts := fetchInlineImageParts(t.Context(), testenv.NewLogger(t), fetcher, "tok", files)
	require.ElementsMatch(t, []string{"F1", "F2"}, fetcher.calls, "a failed download must not stop other files")
	require.Len(t, parts, 1)
}

func TestFetchInlineImagePartsEnforcesTurnByteBudget(t *testing.T) {
	t.Parallel()

	fetcher := &stubImageFetcher{
		images: map[string]*slackapi.ImageFile{
			// Declared sizes lie low so the budget check has to use actual
			// downloaded bytes.
			"F1": stubImage("F1", maxTurnInlineImageBytes-64),
			"F2": stubImage("F2", 128),
		},
		calls: nil,
	}
	files := []slackFilePayload{
		{ID: "F1", Name: "", Title: "", Mimetype: "image/png", Size: 1, URLPrivateDownload: ""},
		{ID: "F2", Name: "", Title: "", Mimetype: "image/png", Size: 1, URLPrivateDownload: ""},
		// Declared size over the whole turn budget: skipped without a
		// download. Sizes within the budget download concurrently and the
		// admit pass enforces the shared budget afterwards.
		{ID: "F3", Name: "", Title: "", Mimetype: "image/png", Size: maxTurnInlineImageBytes + 1, URLPrivateDownload: ""},
	}

	parts := fetchInlineImageParts(t.Context(), testenv.NewLogger(t), fetcher, "tok", files)
	require.Len(t, parts, 1, "second image exceeds the remaining budget after the first")
	require.ElementsMatch(t, []string{"F1", "F2"}, fetcher.calls, "budget-exceeding declared size must skip the download")
}

type stubEnvLoader map[toolconfig.SlugOrID]map[string]string

func (s stubEnvLoader) Load(_ context.Context, _ uuid.UUID, ref toolconfig.SlugOrID) (map[string]string, error) {
	return s[ref], nil
}

func (s stubEnvLoader) LoadSystemEnv(context.Context, uuid.UUID, uuid.UUID, string, string) (*toolconfig.CaseInsensitiveEnv, error) {
	return toolconfig.CIEnvFrom(nil), nil
}

func TestSlackTriggerTokenReadsSlackAppAuthClientAndFailsClosed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_slack_token")
	require.NoError(t, err)
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:             "org-test",
		Name:           "Test Org",
		Slug:           "test-org",
		WorkosID:       pgtype.Text{},
		Whitelisted:    pgtype.Bool{},
		CreationSource: pgtype.Text{},
	})
	require.NoError(t, err)
	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)

	enc := testenv.NewEncryptionClient(t)
	store := authclients.NewSlackAppCredentialStore(conn, newTestAuthzEngine(t, conn), enc)

	// Create a slack_app auth client with an encrypted bot token.
	clientID, err := testrepo.New(conn).CreateAuthClientFixture(ctx, testrepo.CreateAuthClientFixtureParams{
		OrganizationID: "org-test",
		ProjectID:      uuid.NullUUID{UUID: projectID, Valid: true},
		ClientType:     authz.AuthClientTypeSlackApp,
		Name:           "Turn Images Slack",
	})
	require.NoError(t, err)
	encBot, err := enc.Encrypt([]byte("xoxb-from-auth-client"))
	require.NoError(t, err)
	require.NoError(t, testrepo.New(conn).CreateAuthClientHeaderFixture(ctx, testrepo.CreateAuthClientHeaderFixtureParams{
		AuthClientID:   clientID,
		OrganizationID: "org-test",
		Name:           authclients.SlackHeaderBotToken,
		IsSecret:       true,
		Value:          pgtype.Text{String: encBot, Valid: true},
	}))

	// Create an auth-client-backed Slack trigger and an environment-backed Slack trigger.
	authTrigger, err := triggerrepo.New(conn).CreateTriggerInstance(ctx, triggerrepo.CreateTriggerInstanceParams{
		OrganizationID: "org-test",
		ProjectID:      projectID,
		DefinitionSlug: "slack",
		Name:           "Auth-client Slack",
		EnvironmentID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		AuthClientID:   uuid.NullUUID{UUID: clientID, Valid: true},
		TargetKind:     "assistant",
		TargetRef:      assistantID.String(),
		TargetDisplay:  "Assistant",
		ConfigJson:     []byte(`{}`),
		Status:         "active",
	})
	require.NoError(t, err)

	envRow, err := envrepo.New(conn).CreateEnvironment(ctx, envrepo.CreateEnvironmentParams{
		OrganizationID: "org-test",
		ProjectID:      projectID,
		Name:           "Slack Env",
		Slug:           "slack-env",
		Description:    pgtype.Text{},
	})
	require.NoError(t, err)
	envTrigger, err := triggerrepo.New(conn).CreateTriggerInstance(ctx, triggerrepo.CreateTriggerInstanceParams{
		OrganizationID: "org-test",
		ProjectID:      projectID,
		DefinitionSlug: "slack",
		Name:           "Environment Slack",
		EnvironmentID:  uuid.NullUUID{UUID: envRow.ID, Valid: true},
		AuthClientID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TargetKind:     "assistant",
		TargetRef:      assistantID.String(),
		TargetDisplay:  "Assistant",
		ConfigJson:     []byte(`{}`),
		Status:         "active",
	})
	require.NoError(t, err)

	core := NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendGKE, runTurnErr: nil}, nil, nil, nil, telemetry.NewStub(testenv.NewLogger(t)), nil, newTestAuditLogger(), testIdentityService, newTestAuthzEngine(t, conn))
	core.SetSlackAppCredentials(store)
	core.envLoader = stubEnvLoader{toolconfig.ID(envRow.ID): {"SLACK_BOT_TOKEN": "xoxb-from-env"}}

	thread := assistantThreadRecord{ID: threadID, ProjectID: projectID}

	// Auth-client trigger reads exclusively from its slack_app client.
	gotAuth, err := core.slackTriggerToken(ctx, thread, assistantThreadEventRecord{
		TriggerInstanceID: uuid.NullUUID{UUID: authTrigger.ID, Valid: true},
	})
	require.NoError(t, err)
	require.Equal(t, "xoxb-from-auth-client", gotAuth)

	// Environment-backed trigger reads from its environment.
	gotEnv, err := core.slackTriggerToken(ctx, thread, assistantThreadEventRecord{
		TriggerInstanceID: uuid.NullUUID{UUID: envTrigger.ID, Valid: true},
	})
	require.NoError(t, err)
	require.Equal(t, "xoxb-from-env", gotEnv)

	// Soft-deleting the slack_app client causes the auth-client trigger to fail closed.
	require.NoError(t, testrepo.New(conn).SoftDeleteAuthClientFixture(ctx, testrepo.SoftDeleteAuthClientFixtureParams{
		ID:             clientID,
		OrganizationID: "org-test",
	}))
	_, err = core.slackTriggerToken(ctx, thread, assistantThreadEventRecord{
		TriggerInstanceID: uuid.NullUUID{UUID: authTrigger.ID, Valid: true},
	})
	require.Error(t, err)

	// Hard-deleting sets auth_client_id NULL while environment_id is also NULL -> returns "" (never falls back to stale env).
	require.NoError(t, testrepo.New(conn).HardDeleteAuthClientFixture(ctx, testrepo.HardDeleteAuthClientFixtureParams{
		ID:             clientID,
		OrganizationID: "org-test",
	}))
	gotDeleted, err := core.slackTriggerToken(ctx, thread, assistantThreadEventRecord{
		TriggerInstanceID: uuid.NullUUID{UUID: authTrigger.ID, Valid: true},
	})
	require.NoError(t, err)
	require.Empty(t, gotDeleted)
}
