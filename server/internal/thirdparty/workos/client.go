package workos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/workos/workos-go/v6/pkg/organizations"
	"github.com/workos/workos-go/v6/pkg/usermanagement"
	"github.com/workos/workos-go/v6/pkg/workos_errors"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const workosBaseURL = "https://api.workos.com"

// APIError is returned by do when the WorkOS API responds with a 4xx or 5xx status.
// Callers can use errors.As to inspect the StatusCode for specific handling (e.g. 409 conflict).
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("workos api %s %s: status %d: %s", e.Method, e.Path, e.StatusCode, e.Body)
}

// IsNotFound reports whether err is an APIError with a 404 status code.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// wrapSDKError translates WorkOS SDK errors into APIError so that all WorkOS
// errors surface through a single type. Non-HTTP errors are wrapped normally.
func wrapSDKError(err error, context string) error {
	var httpErr workos_errors.HTTPError
	if errors.As(err, &httpErr) {
		return &APIError{Method: "", Path: "", StatusCode: httpErr.Code, Body: httpErr.Message}
	}
	return fmt.Errorf("%s: %w", context, err)
}

// Client wraps WorkOS API calls for role and membership management.
// It is designed to have a caching layer added later.
type Client struct {
	apiKey     string
	endpoint   string // base URL for raw HTTP calls; defaults to workosBaseURL
	httpClient *guardian.HTTPClient
	orgs       *organizations.Client
	um         *usermanagement.Client
}

// ClientOpts configures optional overrides for New.
// Zero values use production defaults. Primarily used in tests.
type ClientOpts struct {
	// Endpoint overrides the WorkOS base URL for both raw HTTP and SDK calls.
	Endpoint string
	// HTTPClient overrides the default retryable HTTP client.
	HTTPClient *guardian.HTTPClient
}

func NewClient(guardianPolicy *guardian.Policy, apiKey string, opts ...ClientOpts) *Client {
	var opt ClientOpts
	if len(opts) > 0 {
		opt = opts[0]
	}

	endpoint := workosBaseURL
	if opt.Endpoint != "" {
		endpoint = opt.Endpoint
	}

	httpClient := opt.HTTPClient
	if httpClient == nil {
		retryCfg := guardian.DefaultRetryConfig()
		retryCfg.WaitMax = 10 * time.Second
		httpClient = guardianPolicy.PooledClient(guardian.WithRetryConfig(retryCfg))
	}

	um := usermanagement.NewClient(apiKey)
	um.HTTPClient = httpClient
	if opt.Endpoint != "" {
		um.Endpoint = opt.Endpoint
	}

	return &Client{
		apiKey:     apiKey,
		endpoint:   endpoint,
		httpClient: httpClient,
		orgs:       &organizations.Client{APIKey: apiKey, HTTPClient: httpClient, Endpoint: opt.Endpoint, JSONEncode: nil},
		um:         um,
	}
}

// do performs a raw HTTP request against the WorkOS REST API.
// Pass a non-nil out pointer to decode a JSON response body; pass nil to discard it (e.g. DELETE).
func (wc *Client) do(ctx context.Context, method, path string, body []byte, out any) error {
	req, err := wc.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}

	resp, err := wc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(resp.Body)
		return &APIError{Method: method, Path: path, StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}

// newRequest builds an authenticated HTTP request targeting the WorkOS API.
func (wc *Client) newRequest(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	reqURL, err := url.JoinPath(wc.endpoint, path)
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+wc.apiKey)
	req.Header.Set("Content-Type", "application/json")

	return req, nil
}

func convertUser(u usermanagement.User) User {
	return User{ID: u.ID, FirstName: u.FirstName, LastName: u.LastName, Email: u.Email}
}

func convertMember(m usermanagement.OrganizationMembership) Member {
	return Member{
		ID:             m.ID,
		UserID:         m.UserID,
		OrganizationID: m.OrganizationID,
		Organization:   m.OrganizationName,
		RoleSlug:       m.Role.Slug,
		Status:         string(m.Status),
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
	}
}

func convertInvitation(inv usermanagement.Invitation) Invitation {
	return Invitation{
		ID:                  inv.ID,
		Email:               inv.Email,
		State:               InvitationState(inv.State),
		AcceptedAt:          inv.AcceptedAt,
		RevokedAt:           inv.RevokedAt,
		Token:               inv.Token,
		AcceptInvitationURL: inv.AcceptInvitationUrl,
		OrganizationID:      inv.OrganizationID,
		InviterUserID:       inv.InviterUserID,
		ExpiresAt:           inv.ExpiresAt,
		CreatedAt:           inv.CreatedAt,
		UpdatedAt:           inv.UpdatedAt,
	}
}
