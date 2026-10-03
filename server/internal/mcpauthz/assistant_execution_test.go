package mcpauthz

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/stretchr/testify/require"
)

func executionForTest() assistantidentity.Execution {
	policy := json.RawMessage(`{"requested":[],"effective":[]}`)
	digest := sha256.Sum256(policy)
	return assistantidentity.Execution{
		Version: 1, Issuer: "https://gram.example", ThreadID: uuid.New(), EventID: "event-test", Mode: assistantidentity.ExecutionWorkload,
		Identity: assistantidentity.Identity{OrganizationID: "org-test", ProjectID: uuid.New(), AssistantID: uuid.New(), AgentID: uuid.New(), TriggerID: uuid.New(), IssuerID: uuid.New(), Subject: "assistant-trigger:stable", AssistantGeneration: 1, TriggerGeneration: 1},
		Ceiling:  assistantidentity.CeilingSnapshot{EncodingVersion: runtimepolicy.CurrentDelegatedPolicyVersion, Policy: policy, Digest: hex.EncodeToString(digest[:])},
	}
}

func TestAssistantExecutionTokenNamespacesAndGate(t *testing.T) {
	t.Parallel()
	issuer, _ := issuerForTest(t)
	execution := executionForTest()
	raw, err := issuer.MintAssistantExecution(execution)
	require.NoError(t, err)
	claims, err := issuer.ValidateAssistantExecution(raw)
	require.NoError(t, err)
	require.Equal(t, execution, claims.Execution)
	require.Equal(t, execution.Identity.Subject, claims.Subject)
	require.Equal(t, jwt.ClaimStrings{AssistantExecutionAudience}, claims.Audience)
	parsed, _, err := jwt.NewParser().ParseUnverified(raw, jwt.MapClaims{})
	require.NoError(t, err)
	wire, ok := parsed.Claims.(jwt.MapClaims)
	require.True(t, ok)
	require.NotContains(t, wire, "user_id")
	require.ErrorIs(t, assistantidentity.AdmitExecution(execution), assistantidentity.ErrExecutionAdmissionRequired)
	execution.HumanUserID = "human-a"
	require.Error(t, execution.Check(), "workload cannot smuggle a human actor")
	execution.Mode = assistantidentity.ExecutionWorkloadHuman
	require.NoError(t, execution.Check())
	require.ErrorIs(t, assistantidentity.AdmitExecution(execution), assistantidentity.ErrExecutionAdmissionRequired)
}

func TestAssistantExecutionRejectsConfusedOrMalformedSignedClaims(t *testing.T) {
	t.Parallel()
	issuer, _ := issuerForTest(t)
	for _, tc := range []struct {
		name   string
		mutate func(*AssistantExecutionClaims, map[string]any)
	}{
		{"outbound assertion type", func(c *AssistantExecutionClaims, h map[string]any) { h["typ"] = "speakeasy-identity+jwt" }},
		{"business audience", func(c *AssistantExecutionClaims, h map[string]any) {
			c.Audience = jwt.ClaimStrings{"https://business.example"}
		}},
		{"multiple audiences", func(c *AssistantExecutionClaims, h map[string]any) {
			c.Audience = append(c.Audience, "https://business.example")
		}},
		{"wrong issuer", func(c *AssistantExecutionClaims, h map[string]any) { c.Issuer = "https://other.example" }},
		{"wrong root", func(c *AssistantExecutionClaims, h map[string]any) { c.Subject = "different-root" }},
		{"unknown key", func(c *AssistantExecutionClaims, h map[string]any) { h["kid"] = "other-key" }},
		{"expired", func(c *AssistantExecutionClaims, h map[string]any) {
			c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
		}},
		{"unknown version", func(c *AssistantExecutionClaims, h map[string]any) { c.Execution.Version = 99 }},
		{"missing expiration", func(c *AssistantExecutionClaims, h map[string]any) { c.ExpiresAt = nil }},
		{"fabricated human", func(c *AssistantExecutionClaims, h map[string]any) { c.Execution.HumanUserID = "owner" }},
		{"corrupt ceiling", func(c *AssistantExecutionClaims, h map[string]any) { c.Execution.Ceiling.Digest = "invalid" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw, err := issuer.MintAssistantExecution(executionForTest())
			require.NoError(t, err)
			c, err := issuer.ValidateAssistantExecution(raw)
			require.NoError(t, err)
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, c)
			token.Header["typ"] = AssistantExecutionType
			token.Header["kid"] = issuer.kid
			tc.mutate(c, token.Header)
			raw, err = token.SignedString(issuer.key)
			require.NoError(t, err)
			_, err = issuer.ValidateAssistantExecution(raw)
			switch tc.name {
			case "wrong issuer", "unknown key", "wrong root":
				require.ErrorIs(t, err, assistantidentity.ErrInvalidIdentity)
			default:
				require.Error(t, err)
			}
		})
	}
}

func TestAssistantExecutionSigningRotation(t *testing.T) {
	t.Parallel()
	privateA, publicA := keyPEM(t, 2048)
	privateB, publicB := keyPEM(t, 2048)
	a, err := New(privateA, publicA, "https://gram.example", false)
	require.NoError(t, err)
	b, err := New(privateB, publicA+publicB, "https://gram.example", false)
	require.NoError(t, err)
	raw, err := a.MintAssistantExecution(executionForTest())
	require.NoError(t, err)
	_, err = b.ValidateAssistantExecution(raw)
	require.NoError(t, err)
	retired, err := New(privateB, publicB, "https://gram.example", false)
	require.NoError(t, err)
	_, err = retired.ValidateAssistantExecution(raw)
	require.ErrorIs(t, err, assistantidentity.ErrInvalidIdentity)
}
