package triggers

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

const (
	// googleChatRenewalPageSize bounds the number of configurations held in memory.
	googleChatRenewalPageSize = 100

	// googleChatRenewalConcurrency bounds simultaneous provider requests per sweep.
	googleChatRenewalConcurrency = 4
)

// GoogleChatSubscriptionRenewer renews one exact configured space subscription.
// Credentials remain in the activity process and never enter workflow history.
type GoogleChatSubscriptionRenewer func(context.Context, toolconfig.ToolCallEnv, string, string, time.Time) (bool, error)

// RenewGoogleChatSubscriptions scans active, explicitly opted-in triggers. A
// failing subscription does not prevent the rest of the fleet from renewing.
func (a *App) RenewGoogleChatSubscriptions(ctx context.Context, now time.Time, renew GoogleChatSubscriptionRenewer) error {
	if a == nil || a.repo == nil || a.envLoader == nil || renew == nil {
		return fmt.Errorf("google chat subscription renewal is not configured")
	}
	var failures atomic.Int64
	cursor := uuid.Nil
	for {
		rows, err := a.repo.ListAutoRenewGoogleChatTriggers(ctx, triggerrepo.ListAutoRenewGoogleChatTriggersParams{
			AfterID: cursor, PageSize: googleChatRenewalPageSize,
		})
		if err != nil {
			return fmt.Errorf("list renewable Google Chat triggers: %w", err)
		}
		group, groupCtx := errgroup.WithContext(ctx)
		group.SetLimit(googleChatRenewalConcurrency)
		for _, instance := range rows {
			group.Go(func() error {
				renewed, err := a.renewGoogleChatSubscription(groupCtx, instance, now, renew)
				if err != nil {
					failures.Add(1)
					a.logger.WarnContext(groupCtx, "renew Google Chat subscription", attr.SlogTriggerInstanceID(instance.ID.String()), attr.SlogError(err))
				}
				if a.deliveryLogger != nil && (renewed || err != nil) {
					status := DeliveryStatusSent
					if err != nil {
						status = DeliveryStatusFailed
					}
					a.deliveryLogger.LogTriggerDelivery(groupCtx, instance, EventEnvelope{
						EventID: "subscription-renewal:" + now.UTC().Format(time.RFC3339Nano), CorrelationID: "",
						TriggerInstanceID: instance.ID.String(), DefinitionSlug: DefinitionSlugGoogleChat,
						Event: nil, RawPayload: nil, ReceivedAt: now,
					}, status, "Google Chat subscription renewal", err)
				}
				return nil
			})
		}
		if err := group.Wait(); err != nil {
			return fmt.Errorf("renew Google Chat subscription batch: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("renewal sweep cancelled: %w", err)
		}
		if len(rows) < googleChatRenewalPageSize {
			break
		}
		cursor = rows[len(rows)-1].ID
	}
	if failed := failures.Load(); failed > 0 {
		return fmt.Errorf("%d Google Chat subscription renewals failed; check trigger event history", failed)
	}
	return nil
}

func (a *App) renewGoogleChatSubscription(ctx context.Context, instance triggerrepo.TriggerInstance, now time.Time, renew GoogleChatSubscriptionRenewer) (bool, error) {
	raw, err := configJSONToMap(instance.ConfigJson)
	if err != nil {
		return false, fmt.Errorf("decode renewable trigger: %w", err)
	}
	definition, ok := GetDefinition(DefinitionSlugGoogleChat)
	if !ok {
		return false, fmt.Errorf("google chat trigger definition is unavailable")
	}
	config, err := definition.DecodeConfig(raw)
	if err != nil {
		return false, fmt.Errorf("decode Google Chat renewal configuration: %w", err)
	}
	cfg, ok := config.(googlechatTriggerConfig)
	if !ok || cfg.WorkspaceEvents == nil || !cfg.WorkspaceEvents.AutoRenew {
		return false, fmt.Errorf("google chat trigger is not configured for automatic renewal")
	}
	env, err := a.loadEnvironmentMap(ctx, instance.ProjectID, instance.EnvironmentID.UUID)
	if err != nil {
		return false, fmt.Errorf("load Google Chat renewal credentials: %w", err)
	}
	return renew(ctx, toolconfig.ToolCallEnv{SystemEnv: toolconfig.CIEnvFrom(env), UserConfig: nil, OAuthToken: "", GramEmail: "", GramChatID: "", MCPClient: toolconfig.MCPClientIdentity{Name: "", Version: "", OAuthClientID: ""}}, cfg.WorkspaceEvents.WorkspaceSubscription, cfg.SpaceNames[0], now)
}
