package agentcapture

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func testAnonymizer(t *testing.T, seed byte) *Anonymizer {
	t.Helper()
	salt := make([]byte, saltSize)
	for i := range salt {
		salt[i] = seed
	}
	return NewAnonymizer(salt)
}

func scrubToMap(t *testing.T, a *Anonymizer, raw string) map[string]any {
	t.Helper()
	out, err := a.ScrubJSON(raw)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &m))
	return m
}

// nested walks a decoded JSON object down the given keys and returns the
// value at the end of the path.
func nested(t *testing.T, m map[string]any, path ...string) any {
	t.Helper()
	var current any = m
	for _, key := range path {
		node, ok := current.(map[string]any)
		require.True(t, ok, "expected object at %q in path %v", key, path)
		current = node[key]
	}
	return current
}

func nestedString(t *testing.T, m map[string]any, path ...string) string {
	t.Helper()
	s, ok := nested(t, m, path...).(string)
	require.True(t, ok, "expected string at path %v", path)
	return s
}

func TestAnonymizerScrubJSONPseudonymizesEmails(t *testing.T) {
	t.Parallel()
	a := testAnonymizer(t, 1)

	raw := `{"user":{"email":"alice@example.com"},"gram":{"auth":{"user_email":"bob@example.com"}}}`
	first := scrubToMap(t, a, raw)

	userEmail := nestedString(t, first, "user", "email")
	authEmail := nestedString(t, first, "gram", "auth", "user_email")

	require.NotEqual(t, "alice@example.com", userEmail)
	require.NotEqual(t, "bob@example.com", authEmail)
	require.True(t, strings.HasSuffix(userEmail, "@anon.invalid"))
	require.True(t, strings.HasSuffix(authEmail, "@anon.invalid"))
	require.NotEqual(t, userEmail, authEmail)

	// Deterministic: the same input under the same salt maps to the same
	// pseudonym.
	second := scrubToMap(t, a, raw)
	require.Equal(t, userEmail, nestedString(t, second, "user", "email"))
}

func TestAnonymizerScrubJSONPseudonymizesProviderIdentity(t *testing.T) {
	t.Parallel()
	a := testAnonymizer(t, 1)

	accountUUID := "0d5c9bbc-1b13-4d54-8e2c-9f3c1a2b3c4d"
	raw := `{"user":{"account_uuid":"` + accountUUID + `"},"gram":{"external_user":{"id":"user_abc123"},"external_org_id":"org_xyz789"}}`
	out := scrubToMap(t, a, raw)

	scrubbedUUID := nestedString(t, out, "user", "account_uuid")
	require.NotEqual(t, accountUUID, scrubbedUUID)
	// UUID-shaped inputs stay UUID-shaped so downstream parsers keep working.
	_, err := uuid.Parse(scrubbedUUID)
	require.NoError(t, err)

	externalUser := nestedString(t, out, "gram", "external_user", "id")
	require.NotEqual(t, "user_abc123", externalUser)
	require.True(t, strings.HasPrefix(externalUser, "anon-"))

	externalOrg := nestedString(t, out, "gram", "external_org_id")
	require.NotEqual(t, "org_xyz789", externalOrg)
	require.True(t, strings.HasPrefix(externalOrg, "anon-"))
}

func TestAnonymizerDifferentSaltsDiverge(t *testing.T) {
	t.Parallel()
	raw := `{"user":{"email":"alice@example.com"}}`

	one := scrubToMap(t, testAnonymizer(t, 1), raw)
	two := scrubToMap(t, testAnonymizer(t, 2), raw)

	require.NotEqual(t,
		nestedString(t, one, "user", "email"),
		nestedString(t, two, "user", "email"),
	)
}

func TestAnonymizerScrubJSONScrubsContent(t *testing.T) {
	t.Parallel()
	a := testAnonymizer(t, 1)

	raw := `{"gen_ai":{"tool":{"call":{"arguments":"{\"path\":\"/etc/passwd\"}"}},"usage":{"input_tokens":42}},"prompt":"tell me a secret","gram":{"tool":{"name":"Read"}}}`
	out := scrubToMap(t, a, raw)

	args := nestedString(t, out, "gen_ai", "tool", "call", "arguments")
	require.True(t, strings.HasPrefix(args, "[scrubbed "))

	require.Equal(t, "[scrubbed 16 bytes]", nestedString(t, out, "prompt"))

	// Non-content values survive untouched: numbers, and strings under
	// unmatched paths.
	tokens, ok := nested(t, out, "gen_ai", "usage", "input_tokens").(float64)
	require.True(t, ok)
	require.InEpsilon(t, 42.0, tokens, 0.0001)
	require.Equal(t, "Read", nestedString(t, out, "gram", "tool", "name"))
}

func TestAnonymizerScrubJSONWalksArrays(t *testing.T) {
	t.Parallel()
	a := testAnonymizer(t, 1)

	raw := `{"user":{"email":["a@example.com","b@example.com"]}}`
	out := scrubToMap(t, a, raw)

	emails, ok := nested(t, out, "user", "email").([]any)
	require.True(t, ok)
	require.Len(t, emails, 2)
	for _, e := range emails {
		s, ok := e.(string)
		require.True(t, ok)
		require.True(t, strings.HasSuffix(s, "@anon.invalid"))
	}
	require.NotEqual(t, emails[0], emails[1])
}

func TestAnonymizerScrubRowScrubsLongBody(t *testing.T) {
	t.Parallel()
	a := testAnonymizer(t, 1)

	longBody := strings.Repeat("x", maxClearBodyLen+1)
	row := logRow{
		ID:                   "0198c3e4-0000-7000-8000-000000000000",
		TimeUnixNano:         0,
		ObservedTimeUnixNano: 0,
		SeverityText:         nil,
		Body:                 longBody,
		TraceID:              nil,
		SpanID:               nil,
		Attributes:           json.RawMessage(`{}`),
		ResourceAttributes:   json.RawMessage(`{}`),
		GramProjectID:        "",
		GramDeploymentID:     nil,
		GramFunctionID:       nil,
		GramURN:              "",
		ServiceName:          "",
		ServiceVersion:       nil,
		GramChatID:           nil,
		EventURN:             "",
	}
	require.NoError(t, a.ScrubRow(&row))
	require.Equal(t, "[scrubbed 201 bytes]", row.Body)

	row.Body = "Claude Chat cost metrics"
	require.NoError(t, a.ScrubRow(&row))
	require.Equal(t, "Claude Chat cost metrics", row.Body)
}

func TestLoadOrCreateSaltRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "anonymization-salt")

	created, err := loadOrCreateSalt(path)
	require.NoError(t, err)
	require.Len(t, created, saltSize)

	reloaded, err := loadOrCreateSalt(path)
	require.NoError(t, err)
	require.Equal(t, created, reloaded)

	// The salt file is hex text so it can be inspected and copied around.
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, strings.TrimSpace(string(raw)), strings.ToLower(strings.TrimSpace(string(raw))))
}

func TestOriginFromEventURN(t *testing.T) {
	t.Parallel()
	require.Equal(t, "provider_api", originFromEventURN("urn:telemetry:provider_api:metric:user_cost_report"))
	require.Equal(t, "agent_hook", originFromEventURN("urn:telemetry:agent_hook:log:pretooluse"))
	require.Equal(t, "unknown", originFromEventURN(""))
	require.Equal(t, "unknown", originFromEventURN("garbage"))
	require.Equal(t, "unknown", originFromEventURN("urn:telemetry:../escape:log:x"))
}
