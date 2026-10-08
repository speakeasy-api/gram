package platformmcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mv"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestToolInventoryCursorBindsContinuationToPrincipalProjectAndFilters(t *testing.T) {
	t.Parallel()

	codec, err := newToolInventoryCursorCodec("test-cursor-key")
	require.NoError(t, err)
	principal := Principal{OrganizationID: "organization", ConnectionID: "connection", Generation: "generation"}
	projectID := uuid.New()
	value, err := codec.Encode(toolInventoryCursor{
		OrganizationID: principal.OrganizationID,
		Binding:        principalCursorBinding(principal),
		ProjectID:      projectID.String(),
		Query:          "deploy",
		SourceKind:     "function",
		AfterToolURN:   "tools:function:orders:create_order",
	})
	require.NoError(t, err)

	after, err := codec.Decode(value, principal, projectID, "deploy", "function")
	require.NoError(t, err)
	require.Equal(t, "tools:function:orders:create_order", after)

	_, err = codec.Decode(value, Principal{OrganizationID: principal.OrganizationID, ConnectionID: "connection", Generation: "other"}, projectID, "deploy", "function")
	require.ErrorIs(t, err, ErrToolInventoryCursor)
	_, err = codec.Decode(value, principal, uuid.New(), "deploy", "function")
	require.ErrorIs(t, err, ErrToolInventoryCursor)
	_, err = codec.Decode(value, principal, projectID, "other", "function")
	require.ErrorIs(t, err, ErrToolInventoryCursor)
	_, err = codec.Decode(value, principal, projectID, "deploy", "openapi")
	require.ErrorIs(t, err, ErrToolInventoryCursor)
}

// The version token is what stands between a stale read and a silent
// overwrite, so it must move with the list and with the target, and must not
// move with the order the list happens to come back in.
func TestToolExposureVersionTracksListAndTarget(t *testing.T) {
	t.Parallel()

	projectID, mcpID, toolsetID := uuid.New(), uuid.New(), uuid.New()
	urns := []string{"tools:function:orders:create_order", "tools:http:billing:get_invoice"}
	base := mv.ToolListVersionToken(projectID, mcpID, toolsetID, 4, urns)
	require.Len(t, base, 64)

	require.Equal(t, base, mv.ToolListVersionToken(projectID, mcpID, toolsetID, 4, []string{urns[1], urns[0]}),
		"the same committed list must yield the same token whatever order it is read in")
	require.NotEqual(t, base, mv.ToolListVersionToken(projectID, mcpID, toolsetID, 5, urns))
	require.NotEqual(t, base, mv.ToolListVersionToken(projectID, mcpID, toolsetID, 4, append([]string{"tools:function:orders:cancel_order"}, urns...)))
	require.NotEqual(t, base, mv.ToolListVersionToken(projectID, mcpID, uuid.New(), 4, urns))
	require.NotEqual(t, base, mv.ToolListVersionToken(projectID, uuid.New(), toolsetID, 4, urns))
	require.NotEqual(t, base, mv.ToolListVersionToken(uuid.New(), mcpID, toolsetID, 4, urns))
}

func TestToolExposureMutationRefusesUnsafeRequestsBeforeTouchingTheDatabase(t *testing.T) {
	t.Parallel()

	service := &MCPToolExposureService{}
	version := strings.Repeat("a", 64)
	valid := ChangeMCPToolsInput{
		ProjectID: uuid.NewString(), MCPID: uuid.NewString(),
		ToolURNs:        []string{"tools:function:orders:create_order"},
		ExpectedVersion: version, IdempotencyKey: "key", Confirmed: true,
	}

	for name, mutate := range map[string]func(ChangeMCPToolsInput) ChangeMCPToolsInput{
		"no idempotency key": func(in ChangeMCPToolsInput) ChangeMCPToolsInput { in.IdempotencyKey = " "; return in },
		"short version":      func(in ChangeMCPToolsInput) ChangeMCPToolsInput { in.ExpectedVersion = "abc"; return in },
		"bad project":        func(in ChangeMCPToolsInput) ChangeMCPToolsInput { in.ProjectID = "not-a-uuid"; return in },
		"bad mcp":            func(in ChangeMCPToolsInput) ChangeMCPToolsInput { in.MCPID = "not-a-uuid"; return in },
		"no tools":           func(in ChangeMCPToolsInput) ChangeMCPToolsInput { in.ToolURNs = nil; return in },
		"too many tools": func(in ChangeMCPToolsInput) ChangeMCPToolsInput {
			in.ToolURNs = make([]string, maxToolExposureBatch+1)
			for i := range in.ToolURNs {
				in.ToolURNs[i] = "tools:function:orders:create_order"
			}
			return in
		},
	} {
		_, _, _, err := service.validate(mutate(valid))
		require.Error(t, err, "%s must be refused", name)
		var refusal *MCPToolExposureError
		require.ErrorAs(t, err, &refusal)
		require.Equal(t, "invalid_request", refusal.Code, "%s", name)
	}

	// A tool the caller cannot have obtained from the listing is named back
	// rather than dropped from the batch.
	malformed := valid
	malformed.ToolURNs = []string{"tools:function:orders:create_order", "create_order"}
	_, _, _, err := service.validate(malformed)
	var refusal *MCPToolExposureError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, []string{"create_order"}, refusal.UnknownTools)

	// Duplicates collapse instead of being applied twice.
	duplicated := valid
	duplicated.ToolURNs = []string{"tools:function:orders:create_order", " tools:function:orders:create_order "}
	_, _, requested, err := service.validate(duplicated)
	require.NoError(t, err)
	require.Len(t, requested, 1)
}

