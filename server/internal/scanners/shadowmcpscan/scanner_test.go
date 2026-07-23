package shadowmcpscan

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// discardLogger returns a logger that discards output. This internal test
// package cannot use testenv.NewLogger: testenv transitively imports
// shadowmcpscan, so importing it here would create an import cycle.
func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler) //nolint:forbidigo // testenv.NewLogger would import-cycle this package's internal tests
}

// fakeValidator denies any MCP call whose bare (post-variation) tool name is in
// denied. It records the org id it was called with.
type fakeValidator struct {
	denied map[string]bool
	orgIDs []string
}

func (f *fakeValidator) ValidateToolsetCall(_ context.Context, _ any, toolName string, orgID string) (string, bool) {
	f.orgIDs = append(f.orgIDs, orgID)
	if f.denied[toolName] {
		return "denied: " + toolName, true
	}
	return "", false
}

// fakeHosted resolves no extra trusted hosts, leaving the scanner to match
// against the real built-in Gram hosts. Host matching itself is the production
// implementation, so tests exercise the same URL parsing and exact-host rules
// rather than a looser substring check that would pass a raw stdio command
// containing a Gram hostname anywhere in it.
type fakeHosted struct {
	calls int
	err   error
}

func (f *fakeHosted) TrustedMCPHostsForOrg(_ context.Context, _ string) ([]string, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return nil, nil
}

// fakeProvenance returns provenance for the ids it is configured with, or an
// error. It records the ids and time bound it was called with.
type fakeProvenance struct {
	found     map[string]telemetryrepo.MCPProvenance
	err       error
	gotIDs    []string
	gotSince  time.Time
	callCount int
}

func (f *fakeProvenance) LookupMCPProvenanceByToolCallID(_ context.Context, _ uuid.UUID, ids []string, since time.Time) (map[string]telemetryrepo.MCPProvenance, error) {
	f.callCount++
	f.gotIDs = append(f.gotIDs, ids...)
	f.gotSince = since
	if f.err != nil {
		return nil, f.err
	}
	return f.found, nil
}

// noProvenance is the null object for tests exercising the signature fallback.
func noProvenance() *fakeProvenance {
	return &fakeProvenance{found: map[string]telemetryrepo.MCPProvenance{}, err: nil, gotIDs: nil, gotSince: time.Time{}, callCount: 0}
}

// recordedResolution is one CoverageRecorder observation.
type recordedResolution struct {
	hookSource string
	resolution string
}

type fakeCoverage struct {
	got []recordedResolution
}

func (f *fakeCoverage) RecordShadowMCPResolution(_ context.Context, _ string, hookSource string, resolution string) {
	f.got = append(f.got, recordedResolution{hookSource: hookSource, resolution: resolution})
}

type bypassCheck struct {
	organizationID string
	userID         string
	policyID       uuid.UUID
	evidence       shadowmcp.AccessEvidence
	toolName       string
}

type fakeBypassChecker struct {
	allowed bool
	checks  []bypassCheck
}

func (f *fakeBypassChecker) CanBypassShadowMCP(
	_ context.Context,
	organizationID string,
	userID string,
	policyID uuid.UUID,
	evidence shadowmcp.AccessEvidence,
	toolName string,
) bool {
	f.checks = append(f.checks, bypassCheck{
		organizationID: organizationID,
		userID:         userID,
		policyID:       policyID,
		evidence:       evidence,
		toolName:       toolName,
	})
	return f.allowed
}

func gramHosted() *fakeHosted {
	return &fakeHosted{calls: 0, err: nil}
}

// A call whose provenance resolves to a Gram-hosted URL is clean even though
// its arguments carry no x-gram-toolset-id — this is the /x/mcp false-flag the
// provenance-first rework exists to fix.
func TestScanner_HostedProvenanceIsCleanWithoutSignature(t *testing.T) {
	t.Parallel()

	validator := &fakeValidator{denied: map[string]bool{"delete": true}, orgIDs: nil}
	prov := &fakeProvenance{
		found:     map[string]telemetryrepo.MCPProvenance{"call-1": {Match: "https://app.getgram.ai/x/mcp/abc", ServerURL: "https://app.getgram.ai/x/mcp/abc", HookSource: "claude"}},
		err:       nil,
		gotIDs:    nil,
		gotSince:  time.Time{},
		callCount: 0,
	}
	coverage := &fakeCoverage{got: nil}
	s := NewScanner(discardLogger(), validator, gramHosted(), prov, coverage)

	out := s.Scan(t.Context(), "org-1", uuid.New(), uuid.Nil, nil, [][]ToolCall{
		{{ID: "call-1", Name: "mcp__db__delete", Arguments: `{}`, CreatedAt: time.Now()}},
	})

	require.Len(t, out, 1)
	require.Empty(t, out[0], "Gram-hosted provenance must not be flagged")
	require.Empty(t, validator.orgIDs, "resolved provenance must not consult the signature validator")
	require.Equal(t, []recordedResolution{{hookSource: "claude", resolution: ResolutionHosted}}, coverage.got)
}

