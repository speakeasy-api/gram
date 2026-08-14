package norawuseremailfilter

import "github.com/Masterminds/squirrel"

func canonicalEmailExpr(orgLit, expr string) string {
	return "joinGet('identity_map', 'canonical_email', " + orgLit + ", lower(" + expr + "))"
}

func filters() {
	sb := squirrel.Select("user_email AS user_id")                                // projections are allowed
	sb = sb.Where("lower(user_email) = ?", "x")                                   // want "route email matching through the canonical identity fold"
	sb = sb.GroupBy("user_email")                                                 // want "route email matching through the canonical identity fold"
	sb = sb.Having("countIf(user_email != '') = 0")                               // want "route email matching through the canonical identity fold"
	sb = sb.Where(squirrel.Eq{"lower(telemetry_logs.user_email)": []string{"x"}}) // want "route email matching through the canonical identity fold"
	_ = sb
}

func predicateHelpers() {
	// Eq keys are column expressions wherever the literal lives.
	pred := squirrel.Eq{"user_email": ""} // want "route email matching through the canonical identity fold"
	_ = pred
	_ = squirrel.Expr("session_user_email IN (?)", "x") // want "route email matching through the canonical identity fold"
}

func allowed() {
	// The fold itself and literals routed through the canonical helpers.
	sb := squirrel.Select("x")
	sb = sb.Where("(user_id = '' AND " + canonicalEmailExpr("'org'", "telemetry_logs.user_email") + " = ?)")
	sb = sb.Where(squirrel.Eq{"user_id": "u"})
	// Map VALUES are filter data, not column expressions.
	sb = sb.Where(squirrel.Eq{"dimension": "user_email"})
	sb = sb.GroupBy("hook_hostname")
	_ = squirrel.Expr("joinGet('identity_map', 'canonical_user_id', 'org', lower(user_email))")
	_ = sb
}
