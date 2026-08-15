package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"strconv"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	repo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/email"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/usage"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

// WeeklyUsageSummaryTarget is one organization due a weekly usage summary
// email, resolved by ListTargets and passed back into Send by the workflow.
type WeeklyUsageSummaryTarget struct {
	OrganizationID   string
	OrganizationName string
	OrganizationSlug string
	AccountType      string
	AlertEmail       string
	AnchorDay        int
}

// SendWeeklyUsageSummaryArgs carries one send. RunTime is the sweep's
// workflow.Now so cycle math and the send idempotency key stay deterministic
// across activity retries.
type SendWeeklyUsageSummaryArgs struct {
	Target  WeeklyUsageSummaryTarget
	RunTime time.Time
}

// WeeklyUsageSummary emails each organization's billing alert contact a
// weekly digest of tokens-under-management usage so far in the active
// billing cycle, compared against the same elapsed point of the previous
// cycle. The reported total is computed by the same registry-driven measure
// that billing uses (billing.TumComponents via GetTumWindowTotal), so
// changes to the TUM definition show up in the email without any change
// here.
type WeeklyUsageSummary struct {
	logger        *slog.Logger
	db            *pgxpool.Pool
	telemetryRepo *telemetryrepo.Queries
	repo          *repo.Queries
	emails        *email.Service
	siteURL       *url.URL
}

func NewWeeklyUsageSummary(logger *slog.Logger, db *pgxpool.Pool, chConn clickhouse.Conn, emails *email.Service, siteURL *url.URL) *WeeklyUsageSummary {
	return &WeeklyUsageSummary{
		logger:        logger.With(attr.SlogComponent("weekly_usage_summary")),
		db:            db,
		telemetryRepo: telemetryrepo.New(chConn),
		repo:          repo.New(db),
		emails:        emails,
		siteURL:       siteURL,
	}
}

// ListTargets resolves the organizations due a weekly usage summary:
// enabled enterprise and PAYG organizations eligible for billing email.
func (a *WeeklyUsageSummary) ListTargets(ctx context.Context) ([]WeeklyUsageSummaryTarget, error) {
	rows, err := a.repo.ListWeeklyUsageSummaryTargets(ctx)
	if err != nil {
		return nil, fmt.Errorf("query weekly usage summary targets: %w", err)
	}

	targets := make([]WeeklyUsageSummaryTarget, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, WeeklyUsageSummaryTarget{
			OrganizationID:   row.OrganizationID,
			OrganizationName: row.OrganizationName,
			OrganizationSlug: row.OrganizationSlug,
			AccountType:      row.GramAccountType,
			AlertEmail:       conv.FromPGTextOrEmpty[string](row.AlertEmail),
			AnchorDay:        int(row.BillingCycleAnchorDay),
		})
	}
	return targets, nil
}