func TestScanner_ThirdPartyURLProvenanceIsFlagged(t *testing.T) {
	t.Parallel()

	validator := &fakeValidator{denied: map[string]bool{}, orgIDs: nil}
	prov := &fakeProvenance{
		found:     map[string]telemetryrepo.MCPProvenance{"call-1": {Match: "https://evil.example/mcp", ServerURL: "https://evil.example/mcp", HookSource: "claude"}},
		err:       nil,
		gotIDs:    nil,
		gotSince:  time.Time{},
		callCount: 0,
	}
	coverage := &fakeCoverage{got: nil}
	s := NewScanner(discardLogger(), validator, gramHosted(), prov, coverage)

	out := s.Scan(t.Context(), "org-1", uuid.New(), uuid.Nil, nil, [][]ToolCall{
		{{ID: "call-1", Name: "mcp__db__delete", Arguments: `{}`, CreatedAt: time.Now()}},
	})

	require.Len(t, out[0], 1)
	f := out[0][0]
	require.Equal(t, Source, f.Source)
	require.Equal(t, Rule, f.RuleID)
	require.Equal(t, "https://evil.example/mcp", f.Match)
	require.Equal(t, "call-1", f.McpLookupToolCallID)
	require.Contains(t, f.Description, "mcp__db__delete")
	require.Empty(t, validator.orgIDs, "a resolved verdict must not consult the signature validator")
	require.Equal(t, []recordedResolution{{hookSource: "claude", resolution: ResolutionShadow}}, coverage.got)
}

func TestScanner_URLBypassGrantSuppressesFinding(t *testing.T) {
	t.Parallel()

	policyID := uuid.New()
	serverURL := "https://third-party.example/mcp"
	prov := &fakeProvenance{
		found:     map[string]telemetryrepo.MCPProvenance{"call-1": {Match: serverURL, ServerURL: serverURL, HookSource: "cursor"}},
		err:       nil,
		gotIDs:    nil,
		gotSince:  time.Time{},
		callCount: 0,
	}
	bypass := &fakeBypassChecker{allowed: true, checks: nil}
	s := NewScannerWithBypass(discardLogger(), &fakeValidator{denied: map[string]bool{}, orgIDs: nil}, gramHosted(), prov, nil, bypass)

	out := s.Scan(t.Context(), "org-1", uuid.New(), policyID, []string{"user-1"}, [][]ToolCall{
		{{ID: "call-1", Name: "MCP:delete_row", Arguments: `{}`, CreatedAt: time.Now(), Sender: "Cursor"}},
	})

	require.Empty(t, out[0])
	require.Equal(t, []bypassCheck{{
		organizationID: "org-1",
		userID:         "user-1",
		policyID:       policyID,
		evidence: shadowmcp.AccessEvidence{
			FullURL:        serverURL,
			URLHost:        "",
			ServerIdentity: "",
		},
		toolName: "MCP:delete_row",
	}}, bypass.checks)
}

func TestScanner_UnattributedSessionCannotBypassFinding(t *testing.T) {
	t.Parallel()

	serverURL := "https://third-party.example/mcp"
	prov := &fakeProvenance{
		found:     map[string]telemetryrepo.MCPProvenance{"call-1": {Match: serverURL, ServerURL: serverURL, HookSource: "cursor"}},
		err:       nil,
		gotIDs:    nil,
		gotSince:  time.Time{},
		callCount: 0,
	}
	bypass := &fakeBypassChecker{allowed: true, checks: nil}
	s := NewScannerWithBypass(discardLogger(), &fakeValidator{denied: map[string]bool{}, orgIDs: nil}, gramHosted(), prov, nil, bypass)

	out := s.Scan(t.Context(), "org-1", uuid.New(), uuid.New(), []string{""}, [][]ToolCall{
		{{ID: "call-1", Name: "MCP:delete_row", Arguments: `{}`, CreatedAt: time.Now(), Sender: "Cursor"}},
	})

	require.Len(t, out[0], 1)
	require.Empty(t, bypass.checks, "unattributed sessions must not consult grants, including all-users grants")
}

