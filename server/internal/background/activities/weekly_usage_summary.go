package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	"net/url"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	repo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
	"github.com/speakeasy-api/gram/server/internal/usage"
)

const (
	weeklyStorageProductID  = "agent_session_storage"
	weeklyScanningProductID = "risk_content_scans"
	weeklyEgressProductID   = "mcp_egress"
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

// WeeklyUsageSummary emails each organization's billing alert contact a weekly
// digest of storage tokens, scanning tokens, and MCP egress for completed UTC
// days in the active billing cycle, compared with the same number of completed
// days in the previous cycle.
type WeeklyUsageSummary struct {
	logger    *slog.Logger
	db        *pgxpool.Pool
	spendRepo *chrepo.Queries
	repo      *repo.Queries
	emails    *email.Service
	siteURL   *url.URL
}

func NewWeeklyUsageSummary(logger *slog.Logger, db *pgxpool.Pool, meterReadConn clickhouse.Conn, emails *email.Service, siteURL *url.URL) *WeeklyUsageSummary {
	return &WeeklyUsageSummary{
		logger:    logger.With(attr.SlogComponent("weekly_usage_summary")),
		db:        db,
		spendRepo: chrepo.New(meterReadConn),
		repo:      repo.New(db),
		emails:    emails,
		siteURL:   siteURL,
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
// summary email. Organizations on their first cycle day, or with no usage in
// either compared window, are skipped. Retries are safe: the Loops idempotency
// key is derived from the organization and the sweep's run date.
func (a *WeeklyUsageSummary) Send(ctx context.Context, args SendWeeklyUsageSummaryArgs) error {
	target := args.Target
	now := args.RunTime.UTC()
	reportingDay := utcMidnight(now)
	logger := a.logger.With(attr.SlogOrganizationID(target.OrganizationID))

	cycles := usage.BillingCycles(now, target.AnchorDay, 2)
	previous, current := cycles[0], cycles[1]
	if !reportingDay.After(current.Start) {
		logger.InfoContext(ctx, "skipping weekly usage summary on first billing cycle day")
		return nil
	}

	completedDays := int(reportingDay.Sub(current.Start) / (24 * time.Hour))
	previousTo := previous.Start.AddDate(0, 0, completedDays)
	if previousTo.After(previous.End) {
		previousTo = previous.End
	}

	currentRows, err := a.spendRepo.GetSpend(ctx, chrepo.SpendParams{
		OrganizationID: target.OrganizationID,
		From:           current.Start,
		To:             reportingDay,
	})
	if err != nil {
		return fmt.Errorf("query current cycle meter usage: %w", err)
	}
	previousRows, err := a.spendRepo.GetSpend(ctx, chrepo.SpendParams{
		OrganizationID: target.OrganizationID,
		From:           previous.Start,
		To:             previousTo,
	})
	if err != nil {
		return fmt.Errorf("query previous cycle meter usage: %w", err)
	}

	currentTotals, err := weeklySpendTotalsFromRows(currentRows, target.AccountType == string(billing.TierPayg))
	if err != nil {
		return fmt.Errorf("total current cycle meter usage: %w", err)
	}
	previousTotals, err := weeklySpendTotalsFromRows(previousRows, false)
	if err != nil {
		return fmt.Errorf("total previous cycle meter usage: %w", err)
	}
	if currentTotals.empty() && previousTotals.empty() {
		logger.InfoContext(ctx, "skipping weekly usage summary for org without usage")
		return nil
	}

	configuredEmail := conv.PtrEmpty(target.AlertEmail)
	// Targets persisted by workflows started before AccountType was added came
	// only from the legacy enterprise-with-email query.
	accountType := conv.Default(target.AccountType, string(billing.TierEnterprise))
	recipients, resolutionErr := resolveBillingNotificationRecipients(ctx, a.db, target.OrganizationID, accountType, configuredEmail)
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

	showEstimatedSpend := accountType == string(billing.TierPayg)
	tmpl := email.WeeklyUsageSummary{
		OrganizationName:      conv.Default(target.OrganizationName, "your organization"),
		CycleEndDate:          current.End.AddDate(0, 0, -1).Format("January 2, 2006"),
		DaysRemaining:         formatDaysRemaining(daysUntil(now, current.End)),
		UsageThroughDate:      reportingDay.AddDate(0, 0, -1).Format("January 2, 2006"),
		StorageTokens:         formatExactInteger(currentTotals.storage),
		StorageChangePercent:  usageChangePercent(currentTotals.storage, previousTotals.storage),
		ScanningTokens:        formatExactInteger(currentTotals.scanning),
		ScanningChangePercent: usageChangePercent(currentTotals.scanning, previousTotals.scanning),
		EgressGib:             formatGiB(currentTotals.egress),
		EgressChangePercent:   usageChangePercent(currentTotals.egress, previousTotals.egress),
		ShowEstimatedSpend:    fmt.Sprintf("%t", showEstimatedSpend),
		StorageCostUSD:        "",
		ScanningCostUSD:       "",
		EgressCostUSD:         "",
		TotalCostUSD:          "",
		ViewUsageURL:          viewUsageURL,
	}
	if showEstimatedSpend {
		tmpl.StorageCostUSD = formatUSD(currentTotals.storageCost)
		tmpl.ScanningCostUSD = formatUSD(currentTotals.scanningCost)
		tmpl.EgressCostUSD = formatUSD(currentTotals.egressCost)
		tmpl.TotalCostUSD = formatUSD(currentTotals.totalCost())
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

type weeklySpendTotals struct {
	storage      *big.Int
	scanning     *big.Int
	egress       *big.Int
	storageCost  *big.Rat
	scanningCost *big.Rat
	egressCost   *big.Rat
}

func weeklySpendTotalsFromRows(rows []chrepo.SpendRow, calculateCosts bool) (weeklySpendTotals, error) {
	totals := weeklySpendTotals{
		storage:      new(big.Int),
		scanning:     new(big.Int),
		egress:       new(big.Int),
		storageCost:  nil,
		scanningCost: nil,
		egressCost:   nil,
	}
	for _, row := range rows {
		quantity, ok := new(big.Int).SetString(row.Quantity, 10)
		if !ok {
			return weeklySpendTotals{}, fmt.Errorf("invalid exact spend quantity %q", row.Quantity)
		}
		switch row.ProductID {
		case weeklyStorageProductID:
			totals.storage.Add(totals.storage, quantity)
		case weeklyScanningProductID:
			totals.scanning.Add(totals.scanning, quantity)
		case weeklyEgressProductID:
			totals.egress.Add(totals.egress, quantity)
		default:
			return weeklySpendTotals{}, fmt.Errorf("unknown spend product %q", row.ProductID)
		}
	}
	if !calculateCosts {
		return totals, nil
	}
	totals.storageCost = new(big.Rat)
	totals.scanningCost = new(big.Rat)
	totals.egressCost = new(big.Rat)

	for _, spec := range usage.SpendProductSpecs() {
		var quantity *big.Int
		var destination *big.Rat
		switch spec.ID {
		case weeklyStorageProductID:
			quantity, destination = totals.storage, totals.storageCost
		case weeklyScanningProductID:
			quantity, destination = totals.scanning, totals.scanningCost
		case weeklyEgressProductID:
			quantity, destination = totals.egress, totals.egressCost
		default:
			return weeklySpendTotals{}, fmt.Errorf("unknown priced spend product %q", spec.ID)
		}
		cost, err := usage.PriceSpendQuantity(quantity, spec)
		if err != nil {
			return weeklySpendTotals{}, fmt.Errorf("price weekly usage for %s: %w", spec.ID, err)
		}
		destination.Set(cost)
	}
	return totals, nil
}

func (t weeklySpendTotals) empty() bool {
	return t.storage.Sign() == 0 && t.scanning.Sign() == 0 && t.egress.Sign() == 0
}

func (t weeklySpendTotals) totalCost() *big.Rat {
	return new(big.Rat).Add(new(big.Rat).Add(t.storageCost, t.scanningCost), t.egressCost)
}

func utcMidnight(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
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
// so the email copy pluralizes correctly.
func formatDaysRemaining(days int) string {
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}

func formatExactInteger(value *big.Int) string {
	digits := value.String()
	start := 0
	if strings.HasPrefix(digits, "-") {
		start = 1
	}
	for i := len(digits) - 3; i > start; i -= 3 {
		digits = digits[:i] + "," + digits[i:]
	}
	return digits
}

func formatGiB(bytes *big.Int) string {
	gib := new(big.Rat).SetFrac(new(big.Int).Set(bytes), big.NewInt(1_073_741_824))
	if gib.Sign() > 0 && gib.Cmp(big.NewRat(1, 100)) < 0 {
		return "<0.01"
	}
	return gib.FloatString(2)
}

func formatUSD(cost *big.Rat) string {
	if cost.Sign() > 0 && cost.Cmp(big.NewRat(1, 100)) < 0 {
		return "<$0.01"
	}
	if cost.Sign() < 0 {
		return "-$" + new(big.Rat).Abs(cost).FloatString(2)
	}
	return "$" + cost.FloatString(2)
}

// usageChangePercent renders the exact signed percent change between current
// and previous quantities, rounded to the nearest whole percent.
func usageChangePercent(current, previous *big.Int) string {
	if previous.Sign() == 0 {
		if current.Sign() == 0 {
			return "0%"
		}
		return "New"
	}

	numerator := new(big.Int).Mul(new(big.Int).Sub(current, previous), big.NewInt(100))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, previous, remainder)
	if new(big.Int).Mul(new(big.Int).Abs(remainder), big.NewInt(2)).Cmp(new(big.Int).Abs(previous)) >= 0 {
		if numerator.Sign() < 0 {
			quotient.Sub(quotient, big.NewInt(1))
		} else {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	if quotient.Sign() >= 0 {
		return "+" + quotient.String() + "%"
	}
	return quotient.String() + "%"
}
