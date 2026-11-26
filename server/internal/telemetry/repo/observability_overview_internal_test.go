package repo

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Every scope field must be classified, so a new filter cannot reach the
// summary table without a decision.
func TestToolCallSummariesCanServe_ClassifiesEveryScopeField(t *testing.T) {
	t.Parallel()

	supported := map[string]func(*GetOverviewSummaryParams){
		"GramProjectIDs":      func(p *GetOverviewSummaryParams) { p.GramProjectIDs = []string{uuid.NewString()} },
		"TimeStart":           func(p *GetOverviewSummaryParams) { p.TimeStart = 1 },
		"TimeEnd":             func(p *GetOverviewSummaryParams) { p.TimeEnd = 1 },
		"ToolsetSlug":         func(p *GetOverviewSummaryParams) { p.ToolsetSlug = "slug" },
		"ToolsetSlugServerID": func(p *GetOverviewSummaryParams) { p.ToolsetSlugServerID = uuid.NewString() },
		"RemoteMCPServerID":   func(p *GetOverviewSummaryParams) { p.RemoteMCPServerID = uuid.NewString() },
		"MCPServerID":         func(p *GetOverviewSummaryParams) { p.MCPServerID = uuid.NewString() },
		"MetaMCPServerID":     func(p *GetOverviewSummaryParams) { p.MetaMCPServerID = uuid.NewString() },
		"HookSource":          func(p *GetOverviewSummaryParams) { p.HookSource = "cursor" },
		"ExcludedHookSources": func(p *GetOverviewSummaryParams) { p.ExcludedHookSources = []string{"judge"} },
	}
	unsupported := map[string]func(*GetOverviewSummaryParams){
		"User": func(p *GetOverviewSummaryParams) { p.User = UserIdentity{UserIDs: []string{"user"}, Emails: nil} },
		"CanonicalUser": func(p *GetOverviewSummaryParams) {
			p.CanonicalUser = CanonicalUserIdentity{OrgID: uuid.NewString(), UserID: "user", EmailLower: ""}
		},
		"ExternalUserID": func(p *GetOverviewSummaryParams) { p.ExternalUserID = "external" },
		"APIKeyID":       func(p *GetOverviewSummaryParams) { p.APIKeyID = "key" },
		"EventSource":    func(p *GetOverviewSummaryParams) { p.EventSource = "hook" },
		"AccountType":    func(p *GetOverviewSummaryParams) { p.AccountType = "team" },
		"ExternalOrgID":  func(p *GetOverviewSummaryParams) { p.ExternalOrgID = "org" },
	}

	typ := reflect.TypeFor[GetOverviewSummaryParams]()
	for field := range typ.Fields() {
		_, isSupported := supported[field.Name]
		_, isUnsupported := unsupported[field.Name]
		require.NotEqual(t, isSupported, isUnsupported, "classify GetOverviewSummaryParams.%s as supported or unsupported by mcp_tool_call_summaries", field.Name)
	}
	require.Equal(t, typ.NumField(), len(supported)+len(unsupported))

	for name, set := range supported {
		var p GetOverviewSummaryParams
		set(&p)
		require.True(t, p.toolCallSummariesCanServe(), name)
	}
	for name, set := range unsupported {
		var p GetOverviewSummaryParams
		set(&p)
		require.False(t, p.toolCallSummariesCanServe(), name)
	}
}