// A stdio server has no URL; the launch command is the resolved identity and
// is shadow MCP by the same rule the realtime guard applies.
func TestScanner_StdioCommandProvenanceIsFlagged(t *testing.T) {
	t.Parallel()

	validator := &fakeValidator{denied: map[string]bool{}, orgIDs: nil}
	prov := &fakeProvenance{
		found:     map[string]telemetryrepo.MCPProvenance{"call-1": {Match: "npx -y @acme/mcp", ServerURL: "", HookSource: "codex"}},
		err:       nil,
		gotIDs:    nil,
		gotSince:  time.Time{},
		callCount: 0,
	}
	s := NewScanner(discardLogger(), validator, gramHosted(), prov, nil)

	out := s.Scan(t.Context(), "org-1", uuid.New(), uuid.Nil, nil, [][]ToolCall{
		{{ID: "call-1", Name: "mcp__db__delete", Arguments: `{}`, CreatedAt: time.Now()}},
	})

	require.Len(t, out[0], 1)
	require.Equal(t, "npx -y @acme/mcp", out[0][0].Match)
	require.Empty(t, validator.orgIDs)
}

func TestScanner_StdioBypassGrantSuppressesFinding(t *testing.T) {
	t.Parallel()

	policyID := uuid.New()
	command := "npx -y @example/mcp"
	prov := &fakeProvenance{
		found:     map[string]telemetryrepo.MCPProvenance{"call-1": {Match: command, ServerURL: "", HookSource: "codex"}},
		err:       nil,
		gotIDs:    nil,
		gotSince:  time.Time{},
		callCount: 0,
	}
	bypass := &fakeBypassChecker{allowed: true, checks: nil}
	s := NewScannerWithBypass(discardLogger(), &fakeValidator{denied: map[string]bool{}, orgIDs: nil}, gramHosted(), prov, nil, bypass)

	out := s.Scan(t.Context(), "org-1", uuid.New(), policyID, []string{"user-1"}, [][]ToolCall{
		{{ID: "call-1", Name: "mcp__db__delete", Arguments: `{}`, CreatedAt: time.Now(), Sender: "Codex"}},
	})

	require.Empty(t, out[0])
	require.Equal(t, []bypassCheck{{
		organizationID: "org-1",
		userID:         "user-1",
		policyID:       policyID,
		evidence: shadowmcp.AccessEvidence{
			FullURL:        "",
			URLHost:        "",
			ServerIdentity: command,
		},
		toolName: "mcp__db__delete",
	}}, bypass.checks)
}

// The match attribute degrades to the bare mcp__<server>__ prefix when the
// sender's inventory snapshot didn't resolve the server. That value carries no
// server identity — it is derived from the tool name the scanner already has —
// so it must count as unresolved and fall through to the signature check
// rather than flagging every call in a snapshot-less session.
func TestScanner_BarePrefixProvenanceFallsBackToSignature(t *testing.T) {
	t.Parallel()

	validator := &fakeValidator{denied: map[string]bool{}, orgIDs: nil}
	prov := &fakeProvenance{
		found:     map[string]telemetryrepo.MCPProvenance{"call-1": {Match: "db", ServerURL: "", HookSource: "claude"}},
		err:       nil,
		gotIDs:    nil,
		gotSince:  time.Time{},
		callCount: 0,
	}
	coverage := &fakeCoverage{got: nil}
	s := NewScanner(discardLogger(), validator, gramHosted(), prov, coverage)

	out := s.Scan(t.Context(), "org-1", uuid.New(), uuid.Nil, nil, [][]ToolCall{
		{{ID: "call-1", Name: "mcp__db__delete", Arguments: `{}`, CreatedAt: time.Now()}},
	})

	require.Empty(t, out[0], "validator allowed the call, so the fallback keeps it clean")
	require.Equal(t, []string{"org-1"}, validator.orgIDs, "bare prefix must reach the signature validator")
	require.Equal(t, []recordedResolution{{hookSource: "claude", resolution: ResolutionUnresolved}}, coverage.got)
}

