package activities

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	repo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// openRouterCreditsAlertGrace keeps a threshold's dedup reservation alive past
// the calendar month boundary so a poll firing moments after the month rolls
// over cannot re-alert for a threshold already sent in the closing month.
const openRouterCreditsAlertGrace = 48 * time.Hour

// highestCrossedOpenRouterCreditsThreshold returns the highest warning
// threshold that credit usage has crossed, as a percentage of the monthly cap:
// 50, 75, 90, then 100 (exhausted). Returns 0 while usage sits below the lowest
// threshold. Unlike the TUM alerts there is no beyond-100 continuation — once
// the cap is hit the key is exhausted and one notice is enough.
func highestCrossedOpenRouterCreditsThreshold(used float64, limit int64) int {
	if limit <= 0 {
		return 0
	}
	pct := used / float64(limit) * 100
	switch {
	case pct < 50:
		return 0
	case pct < 75:
		return 50
	case pct < 90:
		return 75
	case pct < 100:
		return 90
	default:
		return 100
	}
}

// MaybeSendOpenRouterCreditsAlerts emails an organization's billing alert
// contact when its platform-managed OpenRouter chat key crosses a monthly
// credit threshold (50/75/90/100%). It consumes the same per-org metrics the
// credits workflow already collected, so no extra upstream polling happens.
//
// Only the 'chat' key is alerted on: exhausting it 402s the customer's chat
// surfaces, which is what the admin can act on, whereas the 'internal' key pays
// for platform-initiated inference the admin has no control over. BYOK orgs and
// orgs without a configured billing alert email are filtered out at the SQL
// layer.
type MaybeSendOpenRouterCreditsAlerts struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
	cache  cache.Cache
	emails *email.Service
}

func NewMaybeSendOpenRouterCreditsAlerts(
	logger *slog.Logger,
	db *pgxpool.Pool,
	cacheAdapter cache.Cache,
	emails *email.Service,
) *MaybeSendOpenRouterCreditsAlerts {
	return &MaybeSendOpenRouterCreditsAlerts{
		logger: logger.With(attr.SlogComponent("openrouter_credits_alerts")),
		db:     db,
		repo:   repo.New(db),
		cache:  cacheAdapter,
		emails: emails,
	}
}

// openRouterCreditsAlertCandidate is an org that has crossed a threshold on its
// chat key this tick and is a candidate for an email, pending recipient
// resolution.
type openRouterCreditsAlertCandidate struct {
	threshold int
	limit     int64
}

func (a *MaybeSendOpenRouterCreditsAlerts) Do(ctx context.Context, metrics []OpenRouterCreditsMetric) error {
	// Collapse the tick's metrics down to the chat-key orgs that have crossed a
	// threshold. Everything below the lowest threshold, every non-chat key, and
	// every unprovisioned (zero-limit) key drops out here.
	candidates := make(map[string]openRouterCreditsAlertCandidate, len(metrics))
	for _, m := range metrics {
		if m.KeyType != string(openrouter.KeyTypeChat) {
			continue
		}
		threshold := highestCrossedOpenRouterCreditsThreshold(m.CreditsUsed, m.CreditLimit)
		if threshold == 0 {
			continue
		}
		candidates[m.OrganizationID] = openRouterCreditsAlertCandidate{
			threshold: threshold,
			limit:     m.CreditLimit,
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	orgIDs := make([]string, 0, len(candidates))
	for orgID := range candidates {
		orgIDs = append(orgIDs, orgID)
	}

	// Resolve recipients in one round-trip. The query already excludes BYOK
	// orgs and orgs without a billing alert email, so anything it returns is a
	// send target.
	recipients, err := a.repo.GetOpenRouterCreditsAlertRecipients(ctx, orgIDs)
	if err != nil {
		return fmt.Errorf("get openrouter credits alert recipients: %w", err)
	}

	now := time.Now().UTC()
	for _, r := range recipients {
		alertEmail := conv.FromPGText[string](r.AlertEmail)
		if alertEmail == nil || *alertEmail == "" {
			continue
		}
		candidate, ok := candidates[r.OrganizationID]
		if !ok {
			continue
		}
		a.sendOne(ctx, r.OrganizationID, r.OrganizationName, *alertEmail, candidate, now)
	}

	return nil
}

// sendOne dispatches a single threshold alert, reserving a Redis dedup key so
// the same threshold is not re-sent every 5 minutes. Sending is best-effort:
// any failure releases the reservation so the next poll retries, and no failure
// is propagated to the workflow.
func (a *MaybeSendOpenRouterCreditsAlerts) sendOne(
	ctx context.Context,
	orgID string,
	orgName string,
	recipient string,
	candidate openRouterCreditsAlertCandidate,
	now time.Time,
) {
	logger := a.logger.With(attr.SlogOrganizationID(orgID))

	// The limit is part of the dedup key so raising the cap re-arms every
	// threshold, and the calendar month re-arms the whole ladder each cycle to
	// track OpenRouter's monthly credit reset. Only the highest crossed
	// threshold is alerted, so usage blowing through several thresholds between
	// runs sends a single email.
	period := now.Format("200601")
	key := fmt.Sprintf("openrouter-credits-alert:%s:%s:%s:%d:%d",
		orgID, openrouter.KeyTypeChat, period, candidate.limit, candidate.threshold)
	ttl := startOfNextMonthUTC(now).Sub(now) + openRouterCreditsAlertGrace

	won, err := a.cache.Add(ctx, key, ttl)
	if err != nil {
		logger.ErrorContext(ctx, "failed to reserve openrouter credits alert", attr.SlogError(err))
		return
	}
	if !won {
		return
	}

	// The reservation is held: release it on any failure so the next poll
	// retries. The release must outlive the failure that triggered it,
	// including context cancellation, or the stale reservation suppresses the
	// alert for the rest of the month.
	release := func(msg string, err error) {
		logger.ErrorContext(ctx, msg, attr.SlogError(err))
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := a.cache.Delete(releaseCtx, key); err != nil {
			logger.ErrorContext(releaseCtx, "failed to release openrouter credits alert reservation", attr.SlogError(err))
		}
	}

	tmpl := email.OpenRouterCreditsThreshold{
		OrganizationName: conv.Default(orgName, "your organization"),
		ThresholdPercent: strconv.Itoa(candidate.threshold),
		Exhausted:        candidate.threshold >= 100,
	}
	if err := a.emails.Send(ctx, recipient, tmpl); err != nil {
		release("failed to send openrouter credits alert", err)
		return
	}

	logger.InfoContext(ctx, "sent openrouter credits alert", attr.SlogValueInt(candidate.threshold))
}

// startOfNextMonthUTC returns midnight UTC on the first day of the month after
// now, used to expire dedup reservations at the credit-reset boundary.
func startOfNextMonthUTC(now time.Time) time.Time {
	y, m, _ := now.UTC().Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
}
