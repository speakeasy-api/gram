// Package speakeasyclient is a thin HTTP client for the Speakeasy auth
// provider's IDP wire endpoints (`/v1/speakeasy_provider/{exchange,validate}`)
// plus the post-IDP user-bootstrap side effects every Speakeasy-authenticated
// caller should run.
//
// It exists SPECIFICALLY AND EXCLUSIVELY to support exchanging tokens with
// the Speakeasy auth provider in Gram's auth flows. It is NOT a general-
// purpose IDP integration and should be removed when we replace it with an
// improved direct OIDC connection.
//
// Both `auth/sessions` (chat-session manager) and `mcp/authnchallenge.go`
// (user-session AS path) depend on this client. Centralising the
// bootstrap side effects (UpsertUser, posthog "first-time user" signup
// event, WorkOS membership sync) here means a non-dashboard MCP-only user
// gets the same baseline treatment as a dashboard user — required for
// downstream RBAC to work.
//
// The chat-session manager keeps its own concerns (sessionCache, userInfoCache,
// pylon signing, admin-override resolution, non-free org filtering); the
// user-session path neither needs nor wants those.
package speakeasyclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// Client wraps the IDP wire endpoints and the post-authentication user
// bootstrap. Constructed once at server boot and shared across goroutines.
type Client struct {
	logger        *slog.Logger
	tracer        trace.Tracer
	serverAddress string
	secretKey     string
	httpClient    *guardian.HTTPClient

	// Bootstrap dependencies — invoked when a user authenticates via the IDP.
	db      *pgxpool.Pool
	workos  *workos.Client   // nullable; nil disables WorkOS sync
	posthog *posthog.Posthog // nullable; nil disables signup events
}

// NewClient builds the IDP client. The HTTP client is mediated by the
// supplied Guardian policy with a 10-second timeout — same shape as the
// chat-session manager used to use, so retry / timeout behaviour stays
// consistent across both consumers.
func NewClient(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	guardianPolicy *guardian.Policy,
	serverAddress string,
	secretKey string,
	db *pgxpool.Pool,
	workos *workos.Client,
	posthog *posthog.Posthog,
) *Client {
	httpClient := guardianPolicy.PooledClient()
	httpClient.Timeout = 10 * time.Second

	return &Client{
		logger:        logger.With(attr.SlogComponent("speakeasyclient")),
		tracer:        tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/auth/speakeasyclient"),
		serverAddress: serverAddress,
		secretKey:     secretKey,
		httpClient:    httpClient,
		db:            db,
		workos:        workos,
		posthog:       posthog,
	}
}

// ValidatedToken is the response shape of the IDP `/validate` endpoint, in
// the public form callers consume.
type ValidatedToken struct {
	UserID        string
	Email         string
	DisplayName   string
	PhotoURL      *string
	Admin         bool
	Whitelisted   bool
	Organizations []ValidatedOrganization
}

// ValidatedOrganization is one organization record in the IDP /validate
// response.
type ValidatedOrganization struct {
	ID                 string
	Name               string
	Slug               string
	AccountType        string
	WorkOSID           *string
	UserWorkspaceSlugs []string
}

// internal wire shape — the public ValidatedToken is the stable contract.
type validateResponse struct {
	User struct {
		ID          string  `json:"id"`
		Email       string  `json:"email"`
		DisplayName string  `json:"display_name"`
		PhotoURL    *string `json:"photo_url"`
		Admin       bool    `json:"admin"`
		Whitelisted bool    `json:"whitelisted"`
	} `json:"user"`
	Organizations []struct {
		ID                 string   `json:"id"`
		Name               string   `json:"name"`
		Slug               string   `json:"slug"`
		AccountType        string   `json:"account_type"`
		WorkOSID           *string  `json:"workos_id,omitempty"`   //nolint:tagliatelle // workos_id is the canonical key on the wire.
		UserWorkspaceSlugs []string `json:"user_workspaces_slugs"` //nolint:tagliatelle // user_workspaces_slugs is the canonical key on the wire.
	} `json:"organizations"`
}

// ExchangeCode trades a Speakeasy IDP authorization code for an `id_token`.
// Pure HTTP — no persistence, no telemetry beyond the span.
func (c *Client) ExchangeCode(ctx context.Context, code string) (idToken string, err error) {
	ctx, span := c.tracer.Start(ctx, "speakeasyclient.exchangeCode")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	body, err := json.Marshal(struct {
		Code string `json:"code"`
	}{Code: code})
	if err != nil {
		return "", fmt.Errorf("marshal exchange request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.serverAddress+"/v1/speakeasy_provider/exchange", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create exchange request: %w", err)
	}
	c.authHeaders(req, "")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("perform exchange request: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("exchange failed with status %s", resp.Status)
	}

	var out struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode exchange response: %w", err)
	}
	return out.IDToken, nil
}

