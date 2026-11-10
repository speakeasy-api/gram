package adminmcp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
)

const (
	// defaultProviderHealthWindowHours is a day: long enough to show a pattern at today's volume.
	defaultProviderHealthWindowHours = 24

	// defaultProviderHealthTrendDays is two weeks of the daily trend.
	defaultProviderHealthTrendDays = 14
)

var (
	errProviderHealthUnavailable = errors.New("provider health is unavailable")

	providerHealthWindowHours = []int{1, 24, 168}
)

const (
	// unknownReasonText stands in for a reason the catalog does not know, such as other.
	unknownReasonText = "Unknown reason."

	// unknownWhoFixes is who fixes an unknown reason.
	unknownWhoFixes = "unknown"
)

// ProviderHealthReader reads provider health across organizations, as counts only.
type ProviderHealthReader interface {
	ReadProviderHealth(context.Context, mcpconnectionattempts.ProviderHealthQuery) ([]mcpconnectionattempts.ProviderHealthReport, error)
}

// ProviderHealthInput selects providers and how far back to look.
type ProviderHealthInput struct {
	// Provider keeps one provider.
	Provider string `json:"provider,omitempty" jsonschema:"One known provider host, such as accounts.google.com, or other; omit for every provider"`

	// WindowHours bounds failures, reconnect loops and abandons.
	WindowHours int `json:"window_hours,omitempty" jsonschema:"Window for failures, reconnect loops and abandons in hours: 1, 24 or 168 (default 24). Status always uses the last hour."`

	// TrendDays bounds the daily trend.
	TrendDays *int `json:"trend_days,omitempty" jsonschema:"Days of daily trend from 0 to 395 (default 14)"`
}

// ProviderHealthOutput is provider health across every organization. It names no organization.
type ProviderHealthOutput struct {
	// WindowHours is the detail window.
	WindowHours int `json:"window_hours"`

	// Thresholds are what an outage needs.
	Thresholds ProviderHealthThresholds `json:"thresholds"`

	// Providers are every known provider, busiest in the last hour first, then other when it had attempts.
	Providers []ProviderHealthEntry `json:"providers"`
}

// ProviderHealthThresholds are the outage rules. Meeting all is an outage, all but one is elevated.
type ProviderHealthThresholds struct {
	// StatusWindowMinutes is the recent stretch judged.
	StatusWindowMinutes int `json:"status_window_minutes"`

	// BaselineDays is the provider's own past it is compared with.
	BaselineDays int `json:"baseline_days"`

	// MinProviderFailures is the fewest failed attempts on the provider's side.
	MinProviderFailures int `json:"min_provider_failures"`

	// MinOrganizations is the fewest organizations with such failures.
	MinOrganizations int `json:"min_organizations"`

	// RateMultiple is how many times the usual failure rate.
	RateMultiple int `json:"rate_multiple"`

	// RateIncrease is how far above the usual failure rate, as a fraction.
	RateIncrease float64 `json:"rate_increase"`
}

// ProviderHealthEntry is one provider.
type ProviderHealthEntry struct {
	// Provider is a known provider host or other.
	Provider string `json:"provider"`

	// Status is outage, elevated, normal, quiet or not_checked.
	Status string `json:"status"`

	// Summary says the status in one plain sentence.
	Summary string `json:"summary"`

	// LastHour backs Status.
	LastHour ProviderHealthLastHour `json:"last_hour"`

	// Failures are the most common ways attempts ended failed in the window, whoever has to fix them.
	Failures []ProviderHealthFailure `json:"failures"`

	// ReconnectLoops signed in to the provider, then still needed sign-in or were rejected by the server.
	ReconnectLoops uint64 `json:"reconnect_loops"`

	// Abandoned stopped with nothing recorded, by step.
	Abandoned []ProviderHealthStep `json:"abandoned"`

	// Daily is the long-term trend, oldest first.
	Daily []ProviderHealthDay `json:"daily"`
}

// ProviderHealthLastHour counts attempts in the status window.
type ProviderHealthLastHour struct {
	// Attempts reached the provider.
	Attempts uint64 `json:"attempts"`

	// ProviderFailures ended on a failure the provider has to fix.
	ProviderFailures uint64 `json:"provider_failures"`

	// OrganizationsAffected is how many organizations had one. A count only.
	OrganizationsAffected int `json:"organizations_affected"`

	// FailureRate is provider failures over attempts.
	FailureRate float64 `json:"failure_rate"`

	// UsualFailureRate is the same rate over the baseline, null when the baseline has no attempts.
	UsualFailureRate *float64 `json:"usual_failure_rate"`
}

