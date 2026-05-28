package access

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	shadowMCPApprovalRequestTokenIssuer  = "gram"
	shadowMCPApprovalRequestTokenSubject = "shadow_mcp_approval_request" // #nosec G101 -- JWT subject label, not a credential.
	shadowMCPApprovalRequestTokenPrefix  = "smar1."
)

type ShadowMCPApprovalRequestTokenInput struct {
	OrganizationID         string
	ProjectID              string
	RequesterUserID        string
	ObservedName           *string
	ObservedFullURL        *string
	ObservedURLHost        *string
	ObservedServerIdentity *string
	ToolName               *string
	ToolCall               *string
	BlockReason            *string
	RiskPolicyID           *string
	RiskResultID           *string
}

type shadowMCPApprovalRequestClaims struct {
	OrganizationID         string  `json:"organization_id"`
	ProjectID              string  `json:"project_id"`
	RequesterUserID        string  `json:"requester_user_id,omitempty"`
	ObservedName           *string `json:"observed_name,omitempty"`
	ObservedFullURL        *string `json:"observed_full_url,omitempty"`
	ObservedURLHost        *string `json:"observed_url_host,omitempty"`
	ObservedServerIdentity *string `json:"observed_server_identity,omitempty"`
	ToolName               *string `json:"tool_name,omitempty"`
	ToolCall               *string `json:"tool_call,omitempty"`
	BlockReason            *string `json:"block_reason,omitempty"`
	RiskPolicyID           *string `json:"risk_policy_id,omitempty"`
	RiskResultID           *string `json:"risk_result_id,omitempty"`
	jwt.RegisteredClaims
}

func GenerateShadowMCPApprovalRequestToken(jwtSecret string, input ShadowMCPApprovalRequestTokenInput, ttl time.Duration) (string, time.Time, error) {
	if strings.TrimSpace(jwtSecret) == "" {
		return "", time.Time{}, fmt.Errorf("jwt secret is required")
	}
	now := time.Now()
	expiry := now.Add(ttl).Truncate(time.Second)
	claims := shadowMCPApprovalRequestClaims{
		OrganizationID:         strings.TrimSpace(input.OrganizationID),
		ProjectID:              strings.TrimSpace(input.ProjectID),
		RequesterUserID:        strings.TrimSpace(input.RequesterUserID),
		ObservedName:           normalizeOptionalString(input.ObservedName),
		ObservedFullURL:        normalizeOptionalString(input.ObservedFullURL),
		ObservedURLHost:        normalizeOptionalLowerString(input.ObservedURLHost),
		ObservedServerIdentity: normalizeOptionalString(input.ObservedServerIdentity),
		ToolName:               normalizeOptionalString(input.ToolName),
		ToolCall:               normalizeOptionalString(input.ToolCall),
		BlockReason:            normalizeOptionalString(input.BlockReason),
		RiskPolicyID:           normalizeOptionalString(input.RiskPolicyID),
		RiskResultID:           normalizeOptionalString(input.RiskResultID),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    shadowMCPApprovalRequestTokenIssuer,
			Subject:   shadowMCPApprovalRequestTokenSubject,
			Audience:  nil,
			ExpiresAt: jwt.NewNumericDate(expiry),
			NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        uuid.NewString(),
		},
	}
	if err := validateShadowMCPApprovalRequestClaims(&claims); err != nil {
		return "", time.Time{}, err
	}

	plaintext, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("marshal shadow mcp approval request token: %w", err)
	}
	token, err := encryptShadowMCPApprovalRequestToken(jwtSecret, plaintext)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expiry, nil
}

func parseShadowMCPApprovalRequestToken(jwtSecret string, tokenString string) (*shadowMCPApprovalRequestClaims, error) {
	if strings.TrimSpace(jwtSecret) == "" {
		return nil, fmt.Errorf("jwt secret is required")
	}
	plaintext, err := decryptShadowMCPApprovalRequestToken(jwtSecret, tokenString)
	if err != nil {
		return nil, err
	}
	var claims shadowMCPApprovalRequestClaims
	if err := json.Unmarshal(plaintext, &claims); err != nil {
		return nil, fmt.Errorf("unmarshal shadow mcp approval request token: %w", err)
	}
	if err := validateShadowMCPApprovalRequestClaims(&claims); err != nil {
		return nil, err
	}
	return &claims, nil
}

