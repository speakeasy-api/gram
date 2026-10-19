package demoseed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
)

// localSlackTimeout bounds the Slack calls the local fixture makes.
const localSlackTimeout = 15 * time.Second

// LocalSlackOptions connects the local organization to a developer's own Slack
// test app, so tool approval requests reach them as DMs. `mise run zero:slack`
// stores the values in the git-ignored mise.local.toml.
type LocalSlackOptions struct {
	// BotToken is the test app's bot token. Empty leaves Slack out.
	BotToken string

	// SlackEmail is the developer's Slack account email. Empty uses their
	// Speakeasy email.
	SlackEmail string

	// Encryption stores the token the way the server reads it.
	Encryption *encryption.Client

	// Slack calls the Slack Web API.
	Slack *slackapi.Client
}

// connectLocalSlack connects the workspace the bot token belongs to and maps
// the developer to their Slack account there. Slack being unreachable or the
// developer not being found only warns: the rest of the seed stays usable.
func connectLocalSlack(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, organizationID string, dev developer, opts LocalSlackOptions) error {
	if opts.BotToken == "" {
		return nil
	}
	if opts.Encryption == nil || opts.Slack == nil {
		return errors.New("connect local Slack: encryption and a Slack client are required")
	}
	ctx, cancel := context.WithTimeout(ctx, localSlackTimeout)
	defer cancel()

	workspace, err := inspectBotToken(ctx, opts.Slack, opts.BotToken)
	if err != nil {
		logger.WarnContext(ctx, "local Slack app not connected; check GRAM_DEV_SLACK_BOT_TOKEN", attr.SlogError(err))
		return nil
	}
	plaintext, err := json.Marshal(slackdirectoryconnections.TokenBundle{Version: 1, AccessToken: opts.BotToken, RefreshToken: "", ExpiresAt: nil, TokenType: "bot"})
	if err != nil {
		return fmt.Errorf("encode local Slack token: %w", err)
	}
	ciphertext, err := opts.Encryption.Encrypt(plaintext)
	if err != nil {
		return fmt.Errorf("encrypt local Slack token: %w", err)
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin local Slack connection: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := slackrepo.New(tx)
	existing, err := queries.LockSlackDirectoryConnectionByTeam(ctx, slackrepo.LockSlackDirectoryConnectionByTeamParams{OrganizationID: organizationID, SlackTeamID: workspace.teamID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		_, err = queries.CreateSlackDirectoryConnection(ctx, slackrepo.CreateSlackDirectoryConnectionParams{OrganizationID: organizationID, SlackTeamID: workspace.teamID, SlackTeamName: conv.ToPGTextEmpty(workspace.teamName), CredentialsEncrypted: conv.ToPGText(ciphertext), GrantedScopes: workspace.scopes, Generation: uuid.New()})
	case err == nil:
		_, err = queries.AuthorizeSlackDirectoryConnection(ctx, slackrepo.AuthorizeSlackDirectoryConnectionParams{OrganizationID: organizationID, ID: existing.ID, SlackTeamID: workspace.teamID, SlackTeamName: conv.ToPGTextEmpty(workspace.teamName), CredentialsEncrypted: conv.ToPGText(ciphertext), GrantedScopes: workspace.scopes, Generation: uuid.New(), ExpectedGeneration: existing.Generation})
	}
	if err != nil {
		return fmt.Errorf("save local Slack connection: %w", err)
	}

	email := conv.Default(opts.SlackEmail, dev.Email)
	slackUserID, err := lookupSlackUser(ctx, opts.Slack, opts.BotToken, email)
	if err != nil {
		logger.WarnContext(ctx, "local Slack account not found; set GRAM_DEV_SLACK_EMAIL or map yourself on the Slack workspaces page", attr.SlogError(err))
	}
	if slackUserID != "" {
		if err := mapLocalSlackUser(ctx, queries, organizationID, workspace.teamID, slackUserID, email, dev); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit local Slack connection: %w", err)
	}
	logger.InfoContext(ctx, "local Slack workspace connected", attr.SlogSlackTeamID(workspace.teamID))
	return nil
}

// mapLocalSlackUser records the developer's Slack account and confirms it as
// theirs, unless it is already mapped.
func mapLocalSlackUser(ctx context.Context, queries *slackrepo.Queries, organizationID, teamID, slackUserID, email string, dev developer) error {
	if err := queries.UpsertSlackDirectoryMembershipBatch(ctx, slackrepo.UpsertSlackDirectoryMembershipBatchParams{
		OrganizationID:     organizationID,
		SlackTeamID:        teamID,
		LastSeenAt:         conv.ToPGTimestamptz(time.Now()),
		UserIds:            []string{slackUserID},
		DisplayNames:       []string{dev.Name},
		Emails:             []string{email},
		Statuses:           []string{"active"},
		MemberTypes:        []string{"person"},
		ProviderUpdatedAts: []pgtype.Timestamptz{{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}},
	}); err != nil {
		return fmt.Errorf("record local Slack member: %w", err)
	}
	_, err := queries.ResolveSlackMappingUser(ctx, slackrepo.ResolveSlackMappingUserParams{OrganizationID: organizationID, SlackTeamID: teamID, SlackUserID: slackUserID})
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read local Slack mapping: %w", err)
	}
	if err := queries.ConfirmSlackIdentityMapping(ctx, slackrepo.ConfirmSlackIdentityMappingParams{OrganizationID: organizationID, SlackTeamID: teamID, SlackUserID: slackUserID, UserID: dev.ID}); err != nil {
		return fmt.Errorf("map local Slack member: %w", err)
	}
	return nil
}

// botWorkspace is what auth.test says about a bot token.
type botWorkspace struct {
	teamID   string
	teamName string
	scopes   []string
}

// inspectBotToken asks Slack which workspace a bot token belongs to and which
// scopes it carries. Slack reports scopes in a response header only.
func inspectBotToken(ctx context.Context, slack *slackapi.Client, token string) (botWorkspace, error) {
	none := botWorkspace{teamID: "", teamName: "", scopes: nil}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, slack.BaseURL()+"/auth.test", nil)
	if err != nil {
		return none, fmt.Errorf("build auth.test request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := slack.HTTPClient().Do(req)
	if err != nil {
		return none, fmt.Errorf("call auth.test: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
	var body struct {
		OK     bool   `json:"ok"`
		Error  string `json:"error"`
		TeamID string `json:"team_id"`
		Team   string `json:"team"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return none, fmt.Errorf("decode auth.test: %w", err)
	}
	if !body.OK || body.TeamID == "" {
		return none, fmt.Errorf("auth.test refused the token: %s", body.Error)
	}
	scopes := []string{}
	for scope := range strings.SplitSeq(resp.Header.Get("X-Oauth-Scopes"), ",") {
		if scope = strings.TrimSpace(scope); scope != "" {
			scopes = append(scopes, scope)
		}
	}
	return botWorkspace{teamID: body.TeamID, teamName: body.Team, scopes: scopes}, nil
}

// lookupSlackUser finds a Slack user by email.
func lookupSlackUser(ctx context.Context, slack *slackapi.Client, token, email string) (string, error) {
	body, err := slack.CallWithToken(ctx, "users.lookupByEmail", map[string]any{"email": email}, token)
	if err != nil {
		return "", fmt.Errorf("look up Slack user: %w", err)
	}
	var found struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(body, &found); err != nil {
		return "", fmt.Errorf("decode Slack user: %w", err)
	}
	return found.User.ID, nil
}
