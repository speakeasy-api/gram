package chrepo

import "fmt"

// ActorSpendRow is one actor's total LLM cost over the queried range, keyed by
// the user_email recorded on ClickHouse rows.
type ActorSpendRow struct {
	Email     string  `ch:"user_email"`
	TotalCost float64 `ch:"m_total_cost"`
}

// ActorWindowSpendRow is one actor's total LLM cost across the fixed windows
// used by spend-rule enforcement.
type ActorWindowSpendRow struct {
	Email       string  `ch:"user_email" json:"email"`
	DailyCost   float64 `ch:"m_daily_total_cost" json:"daily_cost"`
	WeeklyCost  float64 `ch:"m_weekly_total_cost" json:"weekly_cost"`
	MonthlyCost float64 `ch:"m_monthly_total_cost" json:"monthly_cost"`
}

func (s ActorWindowSpendRow) SpendUSD(kind string) (float64, error) {
	switch kind {
	case "daily":
		return s.DailyCost, nil
	case "weekly":
		return s.WeeklyCost, nil
	case "monthly":
		return s.MonthlyCost, nil
	default:
		return 0, fmt.Errorf("unknown window kind %q", kind)
	}
}
