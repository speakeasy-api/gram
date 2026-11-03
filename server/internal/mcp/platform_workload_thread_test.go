package mcp_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

type workloadThreadProbe struct {
	calls     int
	principal contextvalues.AssistantPrincipal
	actor     urn.Principal
	project   uuid.UUID
	chatID    string
}

func (*workloadThreadProbe) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		ThreadContext: core.ThreadContextRequired, SourceSlug: "skills", HandlerName: "thread_probe", Name: "thread_probe",
		Description: "Observe selected thread context", InputSchema: []byte(`{"type":"object","properties":{},"additionalProperties":false}`),
		Variables: nil, Annotations: core.ReadOnlyAnnotations(), Managed: true, OwnerKind: nil, OwnerID: nil,
	}
}

func (p *workloadThreadProbe) Call(ctx context.Context, env toolconfig.ToolCallEnv, _ io.Reader, wr io.Writer) error {
	p.calls++
	p.principal, _ = contextvalues.GetAssistantPrincipal(ctx)
	p.actor, _ = contextvalues.AuthenticatedActor(ctx)
	auth, _ := contextvalues.GetAuthContext(ctx)
	if auth != nil && auth.ProjectID != nil {
		p.project = *auth.ProjectID
	}
	p.chatID = env.GramChatID
	return core.EncodeResult(wr, map[string]bool{"observed": true})
}

func TestPlatformWorkloadExplicitThreadHTTP(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	issuer := f.sharedIssuer(t)
	slug := platformtools.AssistantsPlatformToolsetSlug
	probe := &workloadThreadProbe{}
	mcp.SetPlatformThreadToolForTest(f.ti.service.Service, slug, probe)
	token := platformWorkloadToken(t, f, issuer, slug)
	queries := assistantrepo.New(f.ti.conn)
	assistant, err := queries.CreateAssistant(t.Context(), assistantrepo.CreateAssistantParams{
		ProjectID: f.fx.target.ProjectID, OrganizationID: f.fx.orgID, CreatedByUserID: pgtype.Text{},
		Name: "Explicit thread target", Model: "openai/gpt-4o-mini", Instructions: "", WarmTtlSeconds: 300, MaxConcurrency: 1, Status: "active",
	})
	require.NoError(t, err)
	chatID := uuid.New()
	require.NoError(t, queries.UpsertAssistantChat(t.Context(), assistantrepo.UpsertAssistantChatParams{
		ChatID: chatID, ProjectID: f.fx.target.ProjectID, OrganizationID: f.fx.orgID, UserID: pgtype.Text{}, Title: pgtype.Text{},
	}))
	threadID, err := queries.UpsertAssistantThread(t.Context(), assistantrepo.UpsertAssistantThreadParams{
		AssistantID: assistant.ID, ProjectID: f.fx.target.ProjectID, CorrelationID: uuid.NewString(), ChatID: chatID, SourceKind: "dashboard", SourceRefJson: []byte(`{}`),
	})
	require.NoError(t, err)

	listed, err := servePlatformHTTP(t, f.ti, slug, toolsListBody(), token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	var catalog struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				InputSchema struct {
					Properties map[string]json.RawMessage `json:"properties"`
					Required   []string                   `json:"required"`
				} `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(listed.Body.Bytes(), &catalog))
	require.Len(t, catalog.Result.Tools, 1)
	require.Equal(t, "thread_probe", catalog.Result.Tools[0].Name)
	require.Contains(t, catalog.Result.Tools[0].InputSchema.Properties, "thread_id")
	require.Contains(t, catalog.Result.Tools[0].InputSchema.Required, "thread_id")
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "thread_probe", "arguments": map[string]string{"thread_id": threadID.String()}}})
	require.NoError(t, err)
	denied, callErr := servePlatformHTTP(t, f.ti, slug, body, token)
	require.True(t, callErr != nil || denied.Code >= 400 || jsonToolCallFailed(denied.Body.Bytes()), "missing assistant:read must reject: %s", denied.Body.String())
	require.Zero(t, probe.calls, "unauthorized thread selection must not invoke the executor")

	// The same token gains access only through existing live agent/owner policy.
	seedPrincipalGrant(t, t.Context(), f.ti, f.fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), authz.ScopeAssistantRead, assistant.ID.String())
	seedPrincipalGrant(t, t.Context(), f.ti, f.fx.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, f.fx.userID), authz.ScopeAssistantRead, assistant.ID.String())
	response, err := servePlatformHTTP(t, f.ti, slug, body, token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.False(t, jsonToolCallFailed(response.Body.Bytes()), response.Body.String())
	require.Equal(t, 1, probe.calls)
	require.Equal(t, contextvalues.AssistantPrincipal{AssistantID: assistant.ID, ThreadID: threadID}, probe.principal)
	require.Equal(t, urn.NewWorkloadPrincipal(f.issuerID, f.subject), probe.actor, "thread selection must preserve the authenticated workload")
	require.Equal(t, f.fx.target.ProjectID, probe.project)
	require.Equal(t, chatID.String(), probe.chatID)
}

func jsonToolCallFailed(body []byte) bool {
	var response struct {
		Error  json.RawMessage `json:"error"`
		Result struct {
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &response) != nil {
		return true
	}
	return len(response.Error) != 0 || response.Result.IsError
}