func TestScanner_UnresolvedProvenanceFallsBackToSignature(t *testing.T) {
	t.Parallel()

	validator := &fakeValidator{denied: map[string]bool{"delete": true}, orgIDs: nil}
	coverage := &fakeCoverage{got: nil}
	s := NewScanner(discardLogger(), validator, gramHosted(), noProvenance(), coverage)

	out := s.Scan(t.Context(), "org-1", uuid.New(), uuid.Nil, nil, [][]ToolCall{
		{{ID: "call-1", Name: "mcp__db__delete", Arguments: `{}`, CreatedAt: time.Now(), Sender: "Codex"}},
		{{ID: "call-2", Name: "mcp__db__read", Arguments: `{}`, CreatedAt: time.Now(), Sender: "Codex"}},
	})

	require.Len(t, out[0], 1, "denied by signature")
	require.Equal(t, "db", out[0][0].Match, "unresolved findings keep the server-prefix match")
	require.Empty(t, out[1], "allowed by signature")
	require.Equal(t, []string{"org-1", "org-1"}, validator.orgIDs)
	// Unresolved calls have no provenance row and so no hook source of their
	// own; attribution must fall back to the message's sender, lowercased to
	// match the telemetry vocabulary. Without this the whole unresolved
	// population collapses to "unknown" and the metric cannot gate removing
	// the signature fallback per sender.
	require.Equal(t, []recordedResolution{
		{hookSource: "codex", resolution: ResolutionUnresolved},
		{hookSource: "codex", resolution: ResolutionUnresolved},
	}, coverage.got)
}

// The provenance row's own hook source wins over the message's sender when
// both are present.
func TestScanner_ProvenanceHookSourceWinsOverMessageSender(t *testing.T) {
	t.Parallel()

	prov := &fakeProvenance{
		found:     map[string]telemetryrepo.MCPProvenance{"call-1": {Match: "https://evil.example/mcp", ServerURL: "https://evil.example/mcp", HookSource: "claude"}},
		err:       nil,
		gotIDs:    nil,
		gotSince:  time.Time{},
		callCount: 0,
	}
	coverage := &fakeCoverage{got: nil}
	s := NewScanner(discardLogger(), &fakeValidator{denied: map[string]bool{}, orgIDs: nil}, gramHosted(), prov, coverage)

	s.Scan(t.Context(), "org-1", uuid.New(), uuid.Nil, nil, [][]ToolCall{
		{{ID: "call-1", Name: "mcp__db__delete", Arguments: `{}`, CreatedAt: time.Now(), Sender: "Cursor"}},
	})

	require.Equal(t, []recordedResolution{{hookSource: "claude", resolution: ResolutionShadow}}, coverage.got)
}

// A provenance query failure must not decide anything: every call falls back
// to the self-contained signature check rather than being judged on evidence
// the scanner failed to load.
func TestScanner_ProvenanceErrorFallsBackToSignature(t *testing.T) {
	t.Parallel()

	validator := &fakeValidator{denied: map[string]bool{"delete": true}, orgIDs: nil}
	prov := &fakeProvenance{found: nil, err: errors.New("boom"), gotIDs: nil, gotSince: time.Time{}, callCount: 0}
	s := NewScanner(discardLogger(), validator, gramHosted(), prov, nil)

	out := s.Scan(t.Context(), "org-1", uuid.New(), uuid.Nil, nil, [][]ToolCall{
		{{ID: "call-1", Name: "mcp__db__delete", Arguments: `{}`, CreatedAt: time.Now()}},
	})

	require.Len(t, out[0], 1)
	require.Equal(t, "db", out[0][0].Match)
	require.Equal(t, []string{"org-1"}, validator.orgIDs)
}

// ServerURL is the only value guaranteed to be a real resolved URL, so it wins
// over the match union when both are present.
func TestScanner_ServerURLPreferredOverMatch(t *testing.T) {
	t.Parallel()

	validator := &fakeValidator{denied: map[string]bool{}, orgIDs: nil}
	prov := &fakeProvenance{
		found:     map[string]telemetryrepo.MCPProvenance{"call-1": {Match: "db", ServerURL: "https://app.getgram.ai/mcp/team", HookSource: "cursor"}},
		err:       nil,
		gotIDs:    nil,
		gotSince:  time.Time{},
		callCount: 0,
	}
	s := NewScanner(discardLogger(), validator, gramHosted(), prov, nil)

	out := s.Scan(t.Context(), "org-1", uuid.New(), uuid.Nil, nil, [][]ToolCall{
		{{ID: "call-1", Name: "mcp__db__delete", Arguments: `{}`, CreatedAt: time.Now()}},
	})

	require.Empty(t, out[0], "server URL resolves to a Gram host despite the degraded match")
	require.Empty(t, validator.orgIDs)
}

// Cursor emits "MCP:<tool>" rather than the mcp__ form; it must take the same
// provenance path.
func TestScanner_CursorStyleToolNameUsesProvenance(t *testing.T) {
	t.Parallel()

	validator := &fakeValidator{denied: map[string]bool{}, orgIDs: nil}
	prov := &fakeProvenance{
		found:     map[string]telemetryrepo.MCPProvenance{"call-1": {Match: "https://evil.example/mcp", ServerURL: "https://evil.example/mcp", HookSource: "cursor"}},
		err:       nil,
		gotIDs:    nil,
		gotSince:  time.Time{},
		callCount: 0,
	}
	s := NewScanner(discardLogger(), validator, gramHosted(), prov, nil)

	out := s.Scan(t.Context(), "org-1", uuid.New(), uuid.Nil, nil, [][]ToolCall{
		{{ID: "call-1", Name: "MCP:delete_row", Arguments: `{}`, CreatedAt: time.Now()}},
	})

	require.Len(t, out[0], 1)
	require.Equal(t, "https://evil.example/mcp", out[0][0].Match)
}

