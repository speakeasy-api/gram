package wellknown

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/guardian"
)

const (
	authorizationServerDiscoveryTimeout      = 10 * time.Second
	authorizationServerDiscoveryMaxBodyBytes = 1 << 20
)

// DiscoveredAuthorizationServerMetadata is the subset of RFC 8414 metadata
// needed to configure an external OAuth authorization-code flow, together with
// the original document for persistence.
type DiscoveredAuthorizationServerMetadata struct {
	RawMetadata                                json.RawMessage `json:"-"`
	Issuer                                     string          `json:"issuer"`
	AuthorizationEndpoint                      string          `json:"authorization_endpoint"`
	TokenEndpoint                              string          `json:"token_endpoint"`
	RegistrationEndpoint                       string          `json:"registration_endpoint,omitempty"`
	AuthorizationResponseIssParameterSupported bool            `json:"authorization_response_iss_parameter_supported,omitempty"`
}

// AuthorizationServerMetadataURL constructs the RFC 8414 well-known URL for
// an issuer while preserving its escaped path.
func AuthorizationServerMetadataURL(issuer string) (string, error) {
	u, err := validateAuthorizationServerIssuerURL(issuer)
	if err != nil {
		return "", err
	}

	return u.Scheme + "://" + u.Host + OAuthAuthorizationServerPath + strings.TrimRight(u.EscapedPath(), "/"), nil
}

// DiscoverAuthorizationServerMetadata validates issuer, fetches its RFC 8414
// metadata through policy, and requires the returned issuer to match exactly.
func DiscoverAuthorizationServerMetadata(ctx context.Context, policy *guardian.Policy, issuer string) (*DiscoveredAuthorizationServerMetadata, error) {
	metadataURL, err := AuthorizationServerMetadataURL(issuer)
	if err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, authorizationServerDiscoveryTimeout)
	defer cancel()

	if _, err := policy.ValidateHTTPSURL(requestCtx, metadataURL); err != nil {
		return nil, fmt.Errorf("validate authorization server metadata URL: %w", err)
	}

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, metadataURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create authorization server metadata request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	client := policy.Client()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		if req.URL.User != nil {
			return fmt.Errorf("authorization server metadata redirect must not contain userinfo")
		}
		if _, err := policy.ValidateHTTPSURL(req.Context(), req.URL.String()); err != nil {
			return fmt.Errorf("validate authorization server metadata redirect: %w", err)
		}
		return nil
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch authorization server metadata: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("fetch authorization server metadata: HTTP %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, authorizationServerDiscoveryMaxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read authorization server metadata: %w", err)
	}
	if len(raw) > authorizationServerDiscoveryMaxBodyBytes {
		return nil, fmt.Errorf("authorization server metadata response body exceeded the %d-byte limit", authorizationServerDiscoveryMaxBodyBytes)
	}

	var metadata DiscoveredAuthorizationServerMetadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return nil, fmt.Errorf("decode authorization server metadata: %w", err)
	}
	if metadata.Issuer != issuer {
		return nil, fmt.Errorf("discovered authorization server issuer %q does not match configured issuer %q", metadata.Issuer, issuer)
	}
	if metadata.AuthorizationEndpoint == "" {
		return nil, fmt.Errorf("authorization server metadata missing authorization_endpoint")
	}
	if metadata.TokenEndpoint == "" {
		return nil, fmt.Errorf("authorization server metadata missing token_endpoint")
	}
	if err := validateAuthorizationServerEndpoint(metadata.AuthorizationEndpoint); err != nil {
		return nil, fmt.Errorf("invalid authorization_endpoint: %w", err)
	}
	if err := validateAuthorizationServerEndpoint(metadata.TokenEndpoint); err != nil {
		return nil, fmt.Errorf("invalid token_endpoint: %w", err)
	}
	if metadata.RegistrationEndpoint != "" {
		if err := validateAuthorizationServerEndpoint(metadata.RegistrationEndpoint); err != nil {
			return nil, fmt.Errorf("invalid registration_endpoint: %w", err)
		}
	}

	metadata.RawMetadata = append(json.RawMessage(nil), raw...)
	return &metadata, nil
}

func validateAuthorizationServerIssuerURL(issuer string) (*url.URL, error) {
	u, err := url.Parse(issuer)
	if err != nil {
		return nil, fmt.Errorf("parse authorization server issuer: %w", err)
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("authorization server issuer must use HTTPS")
	}
	if u.Host == "" {
		return nil, fmt.Errorf("authorization server issuer must be an absolute URL")
	}
	if u.User != nil {
		return nil, fmt.Errorf("authorization server issuer must not contain userinfo")
	}
	if u.ForceQuery || u.RawQuery != "" {
		return nil, fmt.Errorf("authorization server issuer must not contain a query")
	}
	if u.Fragment != "" || strings.HasSuffix(issuer, "#") {
		return nil, fmt.Errorf("authorization server issuer must not contain a fragment")
	}
	return u, nil
}

func validateAuthorizationServerEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("parse URL: %w", err)
	}
	if u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("must be an absolute HTTPS URL")
	}
	if u.User != nil {
		return fmt.Errorf("must not contain userinfo")
	}
	if u.Fragment != "" || strings.HasSuffix(endpoint, "#") {
		return fmt.Errorf("must not contain a fragment")
	}
	return nil
}
