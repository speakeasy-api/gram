package mcp

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestMetaServerDiscover_InstructionsAndCacheScope(t *testing.T) {
	t.Parallel()

	for _, gated := range []bool{false, true} {
		for _, instructions := range []pgtype.Text{{}, {Valid: true}, {String: " \t\n", Valid: true}, {String: "Operator instructions", Valid: true}} {
			meta := &metamcprepo.MetaMcpServer{
				Instructions:        instructions,
				UserSessionIssuerID: uuid.NullUUID{UUID: uuid.New(), Valid: gated},
			}
			bs, err := handleServerDiscover(t.Context(), testenv.NewLogger(t), mcpjsonrpc.NumberID(7), describeMetaServer(meta, metamcp.DiscoveryModeProgressive, false, nil), mcpversions.SupportedMetaServer())
			require.NoError(t, err)
			var response struct {
				Result struct {
					Instructions string `json:"instructions"`
					CacheScope   string `json:"cacheScope"`
					TTLMs        *int   `json:"ttlMs"`
				} `json:"result"`
			}
			require.NoError(t, json.Unmarshal(bs, &response))
			want := metamcp.Instructions
			if strings.TrimSpace(instructions.String) != "" {
				want = instructions.String
			}
			require.Equal(t, want, response.Result.Instructions)
			scope := "public"
			if gated {
				scope = "private"
			}
			require.Equal(t, scope, response.Result.CacheScope)
			require.NotNil(t, response.Result.TTLMs)
			require.Zero(t, *response.Result.TTLMs)
		}
	}
}

func TestMetaServerDiscover_MemberSummaryIsPrivate(t *testing.T) {
	t.Parallel()

	meta := &metamcprepo.MetaMcpServer{}
	bs, err := handleServerDiscover(t.Context(), testenv.NewLogger(t), mcpjsonrpc.NumberID(7), describeMetaServer(meta, metamcp.DiscoveryModeProgressive, false, []metamcp.MemberLabel{{Slug: "linear", Name: "Linear"}}), mcpversions.SupportedMetaServer())
	require.NoError(t, err)
	var response struct {
		Result struct {
			Instructions string `json:"instructions"`
			CacheScope   string `json:"cacheScope"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(bs, &response))
	require.True(t, strings.HasPrefix(response.Result.Instructions, `Tools from these servers: "Linear".`+"\n\n"))
	require.True(t, strings.HasSuffix(response.Result.Instructions, metamcp.Instructions))
	require.Equal(t, "private", response.Result.CacheScope)
}

// A failed describe_tools row on a member logs only that member's names as its
// request, never names the call gave for other servers, which may be in other
// projects.
func TestDescribeToolsMemberArguments_ListsOnlyTheMembersTools(t *testing.T) {
	t.Parallel()

	require.JSONEq(t, `{"tools":["member--ping","member--pong"]}`,
		string(describeToolsMemberArguments("member", []string{"ping", "pong"})))
}

// A member's discovery failure row gets a trace of its own even when the
// tracer ignores trace.WithNewRoot, as the no-op tracer does with OTel
// tracing off, so it is not summarized into the gateway's discovery call.
func TestWithOwnTrace_FreshTraceWhenTheTracerKeptTheParent(t *testing.T) {
	t.Parallel()

	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{1},
		TraceFlags: trace.FlagsSampled,
		TraceState: trace.TraceState{},
		Remote:     true,
	})
	kept := trace.ContextWithSpanContext(t.Context(), parent)
	own := trace.SpanContextFromContext(withOwnTrace(kept, parent))
	require.True(t, own.IsValid())
	require.NotEqual(t, parent.TraceID(), own.TraceID())

	renewed := trace.ContextWithSpanContext(t.Context(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{2},
		SpanID:     trace.SpanID{2},
		TraceFlags: trace.FlagsSampled,
		TraceState: trace.TraceState{},
		Remote:     false,
	}))
	require.Equal(t, trace.TraceID{2}, trace.SpanContextFromContext(withOwnTrace(renewed, parent)).TraceID(), "a real new root keeps its trace")

	untraced := trace.SpanContextFromContext(withOwnTrace(t.Context(), trace.SpanContext{}))
	require.True(t, untraced.IsValid(), "a request without a trace still gets one of its own")
}

// A member's discovery failure row is the gateway's call: it carries no
// gateway stamp, is named gateway_<tool>, and keeps the member's URN kind.
func TestMetaToolCallFailureLogParams_DiscoveryRowIsUnstamped(t *testing.T) {
	t.Parallel()

	member := metaMember{slug: "billing", serverID: uuid.New(), projectID: uuid.New(), remoteServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
	gate := &metaGateContext{metaServerID: uuid.New(), organizationID: "org-1", projectID: uuid.New()}
	attempt := newMetaToolCallAttempt(time.Now())
	attempt.member = &member
	attempt.fail(http.StatusServiceUnavailable, "server \"billing\" is unavailable")

	qualified := metamcp.QualifyName(member.slug, gatewayDiscoveryToolPrefix+"describe_server")
	params := metaToolCallFailureLogParams(t.Context(), gate, qualified, nil, attempt, nil, metaFailureRow{unstamped: true})
	require.NotNil(t, params)
	require.Equal(t, "gateway_describe_server", params.ToolInfo.Name)
	require.Equal(t, "tools:"+string(urn.ToolKindExternalMCP)+":"+member.remoteServerID.UUID.String()+":gateway_describe_server", params.ToolInfo.URN)
	require.NotContains(t, params.Attributes, attr.MetaMcpServerIDKey)

	stamped := metaToolCallFailureLogParams(t.Context(), gate, qualified, nil, attempt, nil, metaFailureRow{unstamped: false})
	require.NotNil(t, stamped)
	require.Equal(t, gate.metaServerID.String(), stamped.Attributes[attr.MetaMcpServerIDKey])
}