func TestScanner_SkipsNonMCPAndNamelessCalls(t *testing.T) {
	t.Parallel()

	validator := &fakeValidator{denied: map[string]bool{"delete": true}, orgIDs: nil}
	prov := noProvenance()
	s := NewScanner(discardLogger(), validator, gramHosted(), prov, nil)

	out := s.Scan(t.Context(), "org-1", uuid.New(), uuid.Nil, nil, [][]ToolCall{
		{
			{ID: "a", Name: "", Arguments: `{}`, CreatedAt: time.Now()},              // nameless
			{ID: "b", Name: "Bash", Arguments: `{}`, CreatedAt: time.Now()},          // native, non-MCP
			{ID: "c", Name: "mcp__db__read", Arguments: `{}`, CreatedAt: time.Now()}, // MCP, allowed
		},
	})

	require.Len(t, out, 1)
	require.Empty(t, out[0])
	require.Equal(t, []string{"org-1"}, validator.orgIDs, "only the MCP-routed call reaches the validator")
	require.Equal(t, []string{"c"}, prov.gotIDs, "only MCP-routed calls are looked up")
}

func TestScanner_BatchPositionalAlignmentAndSingleLookup(t *testing.T) {
	t.Parallel()

	validator := &fakeValidator{denied: map[string]bool{}, orgIDs: nil}
	prov := &fakeProvenance{
		found: map[string]telemetryrepo.MCPProvenance{
			"call-0": {Match: "https://evil.example/mcp", ServerURL: "https://evil.example/mcp", HookSource: "claude"},
			"call-1": {Match: "https://app.getgram.ai/mcp/team", ServerURL: "https://app.getgram.ai/mcp/team", HookSource: "claude"},
			"call-2": {Match: "npx -y @acme/mcp", ServerURL: "", HookSource: "claude"},
		},
		err:       nil,
		gotIDs:    nil,
		gotSince:  time.Time{},
		callCount: 0,
	}
	s := NewScanner(discardLogger(), validator, gramHosted(), prov, nil)

	out := s.Scan(t.Context(), "org-1", uuid.New(), uuid.Nil, nil, [][]ToolCall{
		{{ID: "call-0", Name: "mcp__db__delete", Arguments: `{}`, CreatedAt: time.Now()}},
		{{ID: "call-1", Name: "mcp__db__read", Arguments: `{}`, CreatedAt: time.Now()}},
		{{ID: "call-2", Name: "mcp__fs__delete", Arguments: `{}`, CreatedAt: time.Now()}},
	})

	require.Len(t, out, 3)
	require.Len(t, out[0], 1)
	require.Equal(t, "https://evil.example/mcp", out[0][0].Match)
	require.Empty(t, out[1])
	require.Len(t, out[2], 1)
	require.Equal(t, "npx -y @acme/mcp", out[2][0].Match)

	require.Equal(t, 1, prov.callCount, "the whole batch resolves in a single lookup")
	require.ElementsMatch(t, []string{"call-0", "call-1", "call-2"}, prov.gotIDs)
}

// The lookup is bounded to the batch's own time range so the ClickHouse query
// stays on the telemetry table's time-ordered primary key.
func TestScanner_LookupIsBoundedByOldestMessage(t *testing.T) {
	t.Parallel()

	oldest := time.Now().Add(-48 * time.Hour)
	prov := noProvenance()
	s := NewScanner(discardLogger(), &fakeValidator{denied: map[string]bool{}, orgIDs: nil}, gramHosted(), prov, nil)

	s.Scan(t.Context(), "org-1", uuid.New(), uuid.Nil, nil, [][]ToolCall{
		{{ID: "call-0", Name: "mcp__db__read", Arguments: `{}`, CreatedAt: time.Now()}},
		{{ID: "call-1", Name: "mcp__db__read", Arguments: `{}`, CreatedAt: oldest}},
	})

	require.Equal(t, oldest.Add(-provenanceLookback), prov.gotSince)
}