func validateShadowMCPApprovalRequestClaims(claims *shadowMCPApprovalRequestClaims) error {
	if claims.Issuer != shadowMCPApprovalRequestTokenIssuer {
		return fmt.Errorf("invalid issuer")
	}
	if claims.Subject != shadowMCPApprovalRequestTokenSubject {
		return fmt.Errorf("invalid subject")
	}
	now := time.Now()
	if claims.ExpiresAt == nil {
		return fmt.Errorf("expiration is required")
	}
	if !now.Before(claims.ExpiresAt.Time) {
		return fmt.Errorf("token is expired")
	}
	if claims.NotBefore != nil && now.Before(claims.NotBefore.Time) {
		return fmt.Errorf("token is not valid yet")
	}
	if strings.TrimSpace(claims.OrganizationID) == "" {
		return fmt.Errorf("organization_id is required")
	}
	if _, err := uuid.Parse(claims.ProjectID); err != nil {
		return fmt.Errorf("invalid project_id: %w", err)
	}
	return validateShadowMCPEvidence(claims.ObservedFullURL, claims.ObservedURLHost, claims.ObservedServerIdentity)
}

func encryptShadowMCPApprovalRequestToken(jwtSecret string, plaintext []byte) (string, error) {
	gcm, err := shadowMCPApprovalRequestTokenCipher(jwtSecret)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("read shadow mcp approval request token nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, []byte(shadowMCPApprovalRequestTokenSubject))
	raw := make([]byte, len(nonce)+len(ciphertext))
	copy(raw, nonce)
	copy(raw[len(nonce):], ciphertext)
	return shadowMCPApprovalRequestTokenPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func decryptShadowMCPApprovalRequestToken(jwtSecret string, tokenString string) ([]byte, error) {
	if !strings.HasPrefix(tokenString, shadowMCPApprovalRequestTokenPrefix) {
		return nil, fmt.Errorf("invalid shadow mcp approval request token format")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(tokenString, shadowMCPApprovalRequestTokenPrefix))
	if err != nil {
		return nil, fmt.Errorf("decode shadow mcp approval request token: %w", err)
	}
	gcm, err := shadowMCPApprovalRequestTokenCipher(jwtSecret)
	if err != nil {
		return nil, err
	}
	if len(raw) <= gcm.NonceSize() {
		return nil, fmt.Errorf("invalid shadow mcp approval request token length")
	}
	nonce := raw[:gcm.NonceSize()]
	ciphertext := raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, []byte(shadowMCPApprovalRequestTokenSubject))
	if err != nil {
		return nil, fmt.Errorf("decrypt shadow mcp approval request token: %w", err)
	}
	return plaintext, nil
}

func shadowMCPApprovalRequestTokenCipher(jwtSecret string) (cipher.AEAD, error) {
	key := sha256.Sum256([]byte(shadowMCPApprovalRequestTokenSubject + "\x00" + jwtSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("create shadow mcp approval request token cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create shadow mcp approval request token gcm: %w", err)
	}
	return gcm, nil
}

func shadowMCPApprovalRequestFingerprint(claims *shadowMCPApprovalRequestClaims) string {
	parts := []string{
		"full_url=" + optionalStringValue(claims.ObservedFullURL),
		"url_host=" + optionalStringValue(claims.ObservedURLHost),
		"server_identity=" + optionalStringValue(claims.ObservedServerIdentity),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeOptionalLowerString(value *string) *string {
	trimmed := normalizeOptionalString(value)
	if trimmed == nil {
		return nil
	}
	lowered := strings.ToLower(*trimmed)
	return &lowered
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
