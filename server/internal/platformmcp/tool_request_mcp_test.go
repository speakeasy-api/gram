package platformmcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
)

type fakeAccessRequester struct {
	in     access.NotifyInput
	result access.NotifyResult
	err    error
	calls  int
}

func (f *fakeAccessRequester) Notify(_ context.Context, in access.NotifyInput) (access.NotifyResult, error) {
	f.calls++
	f.in = in
	return f.result, f.err
}

func TestMemberServerAdvertisesRequestTools(t *testing.T) {
	t.Parallel()

	_, reg := newMemberServer(nil, nil, "test-cursor-key", nil, &fakeAccessRequester{})
	got := make([]string, 0, len(reg.Descriptors()))
	for _, descriptor := range reg.Descriptors() {
		got = append(got, descriptor.Name)
	}
	require.Equal(t, []string{
		"get_platform_context",
		"search_mcp_catalog",
		"inspect_mcp_candidate",
		"request_mcp",
		"send_platform_mcp_feedback",
	}, got)
}

func TestNormalizeRequestMCPInput(t *testing.T) {
	t.Parallel()

	valid, err := normalizeRequestMCPInput(RequestMCPInput{Name: "Slack"})
	require.NoError(t, err)
	require.Equal(t, "Slack", valid.Name)

	_, err = normalizeRequestMCPInput(RequestMCPInput{Name: ""})
	require.ErrorIs(t, err, errRequestMCPInvalid)

	_, err = normalizeRequestMCPInput(RequestMCPInput{
		Name:        "Slack",
		ProviderKey: "slack",
		RemoteURL:   "https://remote.example.test/mcp",
	})
	require.ErrorIs(t, err, errRequestMCPInvalid)

	_, err = normalizeRequestMCPInput(RequestMCPInput{
		Name:       "Slack",
		CatalogRef: "catalog/slack",
	})
	require.ErrorIs(t, err, errRequestMCPInvalid)

	_, err = normalizeRequestMCPInput(RequestMCPInput{
		Name:      "Slack",
		RemoteURL: "http://remote.example.test/mcp",
	})
	require.ErrorIs(t, err, errRequestMCPInvalid)

	normalized, err := normalizeRequestMCPInput(RequestMCPInput{
		Name:        "Slack",
		ProviderKey: "slack",
		CatalogRef:  "catalog/slack",
		Reason:      "needed for support",
	})
	require.NoError(t, err)
	require.Equal(t, "slack", normalized.ProviderKey)
	require.Equal(t, "catalog/slack", normalized.CatalogRef)
	require.Equal(t, "needed for support", normalized.Reason)
}

func TestRequestMCPNotifiesAdmins(t *testing.T) {
	t.Parallel()

	requester := &fakeAccessRequester{result: access.NotifyResult{SentToCount: 2}}
	server, _ := newMemberServer(nil, nil, "test-cursor-key", nil, requester)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "request-mcp-test", Version: "0.0.1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	ctx := ContextWithPrincipal(t.Context(), testPrincipal())
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "request_mcp",
		Arguments: map[string]any{
			"name": "Slack",
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	require.Equal(t, 1, requester.calls)
	require.Equal(t, testPrincipal().OrganizationID, requester.in.OrganizationID)
	require.Equal(t, testPrincipal().UserID, requester.in.UserID)
	require.Equal(t, string(authz.ScopeMCPWrite), requester.in.Scope)
	require.Equal(t, "Slack", requester.in.ResourceName)
	require.Empty(t, requester.in.ResourceID)
}

func TestRequestMCPRejectsInvalidRemoteURL(t *testing.T) {
	t.Parallel()

	requester := &fakeAccessRequester{result: access.NotifyResult{SentToCount: 1}}
	server, _ := newMemberServer(nil, nil, "test-cursor-key", nil, requester)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "request-mcp-test", Version: "0.0.1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	ctx := ContextWithPrincipal(t.Context(), testPrincipal())
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "request_mcp",
		Arguments: map[string]any{
			"name":       "Slack",
			"remote_url": "http://remote.example.test/mcp",
		},
	})
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Zero(t, requester.calls)

	payload, err := json.Marshal(result.Content)
	require.NoError(t, err)
	require.Contains(t, string(payload), "invalid_request")
}
