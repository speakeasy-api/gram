package memory

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/memory"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

func TestRecallTool_RequiresAssistantPrincipal(t *testing.T) {
	t.Parallel()

	tool := NewRecallTool(&fakeMemoryService{})

	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-1",
		ProjectID:            &projectID,
	})

	err := tool.Call(ctx, toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}, bytes.NewBufferString(`{"query":"x"}`), &bytes.Buffer{})
	require.Error(t, err)

	var shaped *oops.ShareableError
	require.ErrorAs(t, err, &shaped)
	require.Equal(t, oops.CodeUnauthorized, shaped.Code)
}

func TestRecallTool_DefaultsLimitAndShapesResponse(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	memID := uuid.New()

	sourceKind := "slack"
	sourceUser := "U123"
	sourceChannel := "C456"
	sourceTime := time.Date(2026, 6, 12, 9, 30, 0, 0, time.UTC)

	fake := &fakeMemoryService{
		recallResult: []memory.RecallResult{
			{
				ID:              memID,
				Content:         "lorem",
				Tags:            []string{"x"},
				Score:           0.87,
				Similarity:      0.95,
				CreatedAt:       created,
				SourceKind:      &sourceKind,
				SourceUserID:    &sourceUser,
				SourceChannel:   &sourceChannel,
				SourceTimestamp: &sourceTime,
			},
		},
	}
	tool := NewRecallTool(fake)

	assistantID := uuid.New()
	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-7",
		ProjectID:            &projectID,
	})
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{
		AssistantID: assistantID,
		ThreadID:    uuid.New(),
	})

	var out bytes.Buffer
	err := tool.Call(ctx, toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}, bytes.NewBufferString(`{"query":"hello","tags":["x"]}`), &out)
	require.NoError(t, err)

	require.Equal(t, 1, fake.recallCalls)
	require.Equal(t, assistantID, fake.gotAssist)
	require.Equal(t, "org-7", fake.gotOrg)
	require.Equal(t, "hello", fake.gotQuery)
	require.Equal(t, recallDefaultLimit, fake.gotLimit)
	require.Equal(t, []string{"x"}, fake.gotTags)

	var resp []recallEntry
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	require.Len(t, resp, 1)
	require.Equal(t, memID.String(), resp[0].ID)
	require.Equal(t, "lorem", resp[0].Content)
	require.InDelta(t, 0.87, resp[0].Score, 1e-9)
	require.Equal(t, []string{"x"}, resp[0].Tags)
	require.NotNil(t, resp[0].Source)
	require.Equal(t, "from slack user U123 in C456, 2026-06-12", *resp[0].Source)
}

func TestFormatSourceWithoutProvenanceIsNil(t *testing.T) {
	t.Parallel()

	require.Nil(t, formatSource(memory.RecallResult{
		ID:              uuid.New(),
		Content:         "no provenance",
		Tags:            nil,
		Score:           0.5,
		Similarity:      0.5,
		CreatedAt:       time.Now(),
		SourceKind:      nil,
		SourceUserID:    nil,
		SourceChannel:   nil,
		SourceTimestamp: nil,
	}))
}

func TestFormatSourceCronUsesTriggerChannelOnly(t *testing.T) {
	t.Parallel()

	kind := "cron"
	trigger := "f0e9d8c7-0000-0000-0000-000000000001"
	ts := time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)

	got := formatSource(memory.RecallResult{
		ID:              uuid.New(),
		Content:         "automated fact",
		Tags:            nil,
		Score:           0.5,
		Similarity:      0.5,
		CreatedAt:       ts,
		SourceKind:      &kind,
		SourceUserID:    nil,
		SourceChannel:   &trigger,
		SourceTimestamp: &ts,
	})
	require.NotNil(t, got)
	require.Equal(t, "from cron in f0e9d8c7-0000-0000-0000-000000000001, 2026-06-12", *got)
}

func TestRecallTool_HonorsExplicitLimit(t *testing.T) {
	t.Parallel()

	fake := &fakeMemoryService{}
	tool := NewRecallTool(fake)

	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-2",
		ProjectID:            &projectID,
	})
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{
		AssistantID: uuid.New(),
		ThreadID:    uuid.New(),
	})

	err := tool.Call(ctx, toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}, bytes.NewBufferString(`{"query":"x","limit":3}`), &bytes.Buffer{})
	require.NoError(t, err)

	require.Equal(t, 3, fake.gotLimit)
}
