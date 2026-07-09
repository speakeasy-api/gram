package activities

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/usage"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

// highestCrossedTumThreshold returns the highest alert threshold usage has
// crossed, as a percentage of the contracted limit: 50, 75, 90, 100, then
// every additional 50 beyond that. Returns 0 while usage sits below the
// lowest threshold.
func highestCrossedTumThreshold(usageTokens, limitTokens int64) int64 {
	pct := usageTokens * 100 / limitTokens
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
		return 100 + (pct-100)/50*50
	}
}

// maybeSendUsageAlert emails the org's billing alert contact when the active
// cycle's usage has crossed a threshold that has not been alerted yet.
// Alerting is strictly best-effort: every failure is logged and swallowed so
// the billing snapshot — the durable record — never fails on account of it.
func (s *SnapshotBillingCycleUsage) maybeSendUsageAlert(
	ctx context.Context,
	queries *usagerepo.Queries,
	orgID string,
	meta usagerepo.BillingMetadatum,
	cycle usage.BillingCyclePeriod,
	usageTokens int64,
	now time.Time,
) {
	limit := meta.TumMonthlyTokenLimit.Int64
	alertEmail := conv.FromPGText[string](meta.AlertEmail)
	if !meta.TumMonthlyTokenLimit.Valid || limit <= 0 || alertEmail == nil {
		return
	}

	threshold := highestCrossedTumThreshold(usageTokens, limit)
	if threshold == 0 {
		return
	}

	logger := s.logger.With(attr.SlogOrganizationID(orgID))

	// The limit is part of the dedup key on purpose: changing the contracted
	// allowance re-arms every threshold, so an org that was alerted at 50%
	// and then had its limit raised is alerted again when it crosses 50% of
	// the new limit. Only the highest crossed threshold is checked, so usage
	// blowing through several thresholds between runs sends one email.
	key := fmt.Sprintf("tum-usage-alert:%s:%d:%d:%d", orgID, cycle.Start.Unix(), limit, threshold)
	// Keys outlive the cycle by the finalize grace so late refreshes of a
	// just-closed cycle cannot re-alert; Redis then expires them.
	ttl := cycle.End.Sub(now) + billingCycleFinalizeGrace
	won, err := s.cache.Add(ctx, key, ttl)
	if err != nil {
		logger.ErrorContext(ctx, "failed to reserve tum usage alert", attr.SlogError(err))
		return
	}
	if !won {
		return
	}

	// From here on the alert is reserved: any failure releases the key so
	// the next hourly run retries the send.
	release := func(msg string, err error) {
		logger.ErrorContext(ctx, msg, attr.SlogError(err))
		if err := s.cache.Delete(ctx, key); err != nil {
			logger.ErrorContext(ctx, "failed to release tum usage alert reservation", attr.SlogError(err))
		}
	}

	orgName, err := queries.GetOrganizationName(ctx, orgID)
	if err != nil {
		release("failed to get organization name for tum usage alert", err)
		return
	}

	tmpl := tumUsageAlertTemplate(orgName, threshold, usageTokens, limit, cycle)
	if err := s.emails.Send(ctx, *alertEmail, tmpl); err != nil {
		release("failed to send tum usage alert", err)
		return
	}

	logger.InfoContext(ctx, "sent tum usage alert", attr.SlogValueInt(int(threshold)))
}

// tumUsageAlertTemplate picks the approach or overage email for a crossed
// threshold and fills in its display-formatted variables.
func tumUsageAlertTemplate(orgName string, threshold, usageTokens, limitTokens int64, cycle usage.BillingCyclePeriod) email.Template {
	const cycleDateFormat = "January 2, 2006"
	organizationName := conv.Default(orgName, "your organization")
	thresholdPercent := strconv.FormatInt(threshold, 10)
	cycleStart := cycle.Start.UTC().Format(cycleDateFormat)
	// Cycle ends are exclusive; the email shows the last covered day.
	cycleEnd := cycle.End.UTC().AddDate(0, 0, -1).Format(cycleDateFormat)

	if threshold < 100 {
		return email.TumUsageThreshold{
			OrganizationName: organizationName,
			ThresholdPercent: thresholdPercent,
			UsageTokens:      formatTokenCount(usageTokens),
			TokenLimit:       formatTokenCount(limitTokens),
			CycleStart:       cycleStart,
			CycleEnd:         cycleEnd,
		}
	}

	return email.TumUsageOverage{
		OrganizationName: organizationName,
		ThresholdPercent: thresholdPercent,
		UsageTokens:      formatTokenCount(usageTokens),
		TokenLimit:       formatTokenCount(limitTokens),
		OverageTokens:    formatTokenCount(usageTokens - limitTokens),
		CycleStart:       cycleStart,
		CycleEnd:         cycleEnd,
	}
}

// formatTokenCount renders a non-negative token count with thousands
// separators for email copy, e.g. 45000000 -> "45,000,000".
func formatTokenCount(n int64) string {
	digits := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}
