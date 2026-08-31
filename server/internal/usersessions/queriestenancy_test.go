package usersessions_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A user_session_issuer and its four child tables can be owned by a project or
// by an organization, so tenancy on them is
//
//	x.project_id = @project_id OR (x.project_id IS NULL AND x.organization_id = @organization_id)
//
// The `x.project_id IS NULL` guard is the load-bearing half. Every row written
// today carries BOTH columns, so a predicate that reduces to
// `project_id = @project_id OR organization_id = @organization_id` matches every
// sibling project's rows in the same organization: a cross-project read on the
// listings, and a cross-project write on the revoke and delete paths.
//
// That is not something review reliably catches across ~50 near-identical
// predicates, and it is not something the compiler or sqlc can catch at all.
// This test catches it, in every queries.sql under internal/ rather than a
// fixed list, so a package that starts joining these tables is covered as soon
// as it does.
//
// What it deliberately does NOT catch is a query that scopes these tables by
// nothing at all. Many do so legitimately: the public OAuth surface carries no
// project header and treats the issuer id as the authoritative scope, which
// GetUserSessionClientByClientID and IssuerAdmitsCimdClientURI both say in as
// many words. Telling those apart from an accidental omission needs a
// judgement this test cannot make, so it checks the shape of a tenancy
// predicate that is present rather than the presence of one.

// tenancyScopedTables are the five tables that carry the project/organization
// tier pair. A predicate on any other table's project_id is none of this
// test's business.
var tenancyScopedTables = map[string]bool{
	"user_session_issuers":             true,
	"user_session_clients":             true,
	"user_session_issuer_cimd_clients": true,
	"user_sessions":                    true,
	"user_session_consents":            true,
}

// tenancyExemptQueries are the queries that scope one of those tables by
// project alone on purpose. Every entry needs a reason, and the test fails if
// an entry no longer names a real query, so the list cannot rot into a
// blanket suppression.
var tenancyExemptQueries = map[string]string{
	"GetUserSessionIssuerBySlug": "slug uniqueness is indexed per project with no (organization_id, slug) " +
		"equivalent, so admitting organization-tier rows would make this :one query non-deterministic",

	"LockUserSessionIssuerForMetaMCP": "meta_mcp_servers pins its issuer with a composite foreign key on " +
		"(project_id, user_session_issuer_id) that never matches a NULL project_id, so widening this lock " +
		"alone would turn a clean 404 into a foreign key violation surfacing as a 500",

	"ForceSoftDeleteUserSessionIssuer":      "test-only fixture that targets one known project-tier row",
	"SetUserSessionIssuerCIMDAdmissionMode": "test-only fixture that targets one known project-tier row",
	"SetUserSessionIssuerOrganizationID":    "test-only fixture that targets one known project-tier row",
}

// organizationRHS pins what the guard may compare the organization to: the
// caller's own @organization_id parameter, or a correlated organization column
// from a table already scoped in the same query (IsPlatformMCPNewModelEligible
// compares to projects.organization_id, which the surrounding join has already
// pinned to the caller). Without this the guard would accept any right-hand
// side at all, so `organization_id = @some_other_org` would read as scoped.
const organizationRHS = `organization_id\s*=\s*(?:@organization_id\b|\w+\.organization_id\b)`

// bindPattern captures the table bindings a block establishes: FROM, JOIN,
// UPDATE, INTO and USING, plus comma-separated table lists. An unaliased table
// binds under its own name.
var bindPattern = regexp.MustCompile(`(?i)(?:\b(?:FROM|JOIN|UPDATE|INTO|USING)\s+|,\s*)(\w+)(?:\s+AS\s+(\w+))?`)