// ValidateIDToken verifies an `id_token` against the Speakeasy IDP and
// returns the user + organizations payload. Pure HTTP — does NOT persist
// anything, populate any cache, or trigger any other side effect. Callers
// that want bootstrap side effects should call BootstrapUser separately.
func (c *Client) ValidateIDToken(ctx context.Context, idToken string) (token *ValidatedToken, err error) {
	ctx, span := c.tracer.Start(ctx, "speakeasyclient.validateIDToken")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.serverAddress+"/v1/speakeasy_provider/validate", nil)
	if err != nil {
		return nil, fmt.Errorf("create validate request: %w", err)
	}
	c.authHeaders(req, idToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("perform validate request: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("validate failed with status %s", resp.Status)
	}

	var raw validateResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode validate response: %w", err)
	}

	orgs := make([]ValidatedOrganization, len(raw.Organizations))
	for i, o := range raw.Organizations {
		orgs[i] = ValidatedOrganization{
			ID:                 o.ID,
			Name:               o.Name,
			Slug:               o.Slug,
			AccountType:        o.AccountType,
			WorkOSID:           o.WorkOSID,
			UserWorkspaceSlugs: o.UserWorkspaceSlugs,
		}
	}

	return &ValidatedToken{
		UserID:        raw.User.ID,
		Email:         raw.User.Email,
		DisplayName:   raw.User.DisplayName,
		PhotoURL:      raw.User.PhotoURL,
		Admin:         raw.User.Admin,
		Whitelisted:   raw.User.Whitelisted,
		Organizations: orgs,
	}, nil
}

// BootstrapUser runs the post-IDP user-bootstrap side effects every
// Speakeasy-authenticated caller should run, in order:
//
//  1. UpsertUser — persist a Gram user row keyed on the Speakeasy user id.
//     Required so we have a stable Gram user_id to put on URNs and audit logs.
//  2. Posthog "is_first_time_user_signup" event — only fires when the upsert
//     creates a fresh row. No-ops if posthog is nil.
//  3. WorkOS membership sync — reconciles the user's WorkOS identity and
//     organization-membership records. Best-effort; errors are logged and
//     swallowed (RBAC features degrade gracefully without WorkOS data).
//     No-ops if workos is nil.
//
// Returns the upserted user row so callers don't need to re-query.
func (c *Client) BootstrapUser(ctx context.Context, token *ValidatedToken) (row userRepo.UpsertUserRow, err error) {
	ctx, span := c.tracer.Start(ctx, "speakeasyclient.bootstrapUser")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	row, err = userRepo.New(c.db).UpsertUser(ctx, userRepo.UpsertUserParams{
		ID:          token.UserID,
		Email:       token.Email,
		DisplayName: token.DisplayName,
		PhotoUrl:    conv.PtrToPGText(token.PhotoURL),
		Admin:       token.Admin,
	})
	if err != nil {
		return userRepo.UpsertUserRow{}, fmt.Errorf("upsert user: %w", err)
	}

	if row.WasCreated && c.posthog != nil {
		if err := c.posthog.CaptureEvent(ctx, "is_first_time_user_signup", row.Email, map[string]any{
			"email":        row.Email,
			"display_name": row.DisplayName,
		}); err != nil {
			c.logger.ErrorContext(ctx, "failed to capture is_first_time_user_signup event", attr.SlogError(err))
		}
	}

	if err := c.syncWorkOSMemberships(ctx, row); err != nil {
		// Per legacy contract: WorkOS sync errors are logged inside the helper
		// and don't propagate. We only land here on a logic bug in the helper.
		c.logger.ErrorContext(ctx, "workos membership sync returned error", attr.SlogError(err))
	}

	return row, nil
}

// syncWorkOSMemberships reconciles the user's WorkOS identity and membership
// records against what WorkOS currently reports. Best-effort: failures are
// logged and swallowed because WorkOS data only feeds RBAC features that
// degrade gracefully without it. Mirrors the prior implementation in
// auth/sessions/speakeasyconnections.go.
func (c *Client) syncWorkOSMemberships(ctx context.Context, user userRepo.UpsertUserRow) error {
	if c.workos == nil {
		return nil
	}

	repos := userRepo.New(c.db)
	orgs := orgRepo.New(c.db)

	var workosUserID string

	if user.WorkosID.Valid && user.WorkosID.String != "" {
		workosUserID = user.WorkosID.String
	} else {
		workosUser, err := c.workos.GetUserByEmail(ctx, user.Email)
		if err != nil {
			c.logger.ErrorContext(ctx, "failed to get workos user by email", attr.SlogError(err))
			return nil
		}
		if workosUser == nil {
			return nil
		}

		workosUserID = workosUser.ID

		if err := repos.SetUserWorkosID(ctx, userRepo.SetUserWorkosIDParams{
			ID:       user.ID,
			WorkosID: conv.ToPGText(workosUserID),
		}); err != nil {
			c.logger.ErrorContext(ctx, "failed to set user workos ID", attr.SlogError(err))
		}
	}

	memberships, err := c.workos.ListUserMemberships(ctx, workosUserID)
	if err != nil {
		c.logger.ErrorContext(ctx, "failed to list workos user memberships", attr.SlogError(err))
		return nil
	}

	workosOrgIDs := make([]string, len(memberships))
	membershipIDs := make([]string, len(memberships))
	for i, m := range memberships {
		workosOrgIDs[i] = m.OrganizationID
		membershipIDs[i] = m.ID
	}

	if err := orgs.SetUserWorkOSMemberships(ctx, orgRepo.SetUserWorkOSMembershipsParams{
		UserID:              user.ID,
		WorkosOrgIds:        workosOrgIDs,
		WorkosMembershipIds: membershipIDs,
	}); err != nil {
		c.logger.ErrorContext(ctx, "failed to set user workos memberships", attr.SlogError(err))
	}

	return nil
}

// authHeaders sets the shared header pair every Speakeasy IDP call sends:
// the shared-secret authenticating Gram + (optionally) the user's id token.
func (c *Client) authHeaders(req *http.Request, idToken string) {
	req.Header.Set("speakeasy-auth-provider-key", c.secretKey)
	if idToken != "" {
		req.Header.Set("speakeasy-auth-provider-id-token", idToken)
	}
}
