package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	repo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/modelkeys"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/usage"
)

const (
	meterOpenRouterCreditsAlertsSent   = "gram.openrouter.credits_alerts_sent"
	meterOpenRouterCreditsAlertsFailed = "gram.openrouter.credits_alerts_failed"

	// openRouterCreditsAlertRetryTTL is the reservation length while a
	// threshold's email has not been delivered yet. It is taken before any
	// recipient resolution, so it doubles as a retry backoff (a failed or
	// skipped send is reattempted only after it expires, not every 5-minute
	// tick) and as a crash bound (a worker killed between reserving and
	// sending delays the alert by at most this long instead of suppressing it
	// for the rest of the month).
	openRouterCreditsAlertRetryTTL = time.Hour
	// The pre-send recipient claim must expire before the org-level retry
	// reservation. If a worker crashes after claiming but before sending, the
	// next org retry must see the recipient as eligible instead of treating the
	// unfinished claim as a completed delivery.
	openRouterCreditsAlertRecipientRetryTTL = openRouterCreditsAlertRetryTTL / 2

	// openRouterCreditsAlertGrace extends a delivered threshold's reservation
	// past the calendar-month boundary. OpenRouter resets monthly usage at
	// month start but the first post-rollover polls can still report the old
	// month's usage; without the grace the fresh month would immediately
	// re-send the same threshold.
	openRouterCreditsAlertGrace = 48 * time.Hour
)

// highestCrossedOpenRouterCreditsThreshold returns the highest warning
// threshold that credit usage has crossed, as a percentage of the monthly cap:
// 50, 75, 90, then 100 (exhausted). Returns 0 while usage sits below the lowest
// threshold. Unlike the TUM alerts there is no beyond-100 continuation — once
// the cap is hit the key is exhausted and one notice is enough.
func highestCrossedOpenRouterCreditsThreshold(used float64, limit int64) int {
	if limit <= 0 {
		return 0
	}
	pct := int64(used / float64(limit) * 100)
	return int(highestCrossedAlertThreshold(pct, false))
}

// openRouterCreditsAlertConfig describes how one OpenRouter key type is
// alerted on. Adding threshold warnings for a new key type means adding an
// entry to openRouterCreditsAlertConfigs (plus a registered email template);
// candidate selection, dedup, backoff, and recipient resolution are all
// key-type agnostic.
type openRouterCreditsAlertConfig struct {
	// template builds the Loops email for a crossed threshold of this key.
	template func(orgName string, thresholdPercent string, exhausted bool) email.Template
	// suppressedByChatBYOK marks key types whose exhaustion stops being
	// customer-facing once the org supplies its own chat-serving model
	// provider key. Internal-style keys keep alerting regardless: their usage
	// is platform-billed by definition, even when judge slots are BYOK.
	suppressedByChatBYOK bool
}

// openRouterCreditsAlertConfigs enumerates the key types that produce
// threshold warning emails.
var openRouterCreditsAlertConfigs = map[openrouter.KeyType]openRouterCreditsAlertConfig{
	openrouter.KeyTypeChat: {
		template: func(orgName string, thresholdPercent string, exhausted bool) email.Template {
			return email.OpenRouterChatCreditsThreshold{
				OrganizationName: orgName,
				ThresholdPercent: thresholdPercent,
				Exhausted:        exhausted,
			}
		},
		suppressedByChatBYOK: true,
	},
	openrouter.KeyTypeInternal: {
		template: func(orgName string, thresholdPercent string, exhausted bool) email.Template {
			return email.OpenRouterInternalCreditsThreshold{
				OrganizationName: orgName,
				ThresholdPercent: thresholdPercent,
				Exhausted:        exhausted,
			}
		},
		suppressedByChatBYOK: false,
	},
}

