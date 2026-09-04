package auth

import (
	"encoding/hex"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
)

func TestClassifyPrincipalAPIKeyUsesOnlyCompleteLoadedProfile(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	createdAt := now.Add(-time.Hour)
	valid := keysrepo.GetAPIKeyByKeyHashRow{
		CreatedByUserID:        "user_authorizer",
		SubjectUrn:             pgtype.Text{String: "agent:018f8d7b-58d7-7cc4-bb16-9f8c6b99a001", Valid: true},
		DelegatedGrants:        []byte(`{"requested":[],"effective":[]}`),
		DelegatedGrantsVersion: pgtype.Int4{Int32: 1, Valid: true},
		ExpiresAt:              pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true},
		CreatedAt:              pgtype.Timestamptz{Time: createdAt, Valid: true},
	}

	actor, credential, principal, err := classifyPrincipalAPIKey(valid, now)
	require.NoError(t, err)
	require.True(t, principal)
	require.Equal(t, valid.SubjectUrn.String, actor.String())
	require.Equal(t, valid.CreatedByUserID, credential.AuthorizerUserID)
	require.Equal(t, valid.DelegatedGrants, credential.DelegatedGrants)
	require.Equal(t, int32(1), credential.DelegatedGrantsVersion)

	legacy := keysrepo.GetAPIKeyByKeyHashRow{Scopes: []string{APIKeyScopeAgent.String()}}
	_, _, principal, err = classifyPrincipalAPIKey(legacy, now)
	require.NoError(t, err)
	require.False(t, principal, "legacy scope names cannot select principal authorization")

	invalid := map[string]keysrepo.GetAPIKeyByKeyHashRow{
		"subject only": func() keysrepo.GetAPIKeyByKeyHashRow {
			row := keysrepo.GetAPIKeyByKeyHashRow{}
			row.SubjectUrn = valid.SubjectUrn
			return row
		}(),
		"policy only": {DelegatedGrants: valid.DelegatedGrants},
		"non-agent subject": func() keysrepo.GetAPIKeyByKeyHashRow {
			row := valid
			row.SubjectUrn.String = "user:user_123"
			return row
		}(),
		"malformed subject": func() keysrepo.GetAPIKeyByKeyHashRow {
			row := valid
			row.SubjectUrn.String = "agent:not-a-uuid"
			return row
		}(),
		"legacy scopes present": func() keysrepo.GetAPIKeyByKeyHashRow {
			row := valid
			row.Scopes = []string{APIKeyScopeProducer.String()}
			return row
		}(),
		"expired": func() keysrepo.GetAPIKeyByKeyHashRow { row := valid; row.ExpiresAt.Time = now; return row }(),
		"exceeds maximum lifetime": func() keysrepo.GetAPIKeyByKeyHashRow {
			row := valid
			row.ExpiresAt.Time = createdAt.Add(maxAgentAPIKeyLifetime + time.Second)
			return row
		}(),
	}
	for name, row := range invalid {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, _, principal, err := classifyPrincipalAPIKey(row, now)
			require.True(t, principal)
			require.Error(t, err)
		})
	}
}

func TestPrincipalAPIKeySupportsOnlyPrincipalSafeTransportRoutes(t *testing.T) {
	t.Parallel()
	require.False(t, principalAPIKeySupportsTransportScopes(nil))
	require.True(t, principalAPIKeySupportsTransportScopes([]string{"consumer"}))
	require.True(t, principalAPIKeySupportsTransportScopes([]string{"producer"}))
	for _, scope := range []string{"agent", "agent_user", "chat", "hooks", "unknown"} {
		require.False(t, principalAPIKeySupportsTransportScopes([]string{scope}), scope)
	}
}

// TestEffectiveScopes pins the one-way scope implications, especially the
// device-agent split: an org `agent` install key implies `agent_user` (so it
// still reads the data endpoints during the transition), but a per-user
// `agent_user` key never implies `agent` (so it cannot reach the mint endpoint).
func TestEffectiveScopes(t *testing.T) {
	t.Parallel()
	has := func(scopes []string, s APIKeyScope) bool { return slices.Contains(scopes, s.String()) }

	tests := []struct {
		name          string
		in            []APIKeyScope
		wantAgent     bool
		wantAgentUser bool
		wantConsumer  bool
		wantChat      bool
	}{
		{
			name:          "agent implies agent_user (one-way)",
			in:            []APIKeyScope{APIKeyScopeAgent},
			wantAgent:     true,
			wantAgentUser: true,
		},
		{
			name:          "agent_user does NOT imply agent",
			in:            []APIKeyScope{APIKeyScopeAgentUser},
			wantAgent:     false,
			wantAgentUser: true,
		},
		{
			name:         "producer implies consumer and chat",
			in:           []APIKeyScope{APIKeyScopeProducer},
			wantConsumer: true,
			wantChat:     true,
		},
		{
			name:          "consumer alone implies nothing agent-related",
			in:            []APIKeyScope{APIKeyScopeConsumer},
			wantAgent:     false,
			wantAgentUser: false,
			wantConsumer:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw := make([]string, len(tt.in))
			for i, s := range tt.in {
				raw[i] = s.String()
			}
			got := effectiveScopes(raw)

			if has(got, APIKeyScopeAgent) != tt.wantAgent {
				t.Errorf("agent = %v, want %v (scopes=%v)", has(got, APIKeyScopeAgent), tt.wantAgent, got)
			}
			if has(got, APIKeyScopeAgentUser) != tt.wantAgentUser {
				t.Errorf("agent_user = %v, want %v (scopes=%v)", has(got, APIKeyScopeAgentUser), tt.wantAgentUser, got)
			}
			if tt.wantConsumer && !has(got, APIKeyScopeConsumer) {
				t.Errorf("consumer missing (scopes=%v)", got)
			}
			if tt.wantChat && !has(got, APIKeyScopeChat) {
				t.Errorf("chat missing (scopes=%v)", got)
			}
		})
	}
}

