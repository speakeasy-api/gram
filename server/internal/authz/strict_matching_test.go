package authz

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

const missingKeyLog = "authz check refused: a grant names a key the check does not carry"

// readOnlyConnect is a connect grant on server limited to read-only tools.
func readOnlyConnect(server uuid.UUID) []Grant {
	selector := NewSelector(ScopeMCPConnect, server.String())
	selector[SelectorKeyDisposition] = DispositionReadOnly
	return []Grant{NewGrantWithSelector(ScopeMCPConnect, selector)}
}

// A grant key the check does not carry never matches, except where the
// constructor says the omission is meant: a connection, which names no tool,
// and a probe.
func TestMatching_MissingKeyNeverMatches(t *testing.T) {
	t.Parallel()

	server := uuid.New()
	project := uuid.New()
	grants := readOnlyConnect(server)
	ref := MCPServer(MCPServerIDOf(server), ProjectIDOf(project))

	for name, check := range map[string]Check{
		"generated connection":   ref.Connect(),
		"constructed connection": MCPCheck(ScopeMCPConnect, server.String(), project.String()),
		"probe of a tool call":   ref.ToolCall("mystery", "").Probe(),
	} {
		allowed, err := GrantsAuthorize(grants, check)
		require.NoError(t, err, name)
		require.True(t, allowed, name)
	}

	for name, check := range map[string]Check{
		"tool call without a disposition": ref.ToolCall("mystery", ""),
		"connect check not built as a connection": {
			Scope: ScopeMCPConnect, ResourceKind: "", ResourceID: server.String(),
			Dimensions: map[string]string{SelectorKeyProjectID: project.String()},
		},
		"connection matched exactly": ref.Connect().WithStrictSelectorMatch(),
	} {
		allowed, err := GrantsAuthorize(grants, check)
		require.NoError(t, err, name)
		require.False(t, allowed, name)
	}
}

// Only the connect permission has connections, and only the risk policy
// audience permission (or its bypass) asks an audience question.
func TestMatching_OmissionStampsBelongToTheirPermission(t *testing.T) {
	t.Parallel()

	read := MCPCheck(ScopeMCPRead, "server-1", "project-1")
	require.NoError(t, validateInput(read))
	require.ErrorIs(t, validateInput(read.asConnection()), ErrInvalidCheck)

	audience := RiskPolicyEvaluateCheck("policy-1")
	audience.Scope = ScopeRiskPolicyBlock
	require.ErrorIs(t, validateInput(audience), ErrInvalidCheck)
}

// An audience grant limited to one server still places the caller under the
// policy wherever it is asked: leaving the server out can only apply the
// policy more widely. A bypass limited to one server exempts nothing the
// request does not name.
func TestMatching_RiskPolicyAudienceIgnoresServerLimit(t *testing.T) {
	t.Parallel()

	audience := NewSelector(ScopeRiskPolicyEvaluate, "policy-1")
	audience[SelectorKeyServerURL] = "https://one.example.com"
	bypass := NewSelector(ScopeRiskPolicyBypass, "policy-1")
	bypass[SelectorKeyServerURL] = "https://one.example.com"
	grants := []Grant{NewGrantWithSelector(ScopeRiskPolicyEvaluate, audience), NewGrantWithSelector(ScopeRiskPolicyBypass, bypass)}

	application, err := EvaluateRiskPolicy(grants, "policy-1", RiskPolicyDimensions{ServerURL: "https://two.example.com", ServerIdentity: ""})
	require.NoError(t, err)
	require.Equal(t, RiskPolicyGoverns, application)

	application, err = EvaluateRiskPolicy(grants, "policy-1", RiskPolicyDimensions{ServerURL: "", ServerIdentity: ""})
	require.NoError(t, err)
	require.Equal(t, RiskPolicyGoverns, application)

	application, err = EvaluateRiskPolicy(grants, "policy-1", RiskPolicyDimensions{ServerURL: "https://one.example.com", ServerIdentity: ""})
	require.NoError(t, err)
	require.Equal(t, RiskPolicyExempt, application)
}

// A refusal caused only by a key the check does not carry is logged with the
// key, so a call site that should carry it shows up. Other refusals, and
// allowed checks, log nothing.
func TestMatching_MissingKeyRefusalIsLogged(t *testing.T) {
	t.Parallel()

	server := uuid.New()
	project := uuid.New()
	ref := MCPServer(MCPServerIDOf(server), ProjectIDOf(project))

	var logs bytes.Buffer
	engine := NewEngine(slog.New(slog.NewTextHandler(&logs, nil)), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), readOnlyConnect(server))

	requireForbidden(t, engine.Require(ctx, ref.ToolCall("mystery", "")))
	require.Contains(t, logs.String(), missingKeyLog)
	require.Contains(t, logs.String(), SelectorKeyDisposition)

	logs.Reset()
	requireForbidden(t, engine.RequireAny(ctx, ref.ToolCall("mystery", "")))
	require.Contains(t, logs.String(), missingKeyLog)

	logs.Reset()
	require.NoError(t, engine.Require(ctx, ref.Connect()))
	require.NoError(t, engine.Require(ctx, ref.ToolCall("search", DispositionReadOnly)))
	requireForbidden(t, engine.Require(ctx, ref.ToolCall("delete_all", DispositionDestructive)))
	require.NotContains(t, logs.String(), missingKeyLog)
}

// Only a policy that refuses the check explains the refusal: a missing key on
// a grant in a policy that allows the check is not why it was refused.
func TestMatching_MissingKeysComeFromRefusingPolicies(t *testing.T) {
	t.Parallel()

	server := uuid.New()
	project := uuid.New()
	call := MCPServer(MCPServerIDOf(server), ProjectIDOf(project)).ToolCall("mystery", "")
	otherServer := []Grant{NewGrant(ScopeMCPConnect, uuid.NewString())}
	allowing := append([]Grant{NewGrant(ScopeMCPConnect, server.String())}, readOnlyConnect(server)...)

	refusedElsewhere := grantAuthorization{policies: [][]Grant{otherServer, allowing}, representedUser: "", representedPolicies: nil, legacyAuthCeiling: false}
	require.Empty(t, refusedElsewhere.keysMissingFrom(call))

	refusedForTheKey := grantAuthorization{policies: [][]Grant{readOnlyConnect(server), allowing}, representedUser: "", representedPolicies: nil, legacyAuthCeiling: false}
	require.Equal(t, []string{SelectorKeyDisposition}, refusedForTheKey.keysMissingFrom(call))
}

// RequireAny still reports a missing key on an alternative that no block
// refused, even when a block refused another one.
func TestMatching_RequireAnyLogsUnblockedAlternatives(t *testing.T) {
	t.Parallel()

	server := uuid.New()
	blockedServer := uuid.New()
	project := uuid.New()
	grants := append(readOnlyConnect(server),
		NewGrant(ScopeMCPConnect, blockedServer.String()),
		NewGrant(ScopeMCPBlockedConnect, blockedServer.String()),
	)

	var logs bytes.Buffer
	engine := NewEngine(slog.New(slog.NewTextHandler(&logs, nil)), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), grants)

	requireForbidden(t, engine.RequireAny(ctx,
		MCPServer(MCPServerIDOf(blockedServer), ProjectIDOf(project)).Connect(),
		MCPServer(MCPServerIDOf(server), ProjectIDOf(project)).ToolCall("mystery", ""),
	))
	require.Contains(t, logs.String(), missingKeyLog)
}