// ProviderHealthFailure is one way attempts ended failed.
type ProviderHealthFailure struct {
	// Step is where attempts ended.
	Step string `json:"step"`

	// Reason is the code.
	Reason string `json:"reason"`

	// WhatHappened is the plain explanation.
	WhatHappened string `json:"what_happened"`

	// WhoFixes is the party that has to act.
	WhoFixes string `json:"who_fixes"`

	// Attempts ended this way.
	Attempts uint64 `json:"attempts"`
}

// ProviderHealthStep is a count at one step.
type ProviderHealthStep struct {
	// Step is the step.
	Step string `json:"step"`

	// Attempts counted there.
	Attempts uint64 `json:"attempts"`
}

// ProviderHealthDay is one UTC day.
type ProviderHealthDay struct {
	// Day is YYYY-MM-DD.
	Day string `json:"day"`

	// Attempts started that day.
	Attempts uint64 `json:"attempts"`

	// Failed ended failed.
	Failed uint64 `json:"failed"`

	// Abandoned ended with nothing.
	Abandoned uint64 `json:"abandoned"`

	// Fixed failed and were fixed later.
	Fixed uint64 `json:"fixed"`
}

func registerProviderHealthTools(server *mcp.Server, health ProviderHealthReader) {
	mcp.AddTool(server, &mcp.Tool{
		Meta: nil, InputSchema: nil, OutputSchema: nil, Icons: nil,
		Name:  "describe_provider_health",
		Title: "Describe Provider Health",
		Description: "Show whether known MCP sign-in providers (Google, Microsoft, Linear, Notion, ...) are failing across all organizations: is it us, the app or the provider? " +
			"Status compares the last hour's ended attempts that failed on the provider's side with the same provider's last 7 closed days: outage meets the rate threshold and both count thresholds in thresholds, elevated the rate and one count, normal had ended attempts and is neither, quiet had none, and other (unlisted providers, many in one) is not_checked. Attempts count by when they ended: one that stops without a final row ends when its 60-minute live window does, so it reaches status one to two hours after it happened. Status reads failure codes only: an outage that only leaves people abandoned on the provider's sign-in page is not detected, so check abandoned too. " +
			"failures, reconnect_loops and abandoned cover window_hours; daily and the baseline come from the 13-month table, which lags by up to a day and counts a provider used by fewer than 3 organizations that day under other. The demo organization is left out. " +
			"Counts only: no organization, project, person or server is named. organizations_affected can be 1 or 2, so treat every number here as staff-only and never share it, or anything worked out from it, with customers.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(false), IdempotentHint: true, OpenWorldHint: new(false), Title: ""},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ProviderHealthInput) (*mcp.CallToolResult, ProviderHealthOutput, error) {
		if !verifiedStaff(ctx) || health == nil {
			return nil, ProviderHealthOutput{}, errProviderHealthUnavailable
		}
		if input.Provider != "" && !mcpconnectionattempts.ValidProviderLabel(input.Provider) {
			return nil, ProviderHealthOutput{}, errors.New("provider must be a known provider host or other")
		}
		window := input.WindowHours
		if window == 0 {
			window = defaultProviderHealthWindowHours
		}
		if !slices.Contains(providerHealthWindowHours, window) {
			return nil, ProviderHealthOutput{}, errors.New("window_hours must be 1, 24 or 168")
		}
		trend := defaultProviderHealthTrendDays
		if input.TrendDays != nil {
			trend = *input.TrendDays
		}
		if trend < 0 || trend > mcpconnectionattempts.ProviderHealthMaxTrendDays {
			return nil, ProviderHealthOutput{}, fmt.Errorf("trend_days must be from 0 to %d", mcpconnectionattempts.ProviderHealthMaxTrendDays)
		}

		reports, err := health.ReadProviderHealth(ctx, mcpconnectionattempts.ProviderHealthQuery{
			Provider: input.Provider, Window: time.Duration(window) * time.Hour, TrendDays: trend,
		})
		if err != nil {
			return nil, ProviderHealthOutput{}, errProviderHealthUnavailable
		}
		out := ProviderHealthOutput{
			WindowHours: window,
			Thresholds: ProviderHealthThresholds{
				StatusWindowMinutes: int(mcpconnectionattempts.OutageWindow / time.Minute),
				BaselineDays:        int(mcpconnectionattempts.OutageBaseline / (24 * time.Hour)),
				MinProviderFailures: mcpconnectionattempts.OutageMinFailures,
				MinOrganizations:    mcpconnectionattempts.OutageMinOrganizations,
				RateMultiple:        mcpconnectionattempts.OutageRateMultiple,
				RateIncrease:        mcpconnectionattempts.OutageRateIncrease,
			},
			Providers: make([]ProviderHealthEntry, 0, len(reports)),
		}
		for _, report := range reports {
			out.Providers = append(out.Providers, providerHealthEntry(report))
		}
		return nil, out, nil
	})
}

