package gcpauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/compute/metadata"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/impersonate"
)

// cloudPlatformScope is the broad GCP scope used to mint a token when proving an
// identity resolves.
const cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

// metadataProbeTimeout bounds the metadata-server round trips so a non-GCP host
// (local dev) fails fast to the ADC path instead of hanging on the metadata
// endpoint.
const metadataProbeTimeout = 2 * time.Second

// ErrUnsupportedMode is returned when a credential configures an identity mode
// the resolver does not yet support (Workload Identity Federation). Callers
// should surface it as a reportable outcome rather than an internal error.
var ErrUnsupportedMode = errors.New("credential identity mode not yet supported")

// Resolver resolves a GCP IAM credential into the two things callers need from
// it: who the credential's identity is, and a token source that authenticates
// Google API calls made as that identity.
type Resolver interface {
	// ResolvePrincipal reports the effective principal and proves the identity is
	// usable by minting a token.
	ResolvePrincipal(ctx context.Context, cred Credential) (Principal, error)

	// TokenSource returns a token source that authenticates calls as the
	// credential's identity. The source is lazy: obtaining it does not prove the
	// identity works, so callers that need that guarantee up front should use
	// ResolvePrincipal.
	TokenSource(ctx context.Context, cred Credential) (oauth2.TokenSource, error)
}

// DefaultResolver resolves a GCP credential's effective principal using the
// Google SDK. It is safe to use in every environment: off-GCP with no
// credentials configured it returns an error rather than panicking, so callers
// can report the failure however they need.
type DefaultResolver struct{}

var _ Resolver = (*DefaultResolver)(nil)

// NewResolver returns the production identity resolver.
func NewResolver() *DefaultResolver {
	return &DefaultResolver{}
}

// ResolvePrincipal resolves the effective principal for a GCP credential.
// Workload Identity Federation credentials return ErrUnsupportedMode.
func (r *DefaultResolver) ResolvePrincipal(ctx context.Context, cred Credential) (Principal, error) {
	switch cred.mode() {
	case modeImpersonation:
		return resolveImpersonated(ctx, cred.ImpersonateServiceAccount)
	case modeAmbient:
		return resolveAmbient(ctx)
	case modeWIF:
		return Principal{}, ErrUnsupportedMode
	default:
		return Principal{}, ErrUnsupportedMode
	}
}

// resolveAmbient resolves Gram's own attached identity and proves it is usable.
func resolveAmbient(ctx context.Context) (Principal, error) {
	// Prove the identity is usable by minting a token from Application Default
	// Credentials. ADC reads the metadata server in-cluster and the local
	// credentials off-GCP, so success means the identity can actually obtain a
	// token, not merely that a service account is attached.
	creds, err := ambientCredentials(ctx)
	if err != nil {
		return Principal{}, err
	}
	if _, err := creds.TokenSource.Token(); err != nil {
		return Principal{}, fmt.Errorf("mint token from application default credentials: %w", err)
	}

	// Report the principal. In-cluster the metadata server holds the authoritative
	// attached service-account email; bound that probe so an off-GCP host does not
	// hang on it. The email is best-effort otherwise (user-backed ADC carries no
	// service-account email), and an empty email on a successful token mint is
	// still a usable identity.
	mdCtx, cancel := context.WithTimeout(ctx, metadataProbeTimeout)
	defer cancel()
	if metadata.OnGCEWithContext(mdCtx) {
		if email, err := metadata.EmailWithContext(mdCtx, "default"); err == nil {
			return Principal{Email: strings.TrimSpace(email), Source: SourceMetadataServer}, nil
		}
	}
	return Principal{Email: serviceAccountEmail(creds.JSON), Source: SourceADC}, nil
}

// TokenSource returns a token source authenticating as the credential's
// identity. Workload Identity Federation credentials return ErrUnsupportedMode.
//
// Unlike ResolvePrincipal this does not mint a token, so a credential that
// cannot actually obtain one fails at first use rather than here. That is
// deliberate: the returned source refreshes on its own schedule, so proving it
// once up front would guarantee nothing about later calls.
func (r *DefaultResolver) TokenSource(ctx context.Context, cred Credential) (oauth2.TokenSource, error) {
	switch cred.mode() {
	case modeImpersonation:
		return impersonatedTokenSource(ctx, cred.ImpersonateServiceAccount)
	case modeAmbient:
		creds, err := ambientCredentials(ctx)
		if err != nil {
			return nil, err
		}
		return creds.TokenSource, nil
	case modeWIF:
		return nil, ErrUnsupportedMode
	default:
		return nil, ErrUnsupportedMode
	}
}

// ambientCredentials loads Gram's own Application Default Credentials, which
// read the metadata server in-cluster and local credentials off-GCP.
//
// Both the principal probe and the token source go through it. ResolvePrincipal
// cannot simply call TokenSource, because it also needs the credentials JSON to
// recover a service-account email, so this is the shared point that stops the
// two paths drifting in which credentials they load or how a failure reads.
func ambientCredentials(ctx context.Context) (*google.Credentials, error) {
	creds, err := google.FindDefaultCredentials(ctx, cloudPlatformScope)
	if err != nil {
		return nil, fmt.Errorf("resolve application default credentials: %w", err)
	}

	return creds, nil
}

// resolveImpersonated proves Gram's own identity can impersonate the target
// service account by minting a token as it. Unlike the ambient probe this is a
// real authorization check: it only succeeds when Gram's identity holds
// roles/iam.serviceAccountTokenCreator on the target. The effective principal is
// the target service account itself.
func resolveImpersonated(ctx context.Context, targetServiceAccount string) (Principal, error) {
	tokenSource, err := impersonatedTokenSource(ctx, targetServiceAccount)
	if err != nil {
		return Principal{}, err
	}
	if _, err := tokenSource.Token(); err != nil {
		return Principal{}, fmt.Errorf("impersonate %s: %w", targetServiceAccount, err)
	}
	return Principal{Email: targetServiceAccount, Source: SourceImpersonation}, nil
}

// impersonatedTokenSource builds a token source that mints tokens as the target
// service account, using Gram's own identity (Application Default Credentials)
// as the base. The returned source caches and refreshes tokens internally, so
// callers should retain it rather than rebuilding it per call.
func impersonatedTokenSource(ctx context.Context, targetServiceAccount string) (oauth2.TokenSource, error) {
	tokenSource, err := impersonate.CredentialsTokenSource(ctx, impersonate.CredentialsConfig{
		TargetPrincipal: targetServiceAccount,
		Scopes:          []string{cloudPlatformScope},
		Delegates:       nil,
		Lifetime:        0,
		Subject:         "",
	})
	if err != nil {
		return nil, fmt.Errorf("build impersonated token source for %s: %w", targetServiceAccount, err)
	}
	return tokenSource, nil
}

// serviceAccountEmail extracts the client_email from an ADC credentials JSON
// blob when it is a service-account key. Returns "" for user credentials or any
// blob without the field.
func serviceAccountEmail(credsJSON []byte) string {
	if len(credsJSON) == 0 {
		return ""
	}
	var sa struct {
		ClientEmail string `json:"client_email"`
	}
	if err := json.Unmarshal(credsJSON, &sa); err != nil {
		return ""
	}
	return strings.TrimSpace(sa.ClientEmail)
}
