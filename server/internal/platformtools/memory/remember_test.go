package memory

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/memory"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

func TestRememberTool_RequiresAssistantPrincipal(t *testing.T) {
	t.Parallel()

	tool := NewRememberTool(&fakeMemoryService{})

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
	}, bytes.NewBufferString(`{"content":"hello"}`), &bytes.Buffer{})
	require.Error(t, err)

	var shaped *oops.ShareableError
	require.ErrorAs(t, err, &shaped)
	require.Equal(t, oops.CodeUnauthorized, shaped.Code)
}

func TestRememberTool_DelegatesAndShapesResponse(t *testing.T) {
	t.Parallel()

	assistantID := uuid.New()
	projectID := uuid.New()
	memID := uuid.New()
	supersededID := uuid.New()
	created := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)

	fake := &fakeMemoryService{
		rememberResult: memory.RememberResult{
			ID:           memID,
			CreatedAt:    created,
			Deduped:      false,
			SupersededID: &supersededID,
		},
	}
	tool := NewRememberTool(fake)

	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-42",
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
	}, bytes.NewBufferString(`{"content":"recall this","tags":["a","b"]}`), &out)
	require.NoError(t, err)

	require.Equal(t, 1, fake.rememberCalls)
	require.Equal(t, assistantID, fake.gotAssist)
	require.Equal(t, projectID, fake.gotProject)
	require.Equal(t, "org-42", fake.gotOrg)
	require.Equal(t, "recall this", fake.gotContent)
	require.Equal(t, []string{"a", "b"}, fake.gotTags)

	var resp rememberOutput
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	require.Equal(t, memID.String(), resp.ID)
	require.False(t, resp.Deduped)
	require.NotNil(t, resp.SupersededID)
	require.Equal(t, supersededID.String(), *resp.SupersededID)
}

func TestRememberTool_PropagatesServiceError(t *testing.T) {
	t.Parallel()

	fake := &fakeMemoryService{rememberErr: errors.New("boom")}
	tool := NewRememberTool(fake)

	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-1",
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
	}, bytes.NewBufferString(`{"content":"x"}`), &bytes.Buffer{})
	require.Error(t, err)
	require.ErrorContains(t, err, "boom")
}