func TestScanner_NoMCPCallsSkipsLookup(t *testing.T) {
	t.Parallel()

	prov := noProvenance()
	s := NewScanner(discardLogger(), &fakeValidator{denied: map[string]bool{}, orgIDs: nil}, gramHosted(), prov, nil)

	out := s.Scan(t.Context(), "org-1", uuid.New(), uuid.Nil, nil, [][]ToolCall{
		{{ID: "call-1", Name: "Bash", Arguments: `{}`, CreatedAt: time.Now()}},
	})

	require.Empty(t, out[0])
	require.Zero(t, prov.callCount, "no MCP calls: the lookup is never issued")
}

func TestFinding_UsesCanonicalRuleIDAndLeaksNoInternals(t *testing.T) {
	t.Parallel()

	s := NewScanner(discardLogger(), &fakeValidator{denied: map[string]bool{}, orgIDs: nil}, gramHosted(), noProvenance(), nil)

	f := s.finding(ToolCall{ID: "call-1", Name: "mcp__db__delete", Arguments: "", CreatedAt: time.Time{}}, "db")
	require.Equal(t, Rule, f.RuleID)
	require.NoError(t, scanners.ValidateRuleID(f.RuleID))
	require.Contains(t, f.Description, "mcp__db__delete")
	require.NotContains(t, f.Description, "x-gram-toolset-id", "must not leak validator internals")

	nameless := s.finding(ToolCall{ID: "call-2", Name: "", Arguments: "", CreatedAt: time.Time{}}, "db")
	require.Equal(t, Rule, nameless.RuleID)
	require.NotEmpty(t, nameless.Description)
}

func TestResolvedServerIdentity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		prov         telemetryrepo.MCPProvenance
		serverPrefix string
		wantValue    string
		wantResolved bool
	}{
		{"server url wins", telemetryrepo.MCPProvenance{Match: "db", ServerURL: "https://x.test/mcp", HookSource: ""}, "db", "https://x.test/mcp", true},
		{"url match", telemetryrepo.MCPProvenance{Match: "https://x.test/mcp", ServerURL: "", HookSource: ""}, "db", "https://x.test/mcp", true},
		{"stdio command match", telemetryrepo.MCPProvenance{Match: "npx -y @acme/mcp", ServerURL: "", HookSource: ""}, "db", "npx -y @acme/mcp", true},
		{"bare prefix is unresolved", telemetryrepo.MCPProvenance{Match: "db", ServerURL: "", HookSource: ""}, "db", "", false},
		{"empty is unresolved", telemetryrepo.MCPProvenance{Match: "", ServerURL: "", HookSource: ""}, "db", "", false},
		{"whitespace is unresolved", telemetryrepo.MCPProvenance{Match: "   ", ServerURL: "", HookSource: ""}, "db", "", false},
	}

	for _, tc := range cases {
		value, resolved := resolvedServerIdentity(tc.prov, tc.serverPrefix)
		require.Equal(t, tc.wantResolved, resolved, tc.name)
		require.Equal(t, tc.wantValue, value, tc.name)
	}
}

func TestIsHostedIdentity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		identity string
		want     bool
	}{
		{"gram url", "https://app.getgram.ai/mcp/team", true},
		{"third party url", "https://evil.example/mcp", false},
		{"third party stdio command", "npx -y @acme/mcp", false},
		{"bare token", "db", false},
		{"empty", "", false},
		// Gram's own install snippet for OAuth-backed servers is a stdio
		// entry fronting a Gram URL; it must not read as shadow MCP.
		{"gram mcp-remote snippet", "npx mcp-remote@0.1.25 https://app.getgram.ai/mcp/team-foo", true},
		{"mcp-remote with -y launcher flag", "npx -y mcp-remote https://app.getgram.ai/mcp/x", true},
		{"mcp-remote with headers", "npx mcp-remote https://app.getgram.ai/mcp/x --header Authorization:${TOKEN}", true},
		// A scoped package is a different package. Trusting any name ending in
		// "mcp-remote" would let @evil/mcp-remote, which can connect anywhere,
		// launder a Gram URL argument into a hosted verdict.
		{"scoped lookalike rejected", "npx @evil/mcp-remote https://app.getgram.ai/mcp/x", false},
		{"scoped vendor fork rejected", "npx @speakeasy/mcp-remote@1.2.3 https://app.getgram.ai/mcp/x", false},
		{"path lookalike rejected", "npx foo/mcp-remote https://app.getgram.ai/mcp/x", false},
		{"mcp-remote to third party", "npx mcp-remote https://evil.example/mcp", false},
		// A Gram-shaped path on a foreign host stays shadow: the hosted check
		// is on the host, never the path.
		{"gram path on foreign host", "npx mcp-remote https://evil.example/mcp/team-foo", false},
		// Only the proxy's target counts. A local server carrying an unrelated
		// Gram URL must not clear the check, or evasion costs one extra flag.
		{"unrelated gram url argument", "npx @evil/mcp --docs https://app.getgram.ai/docs", false},
		{"gram url without mcp-remote", "node server.js --ref https://app.getgram.ai/mcp/x", false},
		// First URL after the spec wins, so a trailing argument cannot
		// displace the real target.
		{"trailing gram url does not displace target", "npx mcp-remote https://evil.example/mcp --header X:https://app.getgram.ai", false},
	}

	for _, tc := range cases {
		require.Equal(t, tc.want, isHostedIdentity(tc.identity, nil), tc.name)
	}
}

