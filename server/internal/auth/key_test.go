package auth

import (
	"encoding/hex"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestEffectiveScopes pins the one-way scope implications, especially the
// device-agent split: an org `agent` install key implies `agent_user` (so it
// still reads the data endpoints during the transition), but a per-user
// `agent_user` key never implies `agent` (so it cannot reach the mint endpoint).
func TestEffectiveScopes(t *testing.T) {
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
