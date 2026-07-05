package chrepo

// ActorSpendRow is one actor's total LLM cost over the queried range, keyed by
// the user_email recorded on ClickHouse rows.
type ActorSpendRow struct {
	Email     string  `ch:"user_email"`
	TotalCost float64 `ch:"m_total_cost"`
}