func TestQueriesScopeUserSessionTablesToBothTiers(t *testing.T) {
	t.Parallel()

	files := findQueryFiles(t)
	require.NotEmpty(t, files, "found no queries.sql files to check")

	var violations []string
	seen := map[string]bool{}

	for _, file := range files {
		raw, err := os.ReadFile(file)
		require.NoError(t, err)

		for _, block := range splitNamedQueries(string(raw)) {
			seen[block.name] = true
			if _, exempt := tenancyExemptQueries[block.name]; exempt {
				continue
			}
			for _, alias := range unguardedAliases(block.body) {
				violations = append(violations, fmt.Sprintf(
					"%s: %s scopes %s by project alone (alias %q)",
					file, block.name, alias.table, alias.name))
			}
		}
	}

	for name := range tenancyExemptQueries {
		require.True(t, seen[name],
			"exemption names %q, which is not a query in any queries.sql; remove the stale entry", name)
	}

	sort.Strings(violations)
	require.Empty(t, violations, "queries must admit organization-tier rows with the "+
		"`project_id IS NULL AND organization_id = @organization_id` guard, or be listed in "+
		"tenancyExemptQueries with a reason:\n%s", strings.Join(violations, "\n"))
}

type namedQuery struct {
	name string
	body string
}

type tableAlias struct {
	name  string
	table string
}

// findQueryFiles walks the whole internal tree rather than a fixed list, so a
// new package that joins these tables is covered the day it is added.
func findQueryFiles(t *testing.T) []string {
	t.Helper()

	var files []string
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "queries.sql" {
			files = append(files, path)
		}
		return nil
	})
	require.NoError(t, err)

	sort.Strings(files)
	return files
}

// splitNamedQueries breaks a queries.sql into its `-- name:` blocks, with
// comment lines dropped so prose mentioning a predicate is never mistaken for
// one.
func splitNamedQueries(content string) []namedQuery {
	var out []namedQuery
	var current *namedQuery

	for line := range strings.SplitSeq(content, "\n") {
		if name, ok := strings.CutPrefix(line, "-- name:"); ok {
			fields := strings.Fields(name)
			if len(fields) == 0 {
				continue
			}
			out = append(out, namedQuery{name: fields[0], body: ""})
			current = &out[len(out)-1]
			continue
		}
		if current == nil || strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		current.body += line + "\n"
	}

	return out
}

// unguardedAliases returns every alias bound to one of the tenancy-scoped
// tables that is compared to a project parameter without the organization-tier
// arm alongside it.
func unguardedAliases(body string) []tableAlias {
	aliases := map[string]string{}
	for _, m := range bindPattern.FindAllStringSubmatch(body, -1) {
		table, alias := m[1], m[2]
		if !tenancyScopedTables[table] {
			continue
		}
		if alias == "" {
			alias = table
		}
		aliases[alias] = table
	}

	var out []tableAlias
	for alias, table := range aliases {
		// \b on the left so a short alias cannot match inside a longer one:
		// without it, alias "s" matches the "s." at the end of "iss.".
		q := `\b` + regexp.QuoteMeta(alias) + `\.`
		scoped := regexp.MustCompile(q + `project_id\s*=\s*@project_id`).MatchString(body)
		guarded := regexp.MustCompile(q + `project_id IS NULL AND ` + q + organizationRHS).MatchString(body)

		// A table bound without an alias may also be written bare. Go's regexp
		// has no lookbehind, so the bare spelling gets an explicit boundary
		// check rather than a prefix pattern.
		if alias == table {
			scoped = scoped || matchesBare(body, `project_id\s*=\s*@project_id`)
			guarded = guarded || matchesBare(body, `project_id IS NULL AND `+organizationRHS)
		}

		if scoped && !guarded {
			out = append(out, tableAlias{name: alias, table: table})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// matchesBare reports whether pattern occurs unqualified, i.e. not preceded by
// an alias prefix or another identifier character.
func matchesBare(body, pattern string) bool {
	re := regexp.MustCompile(pattern)
	for _, loc := range re.FindAllStringIndex(body, -1) {
		if loc[0] == 0 {
			return true
		}
		prev := body[loc[0]-1]
		if prev != '.' && prev != '_' && !isAlphanumeric(prev) {
			return true
		}
	}
	return false
}

func isAlphanumeric(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
