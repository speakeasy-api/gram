package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/googlechatconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const googleChatRenewalPageSize = 100

// Runtime resolves connection secrets only after a server-selected chat and space match.
type Runtime struct {
	RefreshMembers func(context.Context, uuid.UUID, uuid.UUID) error
	DB             *pgxpool.Pool

	Client *Client
}

func (r *Runtime) Token(ctx context.Context, env toolconfig.ToolCallEnv, scope, resource string) (string, bool, error) {
	chat, err := uuid.Parse(env.GramChatID)
	if err != nil {
		return "", false, nil
	}
	parts := strings.Split(resource, "/")
	if len(parts) < 2 || parts[0] != "spaces" {
		return "", false, nil
	}
	space := strings.Join(parts[:2], "/")
	_, err = repo.New(r.DB).ResolveChatBinding(ctx, repo.ResolveChatBindingParams{ChatID: chat, SpaceName: space})
	if err != nil {
		return "", false, nil
	}
	if scope == "https://www.googleapis.com/auth/chat.bot" {
		token, err := r.Client.AppToken(ctx, false)
		return token, true, err
	}
	token, err := r.Client.AppMessageToken(ctx)
	return token, true, err
}
func (r *Runtime) Validate(ctx context.Context, project uuid.UUID, binding string) error {
	id, err := uuid.Parse(binding)
	if err != nil {
		return errors.New("invalid Google binding")
	}
	_, err = repo.New(r.DB).GetManagedBinding(ctx, repo.GetManagedBindingParams{ID: id, ProjectID: project})
	if err != nil {
		return errors.New("google space connection is inactive")
	}
	return nil
}
func (r *Runtime) Renew(ctx context.Context, now time.Time) error {
	cursor := uuid.Nil
	failures := 0
	for {
		rows, err := repo.New(r.DB).ListRenewableBindings(ctx, repo.ListRenewableBindingsParams{AfterID: cursor, PageSize: googleChatRenewalPageSize})
		if err != nil {
			return fmt.Errorf("list managed Google subscriptions: %w", err)
		}
		for _, b := range rows {
			err := r.renew(ctx, b, now)
			code := ""
			if err != nil {
				failures++
				code = "renewal_failed"
			}
			if err := repo.New(r.DB).UpdateBindingHealth(ctx, repo.UpdateBindingHealthParams{OrganizationID: b.OrganizationID, ProjectID: b.ProjectID, ID: b.ID, LastError: conv.ToPGTextEmpty(code)}); err != nil {
				failures++
			}
		}
		if len(rows) < googleChatRenewalPageSize {
			break
		}
		cursor = rows[len(rows)-1].ID
	}
	if failures > 0 {
		return fmt.Errorf("%d managed Google subscriptions require attention", failures)
	}
	return nil
}
func (r *Runtime) renew(ctx context.Context, b repo.GoogleChatBinding, now time.Time) error {
	if !b.SubscriptionName.Valid {
		return errors.New("google subscription is missing")
	}
	token, err := r.Client.AppMessageToken(ctx)
	if err != nil {
		return err
	}
	sub, err := r.Client.GetSubscription(ctx, token, b.SubscriptionName.String)
	if err != nil {
		return fmt.Errorf("read managed Google subscription: %w", err)
	}
	if err := r.Client.ValidateSubscription(sub, b.SpaceName); err != nil {
		return err
	}
	if sub.ExpireTime.After(now.Add(2 * time.Hour)) {
		return nil
	}
	sub, err = r.Client.RenewSubscription(ctx, token, sub)
	if err != nil {
		return fmt.Errorf("renew managed Google subscription: %w", err)
	}
	if err := r.Client.ValidateSubscription(sub, b.SpaceName); err != nil {
		return err
	}
	if !sub.ExpireTime.After(now.Add(2 * time.Hour)) {
		return errors.New("google subscription expiry did not advance")
	}
	return nil
}

// Admit checks the current mapping before a Chat command enters the assistant.
func (r *Runtime) Admit(ctx context.Context, project uuid.UUID, sender triggers.GoogleChatSender) (bool, error) {
	id, err := uuid.Parse(sender.Binding)
	if err != nil {
		return false, errors.New("invalid Google binding")
	}
	b, err := repo.New(r.DB).GetManagedBinding(ctx, repo.GetManagedBindingParams{ID: id, ProjectID: project})
	if err != nil || b.SpaceName != sender.Space {
		return false, errors.New("inactive Google space")
	}
	_, err = repo.New(r.DB).ResolveMappedUser(ctx, repo.ResolveMappedUserParams{OrganizationID: b.OrganizationID, ProjectID: project, BindingID: b.ID, AssistantID: b.AssistantID, GoogleUserName: sender.User})
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("resolve Google participant: %w", err)
	}
	if r.RefreshMembers != nil {
		if err := r.RefreshMembers(ctx, project, id); err != nil {
			return false, fmt.Errorf("refresh Google participants: %w", err)
		}
		_, err = repo.New(r.DB).ResolveMappedUser(ctx, repo.ResolveMappedUserParams{OrganizationID: b.OrganizationID, ProjectID: project, BindingID: b.ID, AssistantID: b.AssistantID, GoogleUserName: sender.User})
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return false, fmt.Errorf("resolve refreshed Google participant: %w", err)
		}
	}
	digest := sha256.Sum256([]byte(sender.Binding + "|" + sender.Thread + "|" + sender.User))
	messageID := "client-map-" + hex.EncodeToString(digest[:16])
	if err := r.Client.NotifyUnmapped(ctx, sender.Space, sender.Thread, messageID); err != nil {
		return false, fmt.Errorf("notify unmapped Google participant: %w", err)
	}
	return false, nil
}

// RecordReply makes replies to a posted triage or digest resume the originating chat.
func (r *Runtime) RecordReply(ctx context.Context, env toolconfig.ToolCallEnv, response []byte) error {
	chatID, err := uuid.Parse(env.GramChatID)
	if err != nil {
		return nil
	}
	var message struct {
		Name   string `json:"name"`
		Thread struct {
			Name string `json:"name"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(response, &message); err != nil {
		return fmt.Errorf("decode posted Google message: %w", err)
	}
	parts := strings.Split(message.Name, "/")
	if len(parts) != 4 || parts[0] != "spaces" {
		return errors.New("posted Google message has no valid space")
	}
	space := strings.Join(parts[:2], "/")
	b, err := repo.New(r.DB).ResolveChatBinding(ctx, repo.ResolveChatBindingParams{ChatID: chatID, SpaceName: space})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolve posted Google binding: %w", err)
	}
	if !b.TriggerID.Valid || !strings.HasPrefix(message.Thread.Name, space+"/threads/") || len(message.Thread.Name) > 200 {
		return errors.New("posted Google message has no valid bound thread")
	}
	err = repo.New(r.DB).RecordPostedThreadRoute(ctx, repo.RecordPostedThreadRouteParams{BindingID: b.ID, ChatID: chatID, ThreadName: message.Thread.Name})
	if err != nil {
		return fmt.Errorf("save posted Google thread route: %w", err)
	}
	return nil
}