func TestToolExposureMutationRequiresExplicitConfirmation(t *testing.T) {
	t.Parallel()

	service := &MCPToolExposureService{}
	unconfirmed := ChangeMCPToolsInput{
		ProjectID: uuid.NewString(), MCPID: uuid.NewString(),
		ToolURNs:        []string{"tools:function:orders:create_order"},
		ExpectedVersion: strings.Repeat("a", 64), IdempotencyKey: "key", Confirmed: false,
	}
	_, _, _, err := service.validate(unconfirmed)
	var refusal *MCPToolExposureError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "invalid_request", refusal.Code)
	require.Contains(t, refusal.Message, "Confirm")
}

func TestToolExposureRefusalNamesTheToolsItRefused(t *testing.T) {
	t.Parallel()

	result, ok := toolExposureToolResult(&MCPToolExposureError{
		Code: "invalid_request", Message: "nothing was changed", UnknownTools: []string{"tools:function:orders:create_order"},
	})
	require.True(t, ok)
	require.True(t, result.IsError)
	text, isText := result.Content[0].(*mcp.TextContent)
	require.True(t, isText)
	var refusal toolExposureRefusal
	require.NoError(t, json.Unmarshal([]byte(text.Text), &refusal))
	require.Equal(t, "invalid_request", refusal.Code)
	require.Equal(t, toolExposureFeature, refusal.Feature)
	require.Equal(t, []string{"tools:function:orders:create_order"}, refusal.UnknownTools)

	conflict, ok := toolExposureToolResult(toolExposureConflict())
	require.True(t, ok)
	require.True(t, conflict.IsError)

	missing, ok := toolExposureToolResult(toolExposureMissing())
	require.True(t, ok)
	missingText, isText := missing.Content[0].(*mcp.TextContent)
	require.True(t, isText)
	require.Contains(t, missingText.Text, "dashboard", "a server outside this workflow points the caller somewhere that can do it")
}

// A shared tool list is a shape, not a race. Reporting it as a conflict would
// send a caller following the shipped skill back to a fresh read and the same
// refusal, forever, so these two carry their own codes and say re-reading will
// not help.
func TestToolExposureSharedRefusalsAreNotConflicts(t *testing.T) {
	t.Parallel()

	for code, err := range map[string]error{
		"shared_tool_list":       toolExposureTooManyFrontingServers(),
		"shared_outside_project": toolExposureSharedOutsideProject(),
	} {
		require.ErrorIs(t, err, ErrMCPToolExposureShared, "%s", code)
		require.NotErrorIs(t, err, ErrMCPToolExposureConflict,
			"%s must not read as an optimistic-concurrency failure the caller should retry", code)

		result, ok := toolExposureToolResult(err)
		require.True(t, ok, "%s", code)
		require.True(t, result.IsError, "%s", code)
		text, isText := result.Content[0].(*mcp.TextContent)
		require.True(t, isText, "%s", code)
		var refusal toolExposureRefusal
		require.NoError(t, json.Unmarshal([]byte(text.Text), &refusal))
		require.Equal(t, code, refusal.Code)
		require.Contains(t, refusal.Message, "dashboard", "%s points somewhere that can do it", code)
		require.Contains(t, refusal.Message, "re-reading will not", "%s tells the caller not to loop", code)
	}
}

// A spent allowance is a wait, not a broken feature: it must not be reported
// as the generic unavailable refusal, which reads as "this does not work here".
func TestToolExposureBudgetRefusalsTellThrottleFromFailure(t *testing.T) {
	t.Parallel()

	throttled := toolExposureBudgetError(ErrOperationRateLimited, "Listing a project's tools was asked for too often just now.")
	var refusal *MCPToolExposureError
	require.ErrorAs(t, throttled, &refusal)
	require.Equal(t, "rate_limited", refusal.Code)
	require.Contains(t, refusal.Message, "Try again shortly")

	broken := toolExposureBudgetError(ErrOperationBudgetUnavailable, "Listing a project's tools was asked for too often just now.")
	require.ErrorAs(t, broken, &refusal)
	require.Equal(t, unavailableCode, refusal.Code)
	require.ErrorIs(t, broken, ErrUnavailable)
}

