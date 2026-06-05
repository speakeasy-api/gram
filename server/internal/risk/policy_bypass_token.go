package risk

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	policyBypassRequestTokenIssuer  = "gram"
	policyBypassRequestTokenSubject = "risk_policy_bypass_request" // #nosec G101 -- JWT subject label, not a credential.
	policyBypassRequestTokenPrefix  = "rpbr1."
)

type PolicyBypassRequestTokenInput struct {
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
	RiskPolicyID           string
	RiskResultID           *string
}

type policyBypassRequestClaims struct {
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
	RiskPolicyID           string  `json:"risk_policy_id"`
	RiskResultID           *string `json:"risk_result_id,omitempty"`
	jwt.RegisteredClaims
}

func GeneratePolicyBypassRequestURL(siteURL *url.URL, jwtSecret string, input PolicyBypassRequestTokenInput, ttl time.Duration) (string, time.Time, error) {
	if siteURL == nil {
		return "", time.Time{}, fmt.Errorf("site url is required")
	}
	token, expiry, err := GeneratePolicyBypassRequestToken(jwtSecret, input, ttl)
	if err != nil {
		return "", time.Time{}, err
	}
	requestURL := siteURL.JoinPath("risk-policy-bypass", "request")
	query := url.Values{}
	query.Set("request_token", token)
	requestURL.Fragment = query.Encode()
	return requestURL.String(), expiry, nil
}

func GeneratePolicyBypassRequestToken(jwtSecret string, input PolicyBypassRequestTokenInput, ttl time.Duration) (string, time.Time, error) {
	if strings.TrimSpace(jwtSecret) == "" {
		return "", time.Time{}, fmt.Errorf("jwt secret is required")
	}
	now := time.Now()
	expiry := now.Add(ttl).Truncate(time.Second)
	claims := policyBypassRequestClaims{
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
		RiskPolicyID:           strings.TrimSpace(input.RiskPolicyID),
		RiskResultID:           normalizeOptionalString(input.RiskResultID),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    policyBypassRequestTokenIssuer,
			Subject:   policyBypassRequestTokenSubject,
			Audience:  nil,
			ExpiresAt: jwt.NewNumericDate(expiry),
			NotBefore: jwt.NewNumericDate(now),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        uuid.NewString(),
		},
	}
	if err := validatePolicyBypassRequestClaims(&claims); err != nil {
		return "", time.Time{}, err
	}

	plaintext, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("marshal risk policy bypass request token: %w", err)
	}
	token, err := encryptPolicyBypassRequestToken(jwtSecret, plaintext)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expiry, nil
}

func parsePolicyBypassRequestToken(jwtSecret string, tokenString string) (*policyBypassRequestClaims, error) {
	if strings.TrimSpace(jwtSecret) == "" {
		return nil, fmt.Errorf("jwt secret is required")
	}
	plaintext, err := decryptPolicyBypassRequestToken(jwtSecret, tokenString)
	if err != nil {
		return nil, err
	}
	var claims policyBypassRequestClaims
	if err := json.Unmarshal(plaintext, &claims); err != nil {
		return nil, fmt.Errorf("unmarshal risk policy bypass request token: %w", err)
	}
	if err := validatePolicyBypassRequestClaims(&claims); err != nil {
		return nil, err
	}
	return &claims, nil
}

func validatePolicyBypassRequestClaims(claims *policyBypassRequestClaims) error {
	if claims.Issuer != policyBypassRequestTokenIssuer {
		return fmt.Errorf("invalid issuer")
	}
	if claims.Subject != policyBypassRequestTokenSubject {
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
	if _, err := uuid.Parse(claims.RiskPolicyID); err != nil {
		return fmt.Errorf("invalid risk_policy_id: %w", err)
	}
	return validatePolicyBypassEvidence(claims.ObservedFullURL, claims.ObservedURLHost, claims.ObservedServerIdentity)
}

func validatePolicyBypassEvidence(fullURL, _, _ *string) error {
	if optionalStringValue(fullURL) == "" {
		return fmt.Errorf("observed_full_url is required")
	}
	return nil
}

func encryptPolicyBypassRequestToken(jwtSecret string, plaintext []byte) (string, error) {
	gcm, err := policyBypassRequestTokenCipher(jwtSecret)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("read risk policy bypass request token nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, []byte(policyBypassRequestTokenSubject))
	raw := make([]byte, len(nonce)+len(ciphertext))
	copy(raw, nonce)
	copy(raw[len(nonce):], ciphertext)
	return policyBypassRequestTokenPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func decryptPolicyBypassRequestToken(jwtSecret string, tokenString string) ([]byte, error) {
	if !strings.HasPrefix(tokenString, policyBypassRequestTokenPrefix) {
		return nil, fmt.Errorf("invalid risk policy bypass request token format")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(tokenString, policyBypassRequestTokenPrefix))
	if err != nil {
		return nil, fmt.Errorf("decode risk policy bypass request token: %w", err)
	}
	gcm, err := policyBypassRequestTokenCipher(jwtSecret)
	if err != nil {
		return nil, err
	}
	if len(raw) <= gcm.NonceSize() {
		return nil, fmt.Errorf("invalid risk policy bypass request token length")
	}
	nonce := raw[:gcm.NonceSize()]
	ciphertext := raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, []byte(policyBypassRequestTokenSubject))
	if err != nil {
		return nil, fmt.Errorf("decrypt risk policy bypass request token: %w", err)
	}
	return plaintext, nil
}

func policyBypassRequestTokenCipher(jwtSecret string) (cipher.AEAD, error) {
	key := sha256.Sum256([]byte(policyBypassRequestTokenSubject + "\x00" + jwtSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("create risk policy bypass request token cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create risk policy bypass request token gcm: %w", err)
	}
	return gcm, nil
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