func providerHealthEntry(report mcpconnectionattempts.ProviderHealthReport) ProviderHealthEntry {
	failures := make([]ProviderHealthFailure, 0, len(report.Failures))
	for _, failure := range report.Failures {
		whatHappened, whoFixes := unknownReasonText, unknownWhoFixes
		if meaning, ok := mcpconnectionattempts.MeaningOf(failure.Reason); ok {
			whatHappened, whoFixes = meaning.WhatHappened, string(meaning.WhoFixes)
		}
		failures = append(failures, ProviderHealthFailure{
			Step: failure.Step, Reason: failure.Reason, WhatHappened: whatHappened,
			WhoFixes: whoFixes, Attempts: failure.Attempts,
		})
	}
	abandoned := make([]ProviderHealthStep, 0, len(report.Abandoned))
	for _, step := range report.Abandoned {
		abandoned = append(abandoned, ProviderHealthStep{Step: step.Step, Attempts: step.Attempts})
	}
	daily := make([]ProviderHealthDay, 0, len(report.Daily))
	for _, day := range report.Daily {
		daily = append(daily, ProviderHealthDay{Day: day.Day.Format(time.DateOnly), Attempts: day.Attempts, Failed: day.Failed, Abandoned: day.Abandoned, Fixed: day.Fixed})
	}
	return ProviderHealthEntry{
		Provider: report.Provider,
		Status:   string(report.Status),
		Summary:  providerHealthSummary(report),
		LastHour: ProviderHealthLastHour{
			Attempts: report.Counts.Attempts, ProviderFailures: report.Counts.Failures,
			OrganizationsAffected: report.Counts.Organizations,
			FailureRate:           report.Counts.Rate(), UsualFailureRate: usualFailureRate(report.Counts),
		},
		Failures:       failures,
		ReconnectLoops: report.ReconnectLoops,
		Abandoned:      abandoned,
		Daily:          daily,
	}
}

func providerHealthSummary(report mcpconnectionattempts.ProviderHealthReport) string {
	c := report.Counts
	usual := "There is no usual rate yet."
	if rate := usualFailureRate(c); rate != nil {
		usual = fmt.Sprintf("Usually %s fail.", percent(*rate))
	}
	switch report.Status {
	case mcpconnectionattempts.ProviderOutage:
		return fmt.Sprintf("%s looks down: %d of %s in the last hour failed on its side, in %s. %s",
			report.Provider, c.Failures, countOf(c.Attempts, "attempt"), countOf(c.Organizations, "organization"), usual)
	case mcpconnectionattempts.ProviderElevated:
		return fmt.Sprintf("%s is failing more than usual: %d of %s in the last hour failed on its side, in %s. %s",
			report.Provider, c.Failures, countOf(c.Attempts, "attempt"), countOf(c.Organizations, "organization"), usual)
	case mcpconnectionattempts.ProviderNormal:
		return fmt.Sprintf("%s looks normal: %d of %s in the last hour failed on its side.", report.Provider, c.Failures, countOf(c.Attempts, "attempt"))
	case mcpconnectionattempts.ProviderQuiet:
		return fmt.Sprintf("No attempts reached %s in the last hour.", report.Provider)
	default:
		return "Providers not on the known list are counted together as other and not checked for outages."
	}
}

// percent shows a rate, with one decimal below 1% so a small rate does not read as none.
func percent(rate float64) string {
	if rate > 0 && rate < 0.01 {
		return fmt.Sprintf("%.1f%%", 100*rate)
	}
	return fmt.Sprintf("%.0f%%", 100*rate)
}

// usualFailureRate is the baseline failure rate, nil when the baseline has no attempts.
func usualFailureRate(c mcpconnectionattempts.ProviderCounts) *float64 {
	if c.BaselineAttempts == 0 {
		return nil
	}
	return new(c.BaselineRate())
}

func countOf[N int | uint64](n N, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