// Send computes one organization's cycle-to-date usage and dispatches the
// summary email. Organizations with no usage in either compared window are
// skipped. Retries are safe: the Loops idempotency key is derived from the
// organization and the sweep's run date.
func (a *WeeklyUsageSummary) Send(ctx context.Context, args SendWeeklyUsageSummaryArgs) error {
	target := args.Target
	now := args.RunTime.UTC()
	logger := a.logger.With(attr.SlogOrganizationID(target.OrganizationID))

	queries := usagerepo.New(a.db)
	projectIDs, err := queries.ListBillingProjectIDsByOrganization(ctx, target.OrganizationID)
	if err != nil {
		return fmt.Errorf("list organization projects: %w", err)
	}
	if len(projectIDs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(projectIDs))
	for _, id := range projectIDs {
		ids = append(ids, id.String())
	}

	cycles := usage.BillingCycles(now, target.AnchorDay, 2)
	previous, current := cycles[0], cycles[1]

	// "Previous cycle at this point" is the prior cycle truncated to the same
	// elapsed duration, clamped for anchored cycles of unequal length.
	elapsed := now.Sub(current.Start)
	previousPoint := previous.Start.Add(elapsed)
	if previousPoint.After(previous.End) {
		previousPoint = previous.End
	}

	currentTotal, err := a.telemetryRepo.GetTumWindowTotal(ctx, telemetryrepo.GetTokensUnderManagementParams{
		ProjectIDs:          ids,
		StartUnixNano:       current.Start.UnixNano(),
		EndUnixNano:         now.UnixNano(),
		ExcludedHookSources: billing.GramHostedHookSourceStrings(),
	})
	if err != nil {
		return fmt.Errorf("compute current cycle usage: %w", err)
	}
	previousTotal, err := a.telemetryRepo.GetTumWindowTotal(ctx, telemetryrepo.GetTokensUnderManagementParams{
		ProjectIDs:          ids,
		StartUnixNano:       previous.Start.UnixNano(),
		EndUnixNano:         previousPoint.UnixNano(),
		ExcludedHookSources: billing.GramHostedHookSourceStrings(),
	})
	if err != nil {
		return fmt.Errorf("compute previous cycle usage: %w", err)
	}

	if currentTotal == 0 && previousTotal == 0 {
		logger.InfoContext(ctx, "skipping weekly usage summary for org without usage")
		return nil
	}

	configuredEmail := conv.PtrEmpty(target.AlertEmail)
	recipients, resolutionErr := resolveBillingNotificationRecipients(ctx, a.db, target.OrganizationID, target.AccountType, configuredEmail)
	if len(recipients) == 0 {
		if resolutionErr == nil {
			logger.InfoContext(ctx, "skipping weekly usage summary without eligible recipient")
		}
		return resolutionErr
	}

	viewUsageURL := ""
	if a.siteURL != nil {
		viewUsageURL = a.siteURL.JoinPath(target.OrganizationSlug, "billing").String()
	}

	tmpl := email.WeeklyUsageSummary{
		OrganizationName: conv.Default(target.OrganizationName, "your organization"),
		// Cycle ends are exclusive; the email shows the last covered day.
		CycleEndDate:        current.End.AddDate(0, 0, -1).Format("January 2, 2006"),
		DaysRemaining:       formatDaysRemaining(daysUntil(now, current.End)),
		CycleElapsedPercent: strconv.Itoa(elapsedPercent(current, now)),
		TotalTokens:         formatTokenCount(currentTotal),
		PreviousTotalTokens: formatTokenCount(previousTotal),
		TotalChangePercent:  usageChangePercent(currentTotal, previousTotal),
		ViewUsageURL:        viewUsageURL,
	}

	deliveryErrors := []error{resolutionErr}
	for _, recipient := range recipients {
		idempotencyKey := recipientEmailIdempotencyKey(recipient, "weekly-usage-summary", target.OrganizationID, now.Format(time.DateOnly))
		if err := a.emails.SendIdempotent(ctx, recipient, idempotencyKey, tmpl); err != nil {
			deliveryErrors = append(deliveryErrors, fmt.Errorf("dispatch weekly usage summary email: %w", err))
		}
	}
	if err := errors.Join(deliveryErrors...); err != nil {
		return err
	}

	logger.InfoContext(ctx, "sent weekly usage summary")
	return nil
}

// daysUntil counts the whole days between now and the cycle end, rounding
// partial days up so "ends tomorrow morning" reads as 1, never 0.
func daysUntil(now, end time.Time) int {
	if !end.After(now) {
		return 0
	}
	return int(math.Ceil(end.Sub(now).Hours() / 24))
}

// formatDaysRemaining renders a day count with its unit ("1 day", "5 days")
// so the email copy pluralizes correctly; the Loops template inserts the
// phrase as-is.
func formatDaysRemaining(days int) string {
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}

// elapsedPercent is how far through the billing cycle now sits, clamped to
// [0, 100].
func elapsedPercent(cycle usage.BillingCyclePeriod, now time.Time) int {
	length := cycle.End.Sub(cycle.Start)
	if length <= 0 {
		return 100
	}
	pct := int(math.Round(float64(now.Sub(cycle.Start)) / float64(length) * 100))
	return min(max(pct, 0), 100)
}

// usageChangePercent renders the signed percent change between the current
// and previous window totals, e.g. "+19%". A previous total of zero has no
// meaningful ratio: it renders as "New" when usage appeared and "0%" when
// both windows are empty.
func usageChangePercent(current, previous int64) string {
	if previous == 0 {
		if current == 0 {
			return "0%"
		}
		return "New"
	}
	pct := int(math.Round(float64(current-previous) / float64(previous) * 100))
	return fmt.Sprintf("%+d%%", pct)
}