// TestEffectiveScopes_NoMutation ensures the input slice is not mutated.
func TestEffectiveScopes_NoMutation(t *testing.T) {
	t.Parallel()
	in := []string{APIKeyScopeAgent.String()}
	_ = effectiveScopes(in)
	if len(in) != 1 || in[0] != APIKeyScopeAgent.String() {
		t.Fatalf("input mutated: %v", in)
	}
}

// TestDeviceAgentKeyName pins the DNO-383 device-agent key-name shape:
// device-agent:<userID>:<YYYYMMDD-HHMMSS>:<8 hex chars>. The suffix is fresh
// crypto/rand entropy (the function takes no token, so it cannot embed
// secret-derived bytes), and each call is unique.
func TestDeviceAgentKeyName(t *testing.T) {
	t.Parallel()

	const userID = "user-123"
	name, err := DeviceAgentKeyName(userID)
	require.NoError(t, err)

	parts := strings.Split(name, ":")
	require.Len(t, parts, 4, "name is device-agent:userID:timestamp:entropy")
	require.Equal(t, "device-agent", parts[0])
	require.Equal(t, userID, parts[1])

	_, err = time.Parse("20060102-150405", parts[2])
	require.NoError(t, err, "third segment is a parseable timestamp")

	require.Len(t, parts[3], 8, "entropy suffix is 4 bytes = 8 hex chars")
	_, err = hex.DecodeString(parts[3])
	require.NoError(t, err, "entropy suffix is hex")

	// Two mints for the same user do not collide (fresh entropy each time).
	other, err := DeviceAgentKeyName(userID)
	require.NoError(t, err)
	require.NotEqual(t, name, other)
}

func TestIsOrgWidePluginHooksAPIKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		testName  string
		keyName   string
		key       string
		keyPrefix string
		want      bool
	}{
		{testName: "publish hooks key", keyName: "plugins-hooks-20260713-104500-abcdef", key: "gram_live_abcdef012345", keyPrefix: "gram_live_abcde", want: true},
		{testName: "download hooks key", keyName: "plugins-hooks-download-20260713-104500-0123ab", key: "gram_live_0123ababcdef", keyPrefix: "gram_live_0123a", want: true},
		{testName: "matching name but unrelated token", keyName: "plugins-hooks-20260713-104500-abcdef", key: "gram_live_123456abcdef", keyPrefix: "gram_live_12345", want: false},
		{testName: "legacy personal name", keyName: "plugins-hooks", want: false},
		{testName: "personal suffix", keyName: "plugins-hooks-personal", want: false},
		{testName: "non-hex suffix", keyName: "plugins-hooks-20260713-104500-nothex", want: false},
		{testName: "uppercase suffix", keyName: "plugins-hooks-20260713-104500-ABCDEF", want: false},
		{testName: "fractional seconds", keyName: "plugins-hooks-20260713-104500.5-abcdef", want: false},
		{testName: "invalid timestamp", keyName: "plugins-hooks-20261340-256199-abcdef", want: false},
		{testName: "mcp purpose", keyName: "plugins-mcp-20260713-104500-abcdef", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, IsOrgWidePluginHooksAPIKey(tt.keyName, tt.key, tt.keyPrefix))
		})
	}
}

func TestIsLiteLLMAPIKeyName(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"litellm-1785700000000-1234abcd":                                  true,
		"litellm-0c082267fdc3492fb43250ecddc402e6-1785700000000-1234abcd": true,
		"litellm-personal":               false,
		"litellm-178570000000-1234abcd":  false,
		"litellm-1785700000000-1234ABCd": false,
		"litellm-1785700000000-1234abc":  false,
		"other-1785700000000-1234abcd":   false,
	}
	for name, expected := range tests {
		require.Equal(t, expected, IsLiteLLMAPIKeyName(name), name)
	}
}

func TestLiteLLMInstanceIDFromAPIKeyName(t *testing.T) {
	t.Parallel()

	expected := uuid.MustParse("0c082267-fdc3-492f-b432-50ecddc402e6")
	actual, ok := LiteLLMInstanceIDFromAPIKeyName("litellm-0c082267fdc3492fb43250ecddc402e6-1785700000000-1234abcd")
	require.True(t, ok)
	require.Equal(t, expected, actual)

	_, ok = LiteLLMInstanceIDFromAPIKeyName("litellm-1785700000000-1234abcd")
	require.False(t, ok)
}