// A custom domain resolved for the org counts as Gram-hosted.
func TestIsHostedIdentity_TrustedHosts(t *testing.T) {
	t.Parallel()

	hosts := []string{"mcp.customer.example"}
	require.True(t, isHostedIdentity("https://mcp.customer.example/mcp/team", hosts))
	require.True(t, isHostedIdentity("npx mcp-remote https://mcp.customer.example/mcp/team", hosts))
	require.False(t, isHostedIdentity("https://other.example/mcp", hosts))
}

// The organization's custom-domain lookup sits behind this, so it must not be
// repeated per scanned call.
func TestScanner_ResolvesTrustedHostsOncePerScan(t *testing.T) {
	t.Parallel()

	hosted := gramHosted()
	prov := &fakeProvenance{
		found: map[string]telemetryrepo.MCPProvenance{
			"call-0": {Match: "https://evil.example/mcp", ServerURL: "https://evil.example/mcp", HookSource: "claude"},
			"call-1": {Match: "https://other.example/mcp", ServerURL: "https://other.example/mcp", HookSource: "claude"},
		},
		err:       nil,
		gotIDs:    nil,
		gotSince:  time.Time{},
		callCount: 0,
	}
	s := NewScanner(discardLogger(), &fakeValidator{denied: map[string]bool{}, orgIDs: nil}, hosted, prov, nil)

	s.Scan(t.Context(), "org-1", uuid.New(), uuid.Nil, nil, [][]ToolCall{
		{{ID: "call-0", Name: "mcp__db__delete", Arguments: `{}`, CreatedAt: time.Now()}},
		{{ID: "call-1", Name: "mcp__fs__delete", Arguments: `{}`, CreatedAt: time.Now()}},
	})

	require.Equal(t, 1, hosted.calls)
}

// Without a trusted-host list the scanner cannot tell a Gram host from a third
// party, so it must not judge on the incomplete list. Judging anyway would
// persist shadow findings for calls to an org's own verified custom domain.
func TestScanner_HostResolutionFailureFallsBackToSignature(t *testing.T) {
	t.Parallel()

	hosted := gramHosted()
	hosted.err = errors.New("boom")
	validator := &fakeValidator{denied: map[string]bool{"delete": true}, orgIDs: nil}
	prov := &fakeProvenance{
		found:     map[string]telemetryrepo.MCPProvenance{"call-1": {Match: "https://mcp.customer.example/mcp", ServerURL: "https://mcp.customer.example/mcp", HookSource: "claude"}},
		err:       nil,
		gotIDs:    nil,
		gotSince:  time.Time{},
		callCount: 0,
	}
	coverage := &fakeCoverage{got: nil}
	s := NewScanner(discardLogger(), validator, hosted, prov, coverage)

	out := s.Scan(t.Context(), "org-1", uuid.New(), uuid.Nil, nil, [][]ToolCall{
		{{ID: "call-1", Name: "mcp__db__delete", Arguments: `{}`, CreatedAt: time.Now(), Sender: "Claude"}},
	})

	require.Len(t, out[0], 1, "the signature check still denies this call")
	require.Equal(t, "db", out[0][0].Match, "decided by the fallback, not by provenance")
	require.Equal(t, []string{"org-1"}, validator.orgIDs, "host resolution failure must reach the signature validator")
	require.Equal(t, []recordedResolution{{hookSource: "claude", resolution: ResolutionUnresolved}}, coverage.got)
}

// The same failure must not manufacture findings for calls the signature check
// clears.
func TestScanner_HostResolutionFailureDoesNotFlagSignedCalls(t *testing.T) {
	t.Parallel()

	hosted := gramHosted()
	hosted.err = errors.New("boom")
	prov := &fakeProvenance{
		found:     map[string]telemetryrepo.MCPProvenance{"call-1": {Match: "https://mcp.customer.example/mcp", ServerURL: "https://mcp.customer.example/mcp", HookSource: "claude"}},
		err:       nil,
		gotIDs:    nil,
		gotSince:  time.Time{},
		callCount: 0,
	}
	s := NewScanner(discardLogger(), &fakeValidator{denied: map[string]bool{}, orgIDs: nil}, hosted, prov, nil)

	out := s.Scan(t.Context(), "org-1", uuid.New(), uuid.Nil, nil, [][]ToolCall{
		{{ID: "call-1", Name: "mcp__db__delete", Arguments: `{}`, CreatedAt: time.Now()}},
	})

	require.Empty(t, out[0])
}

