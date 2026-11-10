package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const hostedMemberRefusalMessage = `server "support" is hosted by Speakeasy, and hosted servers can't be used through a gateway. Connect to the server's own MCP URL instead.`

// toolCallResultEnvelope decodes a tools/call result's text blocks.
type toolCallResultEnvelope struct {
	Result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	} `json:"result"`
}

func hostedRefusalGate() *metaGateContext {
	return &metaGateContext{hostedMemberSlugs: []string{"support"}}
}

func hostedRefusalRequest() *rawRequest {
	return &rawRequest{JSONRPC: "2.0", ID: mcpjsonrpc.StringID("1"), Method: "tools/call"}
}

// A call naming a stored gateway's hosted member answers with a tool error
// saying why, not an unknown-server protocol error, and the attempt records
// the refusal for the gateway's tool-call log.
func TestDispatchHostedGatewayMemberIsToolError(t *testing.T) {
	t.Parallel()

	s := &Service{logger: testenv.NewLogger(t)}
	attempt := newMetaToolCallAttempt(time.Now())
	body, err := s.dispatchMetaMemberTool(t.Context(), s.logger, hostedRefusalGate(), nil, hostedRefusalRequest(), "support--create_ticket", json.RawMessage(`{}`), nil, attempt)
	require.NoError(t, err)

	var envelope toolCallResultEnvelope
	require.NoError(t, json.Unmarshal(body, &envelope))
	require.True(t, envelope.Result.IsError)
	require.Len(t, envelope.Result.Content, 1)
	require.Equal(t, hostedMemberRefusalMessage, envelope.Result.Content[0].Text)
	require.Equal(t, http.StatusNotFound, attempt.status)
	require.Equal(t, hostedMemberRefusalMessage, attempt.message)
	require.Nil(t, attempt.member)
}

// Code Mode reports the refusal as a rejected call carrying the reason, so
// the program can stop calling that server. The reason leaves the slug out.
func TestDispatchHostedGatewayMemberRejectsCodeModeCall(t *testing.T) {
	t.Parallel()

	s := &Service{logger: testenv.NewLogger(t)}
	invocation := &metaCodeInvocation{}
	ctx := context.WithValue(t.Context(), metaCodeInvocationKey{}, invocation)
	_, err := s.dispatchMetaMemberTool(ctx, s.logger, hostedRefusalGate(), nil, hostedRefusalRequest(), "support--create_ticket", json.RawMessage(`{}`), nil, newMetaToolCallAttempt(time.Now()))

	dispatchErr, ok := errors.AsType[*codemode.DispatchError](err)
	require.True(t, ok, "error: %v", err)
	require.Equal(t, "tool_call_rejected", dispatchErr.Code)
	require.Equal(t, hostedMemberRefusalUnnamed, dispatchErr.Rejection)
	require.NotContains(t, dispatchErr.Rejection, "support", "the program already holds the slug it called")
}

// A slug that is neither a member nor a refused hosted member still reads as
// unknown.
func TestDispatchUnknownMemberIsNotHostedRefusal(t *testing.T) {
	t.Parallel()

	s := &Service{logger: testenv.NewLogger(t)}
	_, err := s.dispatchMetaMemberTool(t.Context(), s.logger, hostedRefusalGate(), nil, hostedRefusalRequest(), "other--tool", json.RawMessage(`{}`), nil, newMetaToolCallAttempt(time.Now()))
	require.ErrorContains(t, err, `unknown server "other"`)
}

func TestDescribeServerHostedGatewayMemberIsToolError(t *testing.T) {
	t.Parallel()

	s := &Service{logger: testenv.NewLogger(t)}
	body, failure, err := s.handleMetaDescribeServerCall(t.Context(), s.logger, hostedRefusalGate(), nil, hostedRefusalRequest(), json.RawMessage(`{"server":"support"}`))
	require.NoError(t, err)
	require.Nil(t, failure, "a hosted refusal is not a member failure")

	var envelope toolCallResultEnvelope
	require.NoError(t, json.Unmarshal(body, &envelope))
	require.True(t, envelope.Result.IsError)
	require.Equal(t, hostedMemberRefusalMessage, envelope.Result.Content[0].Text)
}

// describe_tools lists a hosted member once under failed, however many of
// its tools were named, rather than as not found.
func TestDescribeToolsHostedGatewayMemberIsFailedServer(t *testing.T) {
	t.Parallel()

	s := &Service{logger: testenv.NewLogger(t)}
	body, failures, err := s.handleMetaDescribeToolsCall(t.Context(), s.logger, hostedRefusalGate(), nil, hostedRefusalRequest(), json.RawMessage(`{"tools":["support--a","support--b"]}`))
	require.NoError(t, err)
	require.Empty(t, failures, "a hosted refusal is not a member failure")

	var envelope struct {
		Result struct {
			StructuredContent metamcp.DescribeToolsResult `json:"structuredContent"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope))
	result := envelope.Result.StructuredContent
	require.Empty(t, result.Tools)
	require.Empty(t, result.NotFound)
	require.Equal(t, []metamcp.FailedServer{{Server: "support", Message: hostedMemberRefusalMessage}}, result.Failed)
}
