package activities

import (
	"context"
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
// orgs without a configured billing alert email are filtered out at the SQL
// layer; chat-BYOK suppression is applied per key type via
// openRouterCreditsAlertConfigs.
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

	// Resolve recipients in one round-trip. The query excludes disabled orgs
	// and orgs without a billing alert email; ineligible orgs keep their
	// reservations, so they are re-checked once per reservation TTL rather
	// than every tick.
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
		a.sendOne(ctx, c, r.OrganizationName, r.AlertEmail.String, now)
	}

	return nil
}

// sendOne dispatches a single threshold alert whose dedup reservation the
// caller already holds. On success the reservation is extended to outlive the
// billing month (plus rollover grace) so the threshold alerts once per month;
// on failure the short reservation is left in place, deferring the retry to
// the next tick after it expires instead of hammering the email provider every
// 5 minutes.
func (a *MaybeSendOpenRouterCreditsAlerts) sendOne(
	ctx context.Context,
	c openRouterCreditsAlertCandidate,
	orgName string,
	recipient string,
	now time.Time,
) {
	logger := a.logger.With(attr.SlogOrganizationID(c.orgID), attr.SlogOpenRouterKeyType(string(c.keyType)))

	tmpl := openRouterCreditsAlertConfigs[c.keyType].template(
		conv.Default(orgName, "your organization"),
		strconv.Itoa(c.threshold),
		c.threshold >= 100,
	)
	if err := a.emails.Send(ctx, recipient, tmpl); err != nil {
		logger.ErrorContext(ctx, "failed to send openrouter credits alert", attr.SlogError(err))
		a.recordFailure(ctx, c.orgID, c.keyType)
		return
	}

	// OpenRouter credits reset at calendar-month boundaries, so the delivered
	// marker must survive until shortly after this month ends and then get out
	// of the way of next month's ladder.
	_, cycleEnd := usage.CurrentBillingCycle(now, 1)
	ttl := cycleEnd.Sub(now) + openRouterCreditsAlertGrace
	if err := a.cache.Expire(ctx, openRouterCreditsAlertKey(c.orgID, c.keyType, c.threshold), ttl); err != nil {
		// Worst case the short reservation lapses and the same threshold
		// re-sends within the month — log so repeats are explicable.
		logger.ErrorContext(ctx, "failed to extend openrouter credits alert reservation", attr.SlogError(err))
	}

	if a.alertsSent != nil {
		a.alertsSent.Add(ctx, 1, metric.WithAttributes(
			attr.OrganizationID(c.orgID),
			attr.OpenRouterKeyType(string(c.keyType)),
		))
	}
	logger.InfoContext(ctx, "sent openrouter credits alert", attr.SlogValueInt(c.threshold))
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
