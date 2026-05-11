package memory

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/memory"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

func TestForgetTool_RequiresAssistantPrincipal(t *testing.T) {
	t.Parallel()

	tool := NewForgetTool(&fakeMemoryService{})

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

func TestForgetTool_HappyPathReturnsID(t *testing.T) {
	t.Parallel()

	memID := uuid.New()
	content := "the memory"
	fake := &fakeMemoryService{
		forgetResult: memory.ForgetResult{
			Forgotten:  true,
			ID:         &memID,
			Content:    &content,
			Reason:     "",
			Candidates: nil,
		},
	}
	tool := NewForgetTool(fake)

	assistantID := uuid.New()
	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-9",
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
	}, bytes.NewBufferString(`{"query":"the","tags":["t"]}`), &out)
	require.NoError(t, err)

	require.Equal(t, 1, fake.forgetCalls)
	require.Equal(t, assistantID, fake.gotAssist)
	require.Equal(t, projectID, fake.gotProject)
	require.Equal(t, "org-9", fake.gotOrg)
	require.Equal(t, "the", fake.gotQuery)
	require.Equal(t, []string{"t"}, fake.gotTags)

	var resp forgetOutput
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	require.True(t, resp.Forgotten)
	require.NotNil(t, resp.ID)
	require.Equal(t, memID.String(), *resp.ID)
	require.Empty(t, resp.Reason)
	require.Empty(t, resp.Candidates)
}

func TestForgetTool_AmbiguousReturnsCandidates(t *testing.T) {
	t.Parallel()

	c1ID := uuid.New()
	c2ID := uuid.New()
	fake := &fakeMemoryService{
		forgetResult: memory.ForgetResult{
			Forgotten: false,
			ID:        nil,
			Content:   nil,
			Reason:    "ambiguous",
			Candidates: []memory.ForgetCandidate{
				{ID: c1ID, Content: "one", Similarity: 0.92},
				{ID: c2ID, Content: "two", Similarity: 0.91},
			},
		},
	}
	tool := NewForgetTool(fake)

	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-9",
		ProjectID:            &projectID,
	})
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{
		AssistantID: uuid.New(),
		ThreadID:    uuid.New(),
	})

	var out bytes.Buffer
	err := tool.Call(ctx, toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}, bytes.NewBufferString(`{"query":"thing"}`), &out)
	require.NoError(t, err)

	var resp forgetOutput
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	require.False(t, resp.Forgotten)
	require.Equal(t, "ambiguous", resp.Reason)
	require.Len(t, resp.Candidates, 2)
	require.Equal(t, c1ID.String(), resp.Candidates[0].ID)
	require.Equal(t, c2ID.String(), resp.Candidates[1].ID)
}

func TestForgetTool_NoMatchReturnsReason(t *testing.T) {
	t.Parallel()

	fake := &fakeMemoryService{
		forgetResult: memory.ForgetResult{
			Forgotten:  false,
			ID:         nil,
			Content:    nil,
			Reason:     "no_match",
			Candidates: nil,
		},
	}
	tool := NewForgetTool(fake)

	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-9",
		ProjectID:            &projectID,
	})
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{
		AssistantID: uuid.New(),
		ThreadID:    uuid.New(),
	})

	var out bytes.Buffer
	err := tool.Call(ctx, toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
	}, bytes.NewBufferString(`{"query":"absent"}`), &out)
	require.NoError(t, err)

	var resp forgetOutput
	require.NoError(t, json.Unmarshal(out.Bytes(), &resp))
	require.False(t, resp.Forgotten)
	require.Equal(t, "no_match", resp.Reason)
	require.Nil(t, resp.ID)
	require.Empty(t, resp.Candidates)
}