// MaybeSendOpenRouterCreditsAlerts emails an organization's billing alert
// contact when a platform-managed OpenRouter key crosses a monthly credit
// threshold (50/75/90/100%). It consumes the same per-org metrics the credits
// workflow already collected, so no extra upstream polling happens.
//
// Each key type carries its own email template — the 'chat' key's exhaustion
// 402s the customer's chat surfaces, while the 'internal' key's exhaustion
// pauses platform-side analysis (risk judges, titles, resolutions, memory) —
// and thresholds dedup independently per (org, key type). Disabled orgs and
// enterprise orgs without a configured billing alert email are filtered out
// at the SQL layer; PAYG orgs fall back to their effective administrators.
// Chat-BYOK suppression is applied per key type via openRouterCreditsAlertConfigs.
type MaybeSendOpenRouterCreditsAlerts struct {
	logger       *slog.Logger
	db           *pgxpool.Pool
	repo         *repo.Queries
	cache        cache.Cache
	emails       *email.Service
	alertsSent   metric.Int64Counter
	alertsFailed metric.Int64Counter
}

func NewMaybeSendOpenRouterCreditsAlerts(
	logger *slog.Logger,
	db *pgxpool.Pool,
	cacheAdapter cache.Cache,
	emails *email.Service,
	meterProvider metric.MeterProvider,
) *MaybeSendOpenRouterCreditsAlerts {
	ctx := context.Background()
	componentLogger := logger.With(attr.SlogComponent("openrouter_credits_alerts"))

	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/background/activities/openrouter_credits_alerts")

	sent, err := meter.Int64Counter(
		meterOpenRouterCreditsAlertsSent,
		metric.WithDescription("OpenRouter credit threshold warning emails delivered."),
		metric.WithUnit("{email}"),
	)
	if err != nil {
		componentLogger.ErrorContext(ctx, "create metric",
			attr.SlogMetricName(meterOpenRouterCreditsAlertsSent), attr.SlogError(err))
	}

	failed, err := meter.Int64Counter(
		meterOpenRouterCreditsAlertsFailed,
		metric.WithDescription("OpenRouter credit threshold alert failures (recipient lookup or email send)."),
		metric.WithUnit("{failure}"),
	)
	if err != nil {
		componentLogger.ErrorContext(ctx, "create metric",
			attr.SlogMetricName(meterOpenRouterCreditsAlertsFailed), attr.SlogError(err))
	}

	return &MaybeSendOpenRouterCreditsAlerts{
		logger:       componentLogger,
		db:           db,
		repo:         repo.New(db),
		cache:        cacheAdapter,
		emails:       emails,
		alertsSent:   sent,
		alertsFailed: failed,
	}
}

// openRouterCreditsAlertKey is the Redis dedup key for one org's crossed
// threshold. The calendar month and the credit limit are deliberately NOT part
// of the key: monthly re-arming comes from the reservation's TTL expiring
// shortly after the cycle ends (see openRouterCreditsAlertGrace), and keying on
// the limit would let the limit flapping between the upstream and DB-cached
// values (when ReconcileMonthlyCredits fails) mint duplicate alerts for one
// unchanged usage state. The trade-off: raising the cap mid-month does not
// re-arm already-sent thresholds until the next month.
func openRouterCreditsAlertKey(orgID string, keyType openrouter.KeyType, threshold int) string {
	return fmt.Sprintf("openrouter-credits-alert:%s:%s:%d", orgID, keyType, threshold)
}

// openRouterCreditsAlertCycle keeps provider idempotency on the prior month
// during the same 48-hour rollover grace used by the Redis reservation. A
// partial-audience retry crossing midnight on the first cannot mint new keys.
func openRouterCreditsAlertCycle(now time.Time) string {
	return now.Add(-openRouterCreditsAlertGrace).Format("2006-01")
}

func openRouterCreditsAlertRecipientKey(idempotencyKey string) string {
	return "openrouter-credits-alert-recipient:" + idempotencyKey
}

// openRouterCreditsAlertCandidate is one (org, key type) pair that crossed a
// threshold this tick and holds a fresh dedup reservation.
type openRouterCreditsAlertCandidate struct {
	orgID     string
	keyType   openrouter.KeyType
	threshold int
}

