package risk

import (
	"context"
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

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

const (
	policyBypassRequestTokenIssuer  = "gram"
	policyBypassRequestTokenSubject = "risk_policy_bypass_request" // #nosec G101 -- JWT subject label, not a credential.
	// policyBypassRequestTokenPrefix is the legacy (rpbr1) format that
	// encrypts the full request state inline in the URL fragment. Still read
	// so links delivered before the rpbr2 cutover keep working until they
	// expire, but no longer generated.
	policyBypassRequestTokenPrefix = "rpbr1."
	// policyBypassRequestTokenPrefixV2 is the current format: a short opaque
	// id whose state lives in the cache. The id is a 128-bit random bearer
	// secret, so a leaked link still cannot be redeemed by the wrong user —
	// CreateRiskPolicyBypassRequest re-checks org and requester.
	policyBypassRequestTokenPrefixV2 = "rpbr2."
	// policyBypassRequestCacheKeyPrefix namespaces the rpbr2 state in the
	// shared cache. The link generator and the redeem handler must build keys
	// identically, so both go through policyBypassRequestCacheKey.
	policyBypassRequestCacheKeyPrefix = "risk:policy-bypass-request:" // #nosec G101 -- cache key namespace, not a credential.
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

// policyBypassRequestRecord is the server-side state an rpbr2 link points at.
// It holds the same evidence the legacy rpbr1 token encrypted inline, plus a
// stable RequestID so repeated redemptions of one link upsert the same row.
type policyBypassRequestRecord struct {
	RequestID              string    `json:"request_id"`
	OrganizationID         string    `json:"organization_id"`
	ProjectID              string    `json:"project_id"`
	RequesterUserID        string    `json:"requester_user_id,omitempty"`
	ObservedName           *string   `json:"observed_name,omitempty"`
	ObservedFullURL        *string   `json:"observed_full_url,omitempty"`
	ObservedURLHost        *string   `json:"observed_url_host,omitempty"`
	ObservedServerIdentity *string   `json:"observed_server_identity,omitempty"`
	ToolName               *string   `json:"tool_name,omitempty"`
	ToolCall               *string   `json:"tool_call,omitempty"`
	BlockReason            *string   `json:"block_reason,omitempty"`
	RiskPolicyID           string    `json:"risk_policy_id"`
	RiskResultID           *string   `json:"risk_result_id,omitempty"`
	ExpiresAt              time.Time `json:"expires_at"`
}

func policyBypassRequestCacheKey(id string) string {
	return policyBypassRequestCacheKeyPrefix + id
}

// newPolicyBypassRequestID returns a 128-bit random, URL-safe id used both as
// the cache key and as the unguessable bearer secret embedded in the link.
func newPolicyBypassRequestID() (string, error) {
	raw := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", fmt.Errorf("generate risk policy bypass request id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func GeneratePolicyBypassRequestURL(ctx context.Context, c cache.Cache, siteURL *url.URL, input PolicyBypassRequestTokenInput, ttl time.Duration) (string, time.Time, error) {
	if siteURL == nil {
		return "", time.Time{}, fmt.Errorf("site url is required")
	}
	token, expiry, err := GeneratePolicyBypassRequestToken(ctx, c, input, ttl)
	if err != nil {
		return "", time.Time{}, err
	}
	requestURL := siteURL.JoinPath("risk-policy-bypass", "request")
	query := url.Values{}
	query.Set("request_token", token)
	requestURL.Fragment = query.Encode()
	return requestURL.String(), expiry, nil
}

// GeneratePolicyBypassRequestToken stores the request state in the cache and
// returns a short rpbr2 token (the cache id) plus its expiry. The token is
// the only reference to the state — it must be stored under the same cache
// the redeem handler reads from.
func GeneratePolicyBypassRequestToken(ctx context.Context, c cache.Cache, input PolicyBypassRequestTokenInput, ttl time.Duration) (string, time.Time, error) {
	now := time.Now()
	expiry := now.Add(ttl).Truncate(time.Second)
	record := policyBypassRequestRecord{
		RequestID:              uuid.NewString(),
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
		ExpiresAt:              expiry,
	}
	if err := validatePolicyBypassRequestFields(record.OrganizationID, record.ProjectID, record.RiskPolicyID, record.ObservedFullURL, record.ObservedURLHost, record.ObservedServerIdentity); err != nil {
		return "", time.Time{}, err
	}
	if c == nil {
		return "", time.Time{}, fmt.Errorf("risk policy bypass request cache is not configured")
	}

	id, err := newPolicyBypassRequestID()
	if err != nil {
		return "", time.Time{}, err
	}
	if err := c.Set(ctx, policyBypassRequestCacheKey(id), record, ttl); err != nil {
		return "", time.Time{}, fmt.Errorf("store risk policy bypass request: %w", err)
	}
	return policyBypassRequestTokenPrefixV2 + id, expiry, nil
}

func parsePolicyBypassRequestToken(ctx context.Context, c cache.Cache, jwtSecret string, tokenString string) (*policyBypassRequestClaims, error) {
	if strings.HasPrefix(tokenString, policyBypassRequestTokenPrefixV2) {
		return lookupPolicyBypassRequestClaims(ctx, c, tokenString)
	}

	// Legacy rpbr1 path: decrypt the inline claims. Retained only for links
	// already in flight at the rpbr2 cutover; drop in a later contract change.
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

func lookupPolicyBypassRequestClaims(ctx context.Context, c cache.Cache, tokenString string) (*policyBypassRequestClaims, error) {
	if c == nil {
		return nil, fmt.Errorf("risk policy bypass request cache is not configured")
	}
	id := strings.TrimPrefix(tokenString, policyBypassRequestTokenPrefixV2)
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("invalid risk policy bypass request token format")
	}
	var record policyBypassRequestRecord
	if err := c.Get(ctx, policyBypassRequestCacheKey(id), &record); err != nil {
		return nil, fmt.Errorf("risk policy bypass request not found or expired: %w", err)
	}
	claims := &policyBypassRequestClaims{
		OrganizationID:         record.OrganizationID,
		ProjectID:              record.ProjectID,
		RequesterUserID:        record.RequesterUserID,
		ObservedName:           record.ObservedName,
		ObservedFullURL:        record.ObservedFullURL,
		ObservedURLHost:        record.ObservedURLHost,
		ObservedServerIdentity: record.ObservedServerIdentity,
		ToolName:               record.ToolName,
		ToolCall:               record.ToolCall,
		BlockReason:            record.BlockReason,
		RiskPolicyID:           record.RiskPolicyID,
		RiskResultID:           record.RiskResultID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    policyBypassRequestTokenIssuer,
			Subject:   policyBypassRequestTokenSubject,
			Audience:  nil,
			ExpiresAt: jwt.NewNumericDate(record.ExpiresAt),
			NotBefore: nil,
			IssuedAt:  nil,
			ID:        record.RequestID,
		},
	}
	if err := validatePolicyBypassRequestClaims(claims); err != nil {
		return nil, err
	}
	return claims, nil
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
	return validatePolicyBypassRequestFields(claims.OrganizationID, claims.ProjectID, claims.RiskPolicyID, claims.ObservedFullURL, claims.ObservedURLHost, claims.ObservedServerIdentity)
}

func validatePolicyBypassRequestFields(orgID, projectID, policyID string, fullURL, urlHost, serverIdentity *string) error {
	if strings.TrimSpace(orgID) == "" {
		return fmt.Errorf("organization_id is required")
	}
	if _, err := uuid.Parse(projectID); err != nil {
		return fmt.Errorf("invalid project_id: %w", err)
	}
	if _, err := uuid.Parse(policyID); err != nil {
		return fmt.Errorf("invalid risk_policy_id: %w", err)
	}
	return validatePolicyBypassEvidence(fullURL, urlHost, serverIdentity)
}

func validatePolicyBypassEvidence(fullURL, urlHost, serverIdentity *string) error {
	if rawURL := strings.TrimSpace(conv.PtrValOr(fullURL, "")); rawURL != "" {
		parsed, err := url.Parse(rawURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("invalid observed_full_url: must include URI scheme and host")
		}
		return nil
	}
	if strings.TrimSpace(conv.PtrValOr(urlHost, "")) != "" {
		return nil
	}
	if strings.TrimSpace(conv.PtrValOr(serverIdentity, "")) != "" {
		return nil
	}
	return fmt.Errorf("policy bypass request evidence is required")
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
