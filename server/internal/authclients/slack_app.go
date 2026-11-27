package authclients

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// Canonical header names stored on a slack_app typed auth client. Every entry
// is stored encrypted (is_secret = true) and never returned in API responses,
// audit payloads, or logs.
const (
	SlackHeaderBotToken      = "SLACK_BOT_TOKEN"      //nolint:gosec // header name, not a credential
	SlackHeaderUserToken     = "SLACK_USER_TOKEN"     //nolint:gosec // header name, not a credential
	SlackHeaderSigningSecret = "SLACK_SIGNING_SECRET" //nolint:gosec // header name, not a credential
	SlackHeaderToken         = "SLACK_TOKEN"          //nolint:gosec // header name, not a credential
)

var allowedSlackAppHeaderKeys = map[string]string{
	"slack-bot-token":      SlackHeaderBotToken,
	"slack-user-token":     SlackHeaderUserToken,
	"slack-signing-secret": SlackHeaderSigningSecret,
	"slack-token":          SlackHeaderToken,
}

var (
	// ErrNoAuthorizedSlackAppClient reports that the caller holds mcp:auth on
	// zero enabled slack_app auth clients in scope.
	ErrNoAuthorizedSlackAppClient = errors.New("no authorized slack_app auth client")

	// ErrMultipleAuthorizedSlackAppClients reports that the caller holds
	// mcp:auth on more than one enabled slack_app auth client in scope.
	ErrMultipleAuthorizedSlackAppClients = errors.New("multiple authorized slack_app auth clients")

	// ErrSlackAppClientUnavailable reports that a referenced slack_app auth
	// client is missing, soft-deleted, of another type, or outside the tenant.
	ErrSlackAppClientUnavailable = errors.New("slack_app auth client is unavailable")

	// ErrSlackAppClientDisabled reports that a referenced slack_app auth client
	// is disabled.
	ErrSlackAppClientDisabled = errors.New("slack_app auth client is disabled")
)

// SlackAppCredentials holds decrypted credentials from a single slack_app typed
// auth client. Callers must never log, serialize into API responses, or forward
// these values as generic outbound MCP headers.
type SlackAppCredentials struct {
	ClientID      uuid.UUID
	BotToken      string
	UserToken     string
	Token         string
	SigningSecret string
}

// SlackAppCredentialStore resolves and decrypts slack_app typed auth clients
// for the assistant-slack built-in toolset and Slack trigger ingress.
type SlackAppCredentialStore struct {
	db    *pgxpool.Pool
	authz *authz.Engine
	enc   *encryption.Client
}

// NewSlackAppCredentialStore builds a store backed by db, authzEngine, and enc.
func NewSlackAppCredentialStore(db *pgxpool.Pool, authzEngine *authz.Engine, enc *encryption.Client) *SlackAppCredentialStore {
	return &SlackAppCredentialStore{
		db:    db,
		authz: authzEngine,
		enc:   enc,
	}
}

// ResolveForCaller selects the unique enabled slack_app auth client in
// (organizationID, projectID) — including organization-wide clients — that
// passes live mcp:auth for the caller in ctx. Zero or more than one permitted
// client fails closed with a clear precondition error.
func (s *SlackAppCredentialStore) ResolveForCaller(ctx context.Context, organizationID string, projectID uuid.UUID) (SlackAppCredentials, error) {
	if s == nil || s.db == nil || s.authz == nil || s.enc == nil {
		return SlackAppCredentials{}, oops.E(oops.CodeFailedPrecondition, nil, "assistant Slack credentials are not configured")
	}

	candidates, err := repo.New(s.db).ListEnabledSlackAppAuthClientsInScope(ctx, repo.ListEnabledSlackAppAuthClientsInScopeParams{
		OrganizationID: organizationID,
		ProjectID:      uuid.NullUUID{UUID: projectID, Valid: projectID != uuid.Nil},
	})
	if err != nil {
		return SlackAppCredentials{}, fmt.Errorf("list slack_app auth clients: %w", err)
	}

	// Probe candidates without recording authorization challenges so another
	// assistant's auth client in the same project never logs spurious denials.
	authorization, err := s.authz.PrepareMandatory(authz.WithoutChallenges(ctx))
	if err != nil {
		return SlackAppCredentials{}, fmt.Errorf("admit caller for slack_app auth client: %w", err)
	}

	permitted := make([]repo.McpAuthClient, 0, 1)
	for _, client := range candidates {
		check := authz.MCPAuthClientCheck(client.ID.String(), authz.MCPAuthClientDimensions{
			ClientType:      authz.AuthClientTypeSlackApp,
			ProjectID:       projectID.String(),
			ConnectionOwner: "",
			UpstreamClient:  "",
		})
		decision, err := authorization.DecideAuthClientAdmission(organizationID, authz.MandatoryServerPrivate, check)
		if err != nil {
			return SlackAppCredentials{}, fmt.Errorf("evaluate slack_app mcp:auth: %w", err)
		}
		if decision.Allowed {
			permitted = append(permitted, client)
		}
	}

	switch len(permitted) {
	case 0:
		return SlackAppCredentials{}, oops.E(
			oops.CodeFailedPrecondition,
			ErrNoAuthorizedSlackAppClient,
			"no Slack app auth client is authorized for this assistant; link a slack_app auth client and grant this assistant's agent mcp:auth on it",
		)
	case 1:
		return s.LoadCredentials(ctx, organizationID, projectID, permitted[0].ID)
	default:
		return SlackAppCredentials{}, oops.E(
			oops.CodeFailedPrecondition,
			ErrMultipleAuthorizedSlackAppClients,
			"multiple Slack app auth clients (%d) are authorized for this assistant; grant this assistant's agent mcp:auth on exactly one slack_app auth client",
			len(permitted),
		)
	}
}

