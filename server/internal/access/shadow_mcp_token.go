package access

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	shadowMCPApprovalRequestTokenIssuer  = "gram"
	shadowMCPApprovalRequestTokenSubject = "shadow_mcp_approval_request" // #nosec G101 -- JWT subject label, not a credential.
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

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign shadow mcp approval request token: %w", err)
	}
	return signed, expiry, nil
}

func parseShadowMCPApprovalRequestToken(jwtSecret string, tokenString string) (*shadowMCPApprovalRequestClaims, error) {
	if strings.TrimSpace(jwtSecret) == "" {
		return nil, fmt.Errorf("jwt secret is required")
	}
	var claims shadowMCPApprovalRequestClaims
	token, err := jwt.ParseWithClaims(tokenString, &claims, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(jwtSecret), nil
	}, jwt.WithIssuer(shadowMCPApprovalRequestTokenIssuer), jwt.WithSubject(shadowMCPApprovalRequestTokenSubject), jwt.WithExpirationRequired())
	if err != nil {
		return nil, fmt.Errorf("parse shadow mcp approval request token: %w", err)
	}
	if !token.Valid {
		return nil, fmt.Errorf("invalid shadow mcp approval request token")
	}
	if err := validateShadowMCPApprovalRequestClaims(&claims); err != nil {
		return nil, err
	}
	return &claims, nil
}

func validateShadowMCPApprovalRequestClaims(claims *shadowMCPApprovalRequestClaims) error {
	if strings.TrimSpace(claims.OrganizationID) == "" {
		return fmt.Errorf("organization_id is required")
	}
	if _, err := uuid.Parse(claims.ProjectID); err != nil {
		return fmt.Errorf("invalid project_id: %w", err)
	}
	return validateShadowMCPEvidence(claims.ObservedFullURL, claims.ObservedURLHost, claims.ObservedServerIdentity)
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
