package repo

// Identity-bearing attribute paths. A filter on an attribute that resolves to
// one of these columns selects one named individual's rows, which is a
// different kind of read from narrowing by tool, server, or outcome: callers
// that must audit an attribution read before performing one have to be able to
// recognise such a filter, rather than discovering it as a same-shaped
// attribute predicate.
//
// identityAttributeColumns lists the telemetry columns the audited person
// filter itself owns — the three ToolUsageUserFilter kinds address (see
// toolUsageUserKindEmail and friends) and withUserIdentityFilter matches an
// employee on. A predicate reaching one of them is the same read the person
// filter performs, spelled as an attribute.
var identityAttributeColumns = map[string]struct{}{
	"user_email":       {},
	"user_id":          {},
	"external_user_id": {},
}

// IsIdentityAttributePath reports whether a bare attribute path resolves to a
// materialized column that identifies one person. Custom "@" keys and paths
// with no materialized column are not classified here: they carry whatever a
// project's own integrations attached, so a caller that must not serve an
// unaudited identity read applies its own policy to them.
//
// Every materialized path is classified one way or the other, and
// TestIdentityAttributeColumns_ClassifyEveryMaterializedPath fails when a new
// column appears without that decision having been made.
func IsIdentityAttributePath(path string) bool {
	column, ok := materializedColumns[path]
	if !ok {
		return false
	}
	_, identity := identityAttributeColumns[column]
	return identity
}