// ValidateSlackAppAuthClient verifies that authClientID names a live slack_app
// auth client in (organizationID, projectID) or shared across organizationID.
func (s *SlackAppCredentialStore) ValidateSlackAppAuthClient(ctx context.Context, organizationID string, projectID uuid.UUID, authClientID uuid.UUID) error {
	if s == nil || s.db == nil {
		return oops.E(oops.CodeFailedPrecondition, ErrSlackAppClientUnavailable, "slack_app auth client is unavailable")
	}
	client, err := repo.New(s.db).GetSlackAppAuthClientInScope(ctx, repo.GetSlackAppAuthClientInScopeParams{
		ID:             authClientID,
		OrganizationID: organizationID,
		ProjectID:      uuid.NullUUID{UUID: projectID, Valid: projectID != uuid.Nil},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeFailedPrecondition, ErrSlackAppClientUnavailable, "slack_app auth client is unavailable")
	}
	if err != nil {
		return fmt.Errorf("validate slack_app auth client: %w", err)
	}
	if !client.Enabled {
		return oops.E(oops.CodeFailedPrecondition, ErrSlackAppClientDisabled, "slack_app auth client is disabled")
	}
	return nil
}

// LoadCredentials loads and decrypts the secrets of one specific enabled
// slack_app auth client in (organizationID, projectID). Fails closed when the
// client is missing, soft-deleted, disabled, of another type, or outside scope.
func (s *SlackAppCredentialStore) LoadCredentials(ctx context.Context, organizationID string, projectID uuid.UUID, authClientID uuid.UUID) (SlackAppCredentials, error) {
	if s == nil || s.db == nil || s.enc == nil {
		return SlackAppCredentials{}, oops.E(oops.CodeFailedPrecondition, ErrSlackAppClientUnavailable, "slack_app auth client is unavailable")
	}
	q := repo.New(s.db)
	client, err := q.GetSlackAppAuthClientInScope(ctx, repo.GetSlackAppAuthClientInScopeParams{
		ID:             authClientID,
		OrganizationID: organizationID,
		ProjectID:      uuid.NullUUID{UUID: projectID, Valid: projectID != uuid.Nil},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return SlackAppCredentials{}, oops.E(oops.CodeFailedPrecondition, ErrSlackAppClientUnavailable, "slack_app auth client is unavailable")
	}
	if err != nil {
		return SlackAppCredentials{}, fmt.Errorf("get slack_app auth client: %w", err)
	}
	if !client.Enabled {
		return SlackAppCredentials{}, oops.E(oops.CodeFailedPrecondition, ErrSlackAppClientDisabled, "slack_app auth client is disabled")
	}

	rows, err := q.ListSlackAppAuthClientSecrets(ctx, repo.ListSlackAppAuthClientSecretsParams{
		AuthClientID:   client.ID,
		OrganizationID: organizationID,
	})
	if err != nil {
		return SlackAppCredentials{}, fmt.Errorf("list slack_app auth client secrets: %w", err)
	}

	creds := SlackAppCredentials{
		ClientID:      client.ID,
		BotToken:      "",
		UserToken:     "",
		Token:         "",
		SigningSecret: "",
	}
	for _, row := range rows {
		if !row.Value.Valid || row.Value.String == "" {
			continue
		}
		val := row.Value.String
		if row.IsSecret {
			decrypted, err := s.enc.Decrypt(val)
			if err != nil {
				return SlackAppCredentials{}, fmt.Errorf("decrypt slack_app credential %s: %w", row.Name, err)
			}
			val = decrypted
		}
		val = strings.TrimSpace(val)
		switch allowedSlackAppHeaderKeys[headerKey(row.Name)] {
		case SlackHeaderBotToken:
			creds.BotToken = val
		case SlackHeaderUserToken:
			creds.UserToken = val
		case SlackHeaderSigningSecret:
			creds.SigningSecret = val
		case SlackHeaderToken:
			creds.Token = val
		}
	}
	return creds, nil
}

// LoadSlackAppCredentials loads decrypted Slack credentials into an env-var
// keyed map for trigger webhook authentication and Slack bot-token helpers.
func (s *SlackAppCredentialStore) LoadSlackAppCredentials(ctx context.Context, organizationID string, projectID uuid.UUID, authClientID uuid.UUID) (map[string]string, error) {
	creds, err := s.LoadCredentials(ctx, organizationID, projectID, authClientID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, 4)
	if creds.BotToken != "" {
		out[SlackHeaderBotToken] = creds.BotToken
	}
	if creds.UserToken != "" {
		out[SlackHeaderUserToken] = creds.UserToken
	}
	if creds.SigningSecret != "" {
		out[SlackHeaderSigningSecret] = creds.SigningSecret
	}
	if creds.Token != "" {
		out[SlackHeaderToken] = creds.Token
	}
	return out, nil
}