// The service refuses to compose without both allowances, so a deployment can
// never register these tools as live while they are unmetered.
func TestToolExposureServiceRequiresBothOperationBudgets(t *testing.T) {
	t.Parallel()

	compose := func(reads, changes OperationBudget) error {
		_, err := NewMCPToolExposureService(
			testenv.NewLogger(t), &pgxpool.Pool{}, audit.NewLogger(), &authz.Engine{},
			allowExternalCallAuthorizer{}, "key", plugins.PublicationRequests{}, nil, reads, changes,
		)
		return err
	}

	require.ErrorIs(t, compose(OperationBudget{}, testOperationBudget()), ErrMCPToolExposureInvalid,
		"a read without an allowance does not compose")
	require.ErrorIs(t, compose(testOperationBudget(), OperationBudget{}), ErrMCPToolExposureInvalid,
		"a write without an allowance does not compose")
	require.NoError(t, compose(testOperationBudget(), testOperationBudget()))
}

// The tool list belongs to the server detail, not to the list of servers: an
// empty list on a find_mcp row would read as "exposes nothing".
func TestFindMCPResultsCarryNoToolExposure(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(MCP{ID: uuid.NewString(), ProjectID: uuid.NewString()})
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "tool_exposure")
}

// TestFrontingSetGrew pins the asymmetry of the fronting-server guard: it
// refuses growth and tolerates shrinkage. Both halves are load-bearing in
// opposite directions, and the tolerant half is the one that looks like a bug.
//
// The structural guarantee that makes growth near-impossible lives in the lock
// ordering (the toolsets FOR UPDATE conflicts with the FK's FOR KEY SHARE), and
// is covered by the integration test. This is the backstop for the case where
// that ordering stops holding, so it is tested here on its own.
func TestFrontingSetGrew(t *testing.T) {
	t.Parallel()

	alpha, bravo, charlie := uuid.New(), uuid.New(), uuid.New()
	row := func(ids []uuid.UUID, foreign int64) platformrepo.GetPlatformMCPServerToolExposureRow {
		return platformrepo.GetPlatformMCPServerToolExposureRow{FrontingServerIds: ids, ForeignFrontingServerCount: foreign}
	}

	tests := []struct {
		name       string
		authorized platformrepo.GetPlatformMCPServerToolExposureRow
		current    platformrepo.GetPlatformMCPServerToolExposureRow
		want       bool
		why        string
	}{
		{
			name:       "unchanged set is not growth",
			authorized: row([]uuid.UUID{alpha, bravo}, 1),
			current:    row([]uuid.UUID{alpha, bravo}, 1),
			want:       false,
			why:        "the common case: nothing raced, the change proceeds",
		},
		{
			name:       "a server joining the set is growth",
			authorized: row([]uuid.UUID{alpha}, 0),
			current:    row([]uuid.UUID{alpha, bravo}, 1),
			want:       true,
			why:        "bravo would be changed without having been authorized",
		},
		{
			name:       "a server the authorized read never saw is growth even at equal count",
			authorized: row([]uuid.UUID{alpha, bravo}, 1),
			current:    row([]uuid.UUID{alpha, charlie}, 1),
			want:       true,
			why:        "counts match, membership does not; charlie was never authorized",
		},
		{
			name:       "a foreign server appearing is growth even with an unchanged id set",
			authorized: row([]uuid.UUID{alpha}, 0),
			current:    row([]uuid.UUID{alpha}, 1),
			want:       true,
			why:        "a server outside this project's visibility joined, and is not in FrontingServerIds to be spotted by membership alone",
		},
		{
			// This case must stay tolerant. A concurrent DeleteMCPServer sets
			// deleted_at without touching mcp_servers.toolset_id, so it takes
			// no FOR KEY SHARE on the toolsets row and CAN commit while this
			// transaction holds FOR UPDATE. Replacing frontingSetGrew with an
			// equality check would make that ordinary delete refuse a fully
			// authorized change. It reads as a hardening and is a regression.
			name:       "a server leaving the set is not growth",
			authorized: row([]uuid.UUID{alpha, bravo}, 1),
			current:    row([]uuid.UUID{alpha}, 0),
			want:       false,
			why:        "a concurrent delete is harmless here; the change authorized more than it needed",
		},
		{
			name:       "an emptied set is not growth",
			authorized: row([]uuid.UUID{alpha, bravo}, 1),
			current:    row(nil, 0),
			want:       false,
			why:        "same reasoning as a single delete, at the boundary",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, frontingSetGrew(tt.authorized, tt.current), tt.why)
		})
	}
}
