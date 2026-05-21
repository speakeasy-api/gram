package categories

// SQLRows returns the canonical classifier as five parallel arrays sized
// 1-to-1 by row. Pass them straight into any query that classifies findings
// via the standard CTE:
//
//	WITH risk_category_lookup(priority, category, source, rule_id, rule_prefix) AS (
//	  SELECT * FROM unnest(
//	    @cat_priority::int[],
//	    @cat_category::text[],
//	    @cat_source::text[],
//	    @cat_rule_id::text[],
//	    @cat_rule_prefix::text[]
//	  ) AS t(priority, category, source, rule_id, rule_prefix)
//	)
//
// The CTE is then joined per row with a small subquery that picks the
// first matching category by priority. See queries.sql.
//
// Returning parallel arrays (rather than a Definition struct slice and
// having every caller flatten it) keeps the boundary with sqlc clean:
// sqlc understands `unnest(text[], …)` natively; it does not understand
// per-row CTE composition.
func SQLRows() (priority []int32, category, source, ruleID, rulePrefix []string) {
	for prio, def := range Definitions {
		emit := func(src, id, prefix string) {
			priority = append(priority, int32(prio)) //nolint:gosec // bounded by Definitions length, far below int32 max
			category = append(category, string(def.Category))
			source = append(source, src)
			ruleID = append(ruleID, id)
			rulePrefix = append(rulePrefix, prefix)
		}
		switch {
		case def.Source != "":
			emit(def.Source, "", "")
		case len(def.RuleIDs) > 0:
			for _, id := range def.RuleIDs {
				emit("", id, "")
			}
		case def.RulePrefix != "":
			emit("", "", def.RulePrefix)
		}
	}
	return priority, category, source, ruleID, rulePrefix
}