// No MCP calls means neither the provenance query nor the trusted-host lookup
// should be issued.
func TestScanner_NoMCPCallsSkipsHostLookup(t *testing.T) {
	t.Parallel()

	hosted := gramHosted()
	s := NewScanner(discardLogger(), &fakeValidator{denied: map[string]bool{}, orgIDs: nil}, hosted, noProvenance(), nil)

	s.Scan(t.Context(), "org-1", uuid.New(), uuid.Nil, nil, [][]ToolCall{
		{{ID: "call-1", Name: "Bash", Arguments: `{}`, CreatedAt: time.Now()}},
	})

	require.Zero(t, hosted.calls)
}

// A Gram server installed through Gram's own stdio snippet resolves to a
// launch command, not a URL. Flagging every stdio server would flag calls to
// Gram's own servers — the exact false positive this scanner exists to avoid.
func TestScanner_StdioCommandFrontingGramURLIsClean(t *testing.T) {
	t.Parallel()

	validator := &fakeValidator{denied: map[string]bool{"delete": true}, orgIDs: nil}
	prov := &fakeProvenance{
		found:     map[string]telemetryrepo.MCPProvenance{"call-1": {Match: "npx mcp-remote@0.1.25 https://app.getgram.ai/mcp/team-foo", ServerURL: "", HookSource: "claude"}},
		err:       nil,
		gotIDs:    nil,
		gotSince:  time.Time{},
		callCount: 0,
	}
	coverage := &fakeCoverage{got: nil}
	s := NewScanner(discardLogger(), validator, gramHosted(), prov, coverage)

	out := s.Scan(t.Context(), "org-1", uuid.New(), uuid.Nil, nil, [][]ToolCall{
		{{ID: "call-1", Name: "mcp__db__delete", Arguments: `{}`, CreatedAt: time.Now()}},
	})

	require.Empty(t, out[0], "a stdio server fronting a Gram URL is Gram-hosted")
	require.Empty(t, validator.orgIDs)
	require.Equal(t, []recordedResolution{{hookSource: "claude", resolution: ResolutionHosted}}, coverage.got)
}

// The hosted check must not be gated on the identity parsing as an http(s)
// URL: IsGramHostedMCPURL accepts forms a stricter pre-filter would reject
// before the host was ever compared.
func TestScanner_NonHTTPSchemeGramURLIsClean(t *testing.T) {
	t.Parallel()

	prov := &fakeProvenance{
		found:     map[string]telemetryrepo.MCPProvenance{"call-1": {Match: "", ServerURL: "sse://app.getgram.ai/mcp/team", HookSource: "cursor"}},
		err:       nil,
		gotIDs:    nil,
		gotSince:  time.Time{},
		callCount: 0,
	}
	s := NewScanner(discardLogger(), &fakeValidator{denied: map[string]bool{}, orgIDs: nil}, gramHosted(), prov, nil)

	out := s.Scan(t.Context(), "org-1", uuid.New(), uuid.Nil, nil, [][]ToolCall{
		{{ID: "call-1", Name: "mcp__db__delete", Arguments: `{}`, CreatedAt: time.Now()}},
	})

	require.Empty(t, out[0])
}

// Senders relay the server URL straight off a client payload without
// normalizing, so a stray newline must not defeat the hosted check.
func TestScanner_UntrimmedServerURLIsClean(t *testing.T) {
	t.Parallel()

	prov := &fakeProvenance{
		found:     map[string]telemetryrepo.MCPProvenance{"call-1": {Match: "", ServerURL: "https://app.getgram.ai/mcp/team\n", HookSource: "cursor"}},
		err:       nil,
		gotIDs:    nil,
		gotSince:  time.Time{},
		callCount: 0,
	}
	s := NewScanner(discardLogger(), &fakeValidator{denied: map[string]bool{}, orgIDs: nil}, gramHosted(), prov, nil)

	out := s.Scan(t.Context(), "org-1", uuid.New(), uuid.Nil, nil, [][]ToolCall{
		{{ID: "call-1", Name: "mcp__db__delete", Arguments: `{}`, CreatedAt: time.Now()}},
	})

	require.Empty(t, out[0])
}
