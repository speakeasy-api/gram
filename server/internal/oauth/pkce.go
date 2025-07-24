package oauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"log/slog"
	"regexp"
)

// PKCEService handles PKCE (Proof Key for Code Exchange) operations
type PKCEService struct {
	logger *slog.Logger
}

func NewPKCEService(logger *slog.Logger) *PKCEService {
	return &PKCEService{
		logger: logger,
	}
}

// GenerateCodeChallenge generates a code challenge from a code verifier
func (s *PKCEService) GenerateCodeChallenge(codeVerifier string, method string) (string, error) {
	switch method {
	case "plain":
		// For plain method, code challenge is the same as code verifier
		return codeVerifier, nil
	case "S256":
		// For S256 method, use SHA256 hash
		hash := sha256.Sum256([]byte(codeVerifier))
		return base64.RawURLEncoding.EncodeToString(hash[:]), nil
	default:
		return "", fmt.Errorf("unsupported code challenge method: %s", method)
	}
}

// ValidateCodeVerifier validates that a code verifier is properly formatted
func (s *PKCEService) ValidateCodeVerifier(ctx context.Context, codeVerifier string) error {
	// Check length (43-128 characters)
	if len(codeVerifier) < 43 || len(codeVerifier) > 128 {
		return fmt.Errorf("code verifier must be between 43 and 128 characters")
	}

	// Check format (only unreserved characters: A-Z, a-z, 0-9, -, ., _, ~)
	validPattern := regexp.MustCompile(`^[A-Za-z0-9\-._~]+$`)
	if !validPattern.MatchString(codeVerifier) {
		return fmt.Errorf("code verifier contains invalid characters")
	}

	return nil
}

// ValidateCodeChallenge validates that a code challenge is properly formatted
func (s *PKCEService) ValidateCodeChallenge(ctx context.Context, codeChallenge string, method string) error {
	// Validate method
	if method != "plain" && method != "S256" {
		return fmt.Errorf("invalid code challenge method: %s", method)
	}

	// For plain method, validate as code verifier
	if method == "plain" {
		return s.ValidateCodeVerifier(ctx, codeChallenge)
	}

	// For S256 method, validate base64url format and length
	if method == "S256" {
		// Check length (should be 43 characters for base64url encoded SHA256)
		if len(codeChallenge) != 43 {
			return fmt.Errorf("S256 code challenge must be 43 characters")
		}

		// Check format (base64url characters)
		validPattern := regexp.MustCompile(`^[A-Za-z0-9\-_]+$`)
		if !validPattern.MatchString(codeChallenge) {
			return fmt.Errorf("S256 code challenge contains invalid characters")
		}
	}

	return nil
}

// VerifyCodeChallenge verifies that a code verifier matches the code challenge
func (s *PKCEService) VerifyCodeChallenge(ctx context.Context, codeVerifier, codeChallenge, method string) error {
	// Validate the code verifier format
	if err := s.ValidateCodeVerifier(ctx, codeVerifier); err != nil {
		return fmt.Errorf("invalid code verifier: %w", err)
	}

	// Generate the expected code challenge
	expectedChallenge, err := s.GenerateCodeChallenge(codeVerifier, method)
	if err != nil {
		return fmt.Errorf("failed to generate code challenge: %w", err)
	}

	// Compare the challenges (constant time comparison to prevent timing attacks)
	if subtle.ConstantTimeCompare([]byte(expectedChallenge), []byte(codeChallenge)) != 1 {
		s.logger.ErrorContext(ctx, "PKCE verification failed",
			slog.String("method", method),
			slog.String("expected_length", fmt.Sprintf("%d", len(expectedChallenge))),
			slog.String("actual_length", fmt.Sprintf("%d", len(codeChallenge))))
		return fmt.Errorf("PKCE verification failed")
	}

	s.logger.InfoContext(ctx, "PKCE verification successful", slog.String("method", method))
	return nil
}

// ValidatePKCEFlow validates the complete PKCE flow
func (s *PKCEService) ValidatePKCEFlow(ctx context.Context, grant *Grant, codeVerifier string) error {
	// Check if PKCE was used in the authorization request
	if grant.CodeChallenge == "" {
		return fmt.Errorf("PKCE code challenge not found in grant")
	}

	if grant.CodeChallengeMethod == "" {
		return fmt.Errorf("PKCE code challenge method not found in grant")
	}

	// Verify the code verifier against the challenge
	err := s.VerifyCodeChallenge(ctx, codeVerifier, grant.CodeChallenge, grant.CodeChallengeMethod)
	if err != nil {
		return fmt.Errorf("PKCE verification failed: %w", err)
	}

	return nil
}
