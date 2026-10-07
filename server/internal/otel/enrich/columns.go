package enrich

// LogColumns is every enricher that fills an agent_events column from a
// log record, in column order. The classification enricher comes first,
// since its event type is what every table keys on; the enrichers for the
// other columns join the list one ticket at a time, each in its own
// column_<name>.go file. The log transform appends them after the tenancy,
// token and directory enrichers, and the agent_events writer reads what
// they wrote.
//
// The instruments carry the missing-value counter the per-column enrichers
// record into; the classification enricher counts nothing.
func LogColumns(_ *Instruments) []LogEnricher {
	return []LogEnricher{
		&logClassification{},
	}
}
