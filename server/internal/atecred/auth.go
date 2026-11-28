package atecred

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/idtoken"
)

const (
	// googleIssuerHTTPS is the canonical OIDC issuer URL on Google ID tokens.
	googleIssuerHTTPS = "https://accounts.google.com"

	// googleIssuerBare is the alternate issuer string accepted by Google OIDC.
	googleIssuerBare = "accounts.google.com"

	// localEnvironment is the only environment permitted to use a static local
	// bearer token instead of a Google-signed Workload Identity ID token.
	localEnvironment = "local"
)

// ErrAuthRejected marks an unauthenticated or unauthorized call to the
// internal ATE credential mint endpoint.
var ErrAuthRejected = errors.New("ATE credential provider authentication rejected")

// IDTokenValidator verifies Google-signed OIDC ID tokens. *idtoken.Validator
// from google.golang.org/api/idtoken satisfies this interface directly.
type IDTokenValidator interface {
	// Validate verifies rawToken's signature, expiry, and audience claim.
	Validate(ctx context.Context, rawToken, audience string) (*idtoken.Payload, error)
}

// AuthenticatorConfig configures server-side authentication for the internal
// ATE credential mint endpoint.
type AuthenticatorConfig struct {
	// Environment is the deployment environment (local, dev, prod).
	Environment string

	// ExpectedAudience is the exact HTTPS URL of the internal mint endpoint.
	ExpectedAudience string

	// ServiceAccount is the pinned Google Service Account email the credential
	// provider runs as under GKE Workload Identity. Required outside local.
	ServiceAccount string

	// LocalToken is an optional static bearer token permitted only when
	// Environment == "local". Must be empty in all non-local environments.
	LocalToken string

	// Validator verifies Google-signed OIDC ID tokens against Google's JWKS.
	Validator IDTokenValidator
}

// ValidateServerAuthConfig validates server-side mint endpoint authentication
// settings at the composition root.
func ValidateServerAuthConfig(environment, serviceAccount, localToken string) error {
	serviceAccount = strings.TrimSpace(serviceAccount)
	localToken = strings.TrimSpace(localToken)
	if localToken != "" && environment != localEnvironment {
		return errors.New("ATE credential provider local token is only permitted in the local environment")
	}
	if environment != localEnvironment && serviceAccount == "" {
		return errors.New("ATE credential provider service account is required outside local development")
	}
	if environment == localEnvironment && serviceAccount == "" && localToken == "" {
		return errors.New("ATE credential provider requires either a service account or a local token in local development")
	}
	return nil
}

// Authenticator authenticates requests from the ATE credential provider to the
// Speakeasy server's internal mint endpoint.
type Authenticator struct {
	environment      string
	expectedAudience string
	serviceAccount   string
	localToken       string
	validator        IDTokenValidator
}

// NewAuthenticator constructs a server-side mint endpoint authenticator.
// Misconfiguration fails closed on every Authenticate call.
func NewAuthenticator(cfg AuthenticatorConfig) *Authenticator {
	return &Authenticator{
		environment:      strings.TrimSpace(cfg.Environment),
		expectedAudience: strings.TrimSpace(cfg.ExpectedAudience),
		serviceAccount:   strings.TrimSpace(cfg.ServiceAccount),
		localToken:       strings.TrimSpace(cfg.LocalToken),
		validator:        cfg.Validator,
	}
}

// Authenticate validates the Authorization header on a request to the internal
// mint endpoint. Outside local development it requires a Google-signed OIDC ID
// token whose issuer is Google, audience equals ExpectedAudience, email_verified
// is true, and email matches ServiceAccount.
func (a *Authenticator) Authenticate(ctx context.Context, authHeader string) error {
	if a == nil {
		return fmt.Errorf("%w: authenticator is not configured", ErrAuthRejected)
	}
	raw, ok := extractBearer(authHeader)
	if !ok {
		return fmt.Errorf("%w: missing or malformed bearer token", ErrAuthRejected)
	}

	if a.localToken != "" {
		if a.environment != localEnvironment {
			return fmt.Errorf("%w: local token is disabled outside local development", ErrAuthRejected)
		}
		if subtle.ConstantTimeCompare([]byte(raw), []byte(a.localToken)) == 1 {
			return nil
		}
		// In local development, if no Google service account is also pinned,
		// fail closed immediately after a local token mismatch.
		if a.serviceAccount == "" {
			return fmt.Errorf("%w: invalid local bearer token", ErrAuthRejected)
		}
	}

	if a.serviceAccount == "" || a.expectedAudience == "" || a.validator == nil {
		return fmt.Errorf("%w: Google ID token authentication is not configured", ErrAuthRejected)
	}

	payload, err := a.validator.Validate(ctx, raw, a.expectedAudience)
	if err != nil || payload == nil {
		return fmt.Errorf("%w: Google ID token validation failed", ErrAuthRejected)
	}
	if payload.Issuer != googleIssuerHTTPS && payload.Issuer != googleIssuerBare {
		return fmt.Errorf("%w: unexpected ID token issuer", ErrAuthRejected)
	}
	if payload.Audience != a.expectedAudience {
		return fmt.Errorf("%w: unexpected ID token audience", ErrAuthRejected)
	}
	verified, ok := payload.Claims["email_verified"].(bool)
	if !ok || !verified {
		return fmt.Errorf("%w: ID token email is not verified", ErrAuthRejected)
	}
	email, ok := payload.Claims["email"].(string)
	if !ok || email == "" || email != a.serviceAccount {
		return fmt.Errorf("%w: ID token service account does not match", ErrAuthRejected)
	}
	return nil
}

// NewClientTokenSource builds the outbound bearer token source used by
// gram ate-credential-provider when calling the Speakeasy mint endpoint.
// Outside local development it fetches audience-bound Google OIDC ID tokens
// from GKE Workload Identity via google.golang.org/api/idtoken.
func NewClientTokenSource(ctx context.Context, environment, audience, localToken string) (oauth2.TokenSource, error) {
	environment = strings.TrimSpace(environment)
	audience = strings.TrimSpace(audience)
	localToken = strings.TrimSpace(localToken)

	if audience == "" {
		return nil, errors.New("mint endpoint audience URL is required")
	}
	if localToken != "" {
		if environment != localEnvironment {
			return nil, errors.New("local auth token is only permitted in the local environment")
		}
		return oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken:  localToken,
			TokenType:    "Bearer",
			RefreshToken: "",
			Expiry:       time.Time{},
			ExpiresIn:    0,
		}), nil
	}

	ts, err := idtoken.NewTokenSource(ctx, audience)
	if err != nil {
		return nil, fmt.Errorf("create Google ID token source: %w", err)
	}
	return oauth2.ReuseTokenSource(nil, ts), nil
}

func extractBearer(header string) (string, bool) {
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		return "", false
	}
	return token, true
}