func (a *MaybeSendOpenRouterCreditsAlerts) Do(ctx context.Context, metrics []OpenRouterCreditsMetric) error {
	// Collapse the tick's metrics down to the (org, key type) pairs that have
	// crossed a threshold on an alertable key. Everything below the lowest
	// threshold, every unconfigured key type, and every unprovisioned
	// (zero-limit) key drops out here.
	candidates := make([]openRouterCreditsAlertCandidate, 0, len(metrics))
	for _, m := range metrics {
		keyType := openrouter.KeyType(m.KeyType)
		if _, ok := openRouterCreditsAlertConfigs[keyType]; !ok {
			continue
		}
		if threshold := highestCrossedOpenRouterCreditsThreshold(m.CreditsUsed, m.CreditLimit); threshold != 0 {
			candidates = append(candidates, openRouterCreditsAlertCandidate{
				orgID:     m.OrganizationID,
				keyType:   keyType,
				threshold: threshold,
			})
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	// Reserve before any DB work. At steady state — every crossed threshold
	// already alerted or backing off — all candidates drop out on this cheap
	// Redis check and the tick does no recipient lookups at all. The short
	// reservation is extended to month length only after a successful send.
	pending := make([]openRouterCreditsAlertCandidate, 0, len(candidates))
	for _, c := range candidates {
		won, err := a.cache.Add(ctx, openRouterCreditsAlertKey(c.orgID, c.keyType, c.threshold), openRouterCreditsAlertRetryTTL)
		if err != nil {
			a.logger.ErrorContext(ctx, "failed to reserve openrouter credits alert",
				attr.SlogOrganizationID(c.orgID), attr.SlogOpenRouterKeyType(string(c.keyType)), attr.SlogError(err))
			continue
		}
		if won {
			pending = append(pending, c)
		}
	}
	if len(pending) == 0 {
		return nil
	}

	// Resolve billing routing metadata in one round-trip. Ineligible orgs keep
	// their reservations, so they are re-checked once per reservation TTL
	// rather than every tick.
	orgIDSet := make(map[string]struct{}, len(pending))
	orgIDs := make([]string, 0, len(pending))
	for _, c := range pending {
		if _, ok := orgIDSet[c.orgID]; !ok {
			orgIDSet[c.orgID] = struct{}{}
			orgIDs = append(orgIDs, c.orgID)
		}
	}
	recipients, err := a.repo.GetOpenRouterCreditsAlertRecipients(ctx, repo.GetOpenRouterCreditsAlertRecipientsParams{
		OrganizationIds:   orgIDs,
		InternalOnlySlots: modelkeys.InternalOnlySlots(),
	})
	if err != nil {
		a.recordFailure(ctx, "", "")
		return fmt.Errorf("get openrouter credits alert recipients: %w", err)
	}

	now := time.Now().UTC()
	eligible := make(map[string]repo.GetOpenRouterCreditsAlertRecipientsRow, len(recipients))
	for _, r := range recipients {
		eligible[r.OrganizationID] = r
	}
	type resolvedAudience struct {
		recipients []string
		err        error
	}
	audiences := make(map[string]resolvedAudience, len(eligible))
	for _, c := range pending {
		logger := a.logger.With(
			attr.SlogOrganizationID(c.orgID),
			attr.SlogOpenRouterKeyType(string(c.keyType)),
			attr.SlogValueInt(c.threshold),
		)
		r, ok := eligible[c.orgID]
		if !ok {
			// Naturally rate-limited to once per reservation TTL.
			logger.InfoContext(ctx, "skipping openrouter credits alert without eligible recipient")
			continue
		}
		if r.ChatByok && openRouterCreditsAlertConfigs[c.keyType].suppressedByChatBYOK {
			logger.InfoContext(ctx, "skipping openrouter credits alert for chat-BYOK org")
			continue
		}

		audience, resolved := audiences[c.orgID]
		if !resolved {
			configuredEmail := conv.FromPGText[string](r.AlertEmail)
			audience.recipients, audience.err = resolveBillingNotificationRecipients(ctx, a.db, c.orgID, r.GramAccountType, configuredEmail)
			audiences[c.orgID] = audience
		}
		if len(audience.recipients) == 0 {
			if audience.err != nil {
				logger.ErrorContext(ctx, "failed to resolve openrouter credits alert recipients", attr.SlogError(audience.err))
				a.recordFailure(ctx, c.orgID, c.keyType)
			} else {
				logger.InfoContext(ctx, "skipping openrouter credits alert without eligible recipient")
			}
			continue
		}

		deliveryErrors := []error{audience.err}
		for _, recipient := range audience.recipients {
			if err := a.sendOne(ctx, c, r.OrganizationName, recipient, now); err != nil {
				deliveryErrors = append(deliveryErrors, err)
			}
		}
		if err := errors.Join(deliveryErrors...); err != nil {
			logger.ErrorContext(ctx, "failed to deliver openrouter credits alert audience", attr.SlogError(err))
			a.recordFailure(ctx, c.orgID, c.keyType)
			continue
		}

		// Finalize the org-level reservation only after the full audience has
		// accepted the send. Provider idempotency keys make a partial retry safe.
		_, cycleEnd := usage.CurrentBillingCycle(now, 1)
		ttl := cycleEnd.Sub(now) + openRouterCreditsAlertGrace
		expireCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		if err := a.cache.Expire(expireCtx, openRouterCreditsAlertKey(c.orgID, c.keyType, c.threshold), ttl); err != nil {
			logger.ErrorContext(expireCtx, "failed to extend openrouter credits alert reservation", attr.SlogError(err))
		}
		cancel()
	}

	return nil
}

// sendOne dispatches one recipient's threshold alert. The caller holds the
// org-level reservation and only finalizes it after every recipient succeeds.
// Its recipient-specific provider key makes partial audience retries safe.
func (a *MaybeSendOpenRouterCreditsAlerts) sendOne(
	ctx context.Context,
	c openRouterCreditsAlertCandidate,
	orgName string,
	recipient string,
	now time.Time,
) error {
	logger := a.logger.With(attr.SlogOrganizationID(c.orgID), attr.SlogOpenRouterKeyType(string(c.keyType)))

	tmpl := openRouterCreditsAlertConfigs[c.keyType].template(
		conv.Default(orgName, "your organization"),
		strconv.Itoa(c.threshold),
		c.threshold >= 100,
	)
	idempotencyKey := recipientEmailIdempotencyKey(recipient, "openrouter-credits-alert", c.orgID, string(c.keyType), strconv.Itoa(c.threshold), openRouterCreditsAlertCycle(now))
	recipientKey := openRouterCreditsAlertRecipientKey(idempotencyKey)
	won, err := a.cache.Add(ctx, recipientKey, openRouterCreditsAlertRecipientRetryTTL)
	if err != nil {
		return fmt.Errorf("reserve openrouter credits alert recipient: %w", err)
	}
	if !won {
		return nil
	}

	if err := a.emails.SendIdempotent(ctx, recipient, idempotencyKey, tmpl); err != nil {
		sendErr := fmt.Errorf("send openrouter credits alert: %w", err)
		if deleteErr := a.cache.Delete(ctx, recipientKey); deleteErr != nil {
			return errors.Join(sendErr, fmt.Errorf("release openrouter credits alert recipient: %w", deleteErr))
		}
		return sendErr
	}

	_, cycleEnd := usage.CurrentBillingCycle(now, 1)
	if err := a.cache.Expire(ctx, recipientKey, cycleEnd.Sub(now)+openRouterCreditsAlertGrace); err != nil {
		return fmt.Errorf("persist openrouter credits alert recipient delivery: %w", err)
	}

	if a.alertsSent != nil {
		a.alertsSent.Add(ctx, 1, metric.WithAttributes(
			attr.OrganizationID(c.orgID),
			attr.OpenRouterKeyType(string(c.keyType)),
		))
	}
	logger.InfoContext(ctx, "sent openrouter credits alert", attr.SlogValueInt(c.threshold))
	return nil
}

// recordFailure bumps the failure counter that stands in for workflow-level
// failure signal: the credits workflow deliberately swallows alert errors so
// metrics collection never fails on account of alerting, which would otherwise
// leave persistent alert breakage invisible to monitoring.
func (a *MaybeSendOpenRouterCreditsAlerts) recordFailure(ctx context.Context, orgID string, keyType openrouter.KeyType) {
	if a.alertsFailed == nil {
		return
	}
	attrs := []attribute.KeyValue{}
	if orgID != "" {
		attrs = append(attrs, attr.OrganizationID(orgID))
	}
	if keyType != "" {
		attrs = append(attrs, attr.OpenRouterKeyType(string(keyType)))
	}
	a.alertsFailed.Add(ctx, 1, metric.WithAttributes(attrs...))
}
