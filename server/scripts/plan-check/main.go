// plan-check runs EXPLAIN (FORMAT JSON) for a curated list of sqlc queries
// against a freshly migrated and seeded Postgres, then fails CI if any plan
// contains a Seq Scan on a forbidden relation or exceeds a cost threshold.
//
// Usage:
//
//	go run ./server/scripts/plan-check \
//	    -manifest server/scripts/plan-check/manifest.yaml \
//	    -repo-root . \
//	    -dsn "postgres://postgres:pass@localhost:5432/dev?sslmode=disable"
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"gopkg.in/yaml.v3"
)

type paramSpec struct {
	Type  string `yaml:"type"`
	Value any    `yaml:"value"`
}

type queryCheck struct {
	Name            string               `yaml:"name"`
	File            string               `yaml:"file"`
	Params          map[string]paramSpec `yaml:"params"`
	ForbidSeqScanOn []string             `yaml:"forbid_seq_scan_on"`
	MaxTotalCost    float64              `yaml:"max_total_cost"`
}

type manifest struct {
	Version int          `yaml:"version"`
	Queries []queryCheck `yaml:"queries"`
}

type plan struct {
	NodeType  string  `json:"Node Type"`
	Relation  string  `json:"Relation Name"`
	TotalCost float64 `json:"Total Cost"`
	PlanRows  float64 `json:"Plan Rows"`
	Plans     []plan  `json:"Plans"`
}

type explainOutput []struct {
	Plan plan `json:"Plan"`
}

func main() {
	var manifestPath, repoRoot, dsn string
	flag.StringVar(&manifestPath, "manifest", "server/scripts/plan-check/manifest.yaml", "path to plan-check manifest")
	flag.StringVar(&repoRoot, "repo-root", ".", "repository root used to resolve query file paths")
	flag.StringVar(&dsn, "dsn", os.Getenv("PLAN_CHECK_DSN"), "Postgres DSN (or PLAN_CHECK_DSN env)")
	flag.Parse()

	if dsn == "" {
		fail("missing -dsn (or PLAN_CHECK_DSN)")
	}

	m, err := loadManifest(manifestPath)
	if err != nil {
		fail("load manifest: %v", err)
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		fail("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	violations := 0
	for _, q := range m.Queries {
		sqlText, err := loadQuery(filepath.Join(repoRoot, q.File), q.Name)
		if err != nil {
			fail("load query %s: %v", q.Name, err)
		}
		bound, err := bindParams(sqlText, q.Params)
		if err != nil {
			fail("bind %s: %v", q.Name, err)
		}

		var raw []byte
		if err := conn.QueryRow(ctx, "EXPLAIN (FORMAT JSON, BUFFERS) "+bound).Scan(&raw); err != nil {
			fail("explain %s: %v\nsql:\n%s", q.Name, err, bound)
		}
		var out explainOutput
		if err := json.Unmarshal(raw, &out); err != nil {
			fail("parse explain %s: %v", q.Name, err)
		}
		if len(out) == 0 {
			fail("explain %s returned no plan", q.Name)
		}

		problems := inspect(out[0].Plan, q)
		if len(problems) == 0 {
			fmt.Printf("OK    %s (cost=%.1f)\n", q.Name, out[0].Plan.TotalCost)
			continue
		}
		violations++
		fmt.Printf("FAIL  %s\n", q.Name)
		for _, p := range problems {
			fmt.Printf("        - %s\n", p)
			emitGitHubAnnotation(q.File, p)
		}
		fmt.Printf("      plan:\n%s\n", indent(string(raw), "        "))
	}

	if violations > 0 {
		fmt.Fprintf(os.Stderr, "\n%d plan-check violation(s)\n", violations)
		os.Exit(1)
	}
}

func loadManifest(path string) (*manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m manifest
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m.Version != 1 {
		return nil, fmt.Errorf("unsupported manifest version: %d", m.Version)
	}
	return &m, nil
}

var queryHeader = regexp.MustCompile(`(?m)^-- name: (\w+)\b`)

func loadQuery(path, name string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	src := string(b)
	matches := queryHeader.FindAllStringSubmatchIndex(src, -1)
	for i, m := range matches {
		if src[m[2]:m[3]] != name {
			continue
		}
		// Body runs from end of header line to start of next header (or EOF).
		bodyStart := strings.Index(src[m[1]:], "\n")
		if bodyStart < 0 {
			return "", fmt.Errorf("query %s has no body", name)
		}
		bodyStart += m[1] + 1
		bodyEnd := len(src)
		if i+1 < len(matches) {
			bodyEnd = matches[i+1][0]
		}
		return strings.TrimSpace(src[bodyStart:bodyEnd]), nil
	}
	return "", fmt.Errorf("query %s not found in %s", name, path)
}

func bindParams(sqlText string, params map[string]paramSpec) (string, error) {
	out := sqlText
	for name, spec := range params {
		lit, err := literal(spec)
		if err != nil {
			return "", fmt.Errorf("param %s: %w", name, err)
		}
		// sqlc supports both `@name` and `sqlc.arg(name)` styles.
		out = strings.ReplaceAll(out, "@"+name, lit)
		out = strings.ReplaceAll(out, fmt.Sprintf("sqlc.arg(%s)", name), lit)
	}
	if strings.Contains(out, "@") {
		return "", errors.New("unbound @param remaining; manifest missing entries")
	}
	return out, nil
}

func literal(spec paramSpec) (string, error) {
	switch spec.Type {
	case "uuid":
		return fmt.Sprintf("'%v'::uuid", spec.Value), nil
	case "int":
		return fmt.Sprintf("%v", spec.Value), nil
	case "text":
		s := fmt.Sprintf("%v", spec.Value)
		return "'" + strings.ReplaceAll(s, "'", "''") + "'", nil
	case "bool":
		return fmt.Sprintf("%v", spec.Value), nil
	default:
		return "", fmt.Errorf("unknown type %q", spec.Type)
	}
}

func inspect(p plan, q queryCheck) []string {
	var problems []string
	forbidden := map[string]struct{}{}
	for _, r := range q.ForbidSeqScanOn {
		forbidden[r] = struct{}{}
	}
	var walk func(p plan)
	walk = func(p plan) {
		if p.NodeType == "Seq Scan" {
			if _, banned := forbidden[p.Relation]; banned {
				problems = append(problems,
					fmt.Sprintf("Seq Scan on %s (%.0f rows, cost=%.1f). Add or extend an index covering this query's predicate.",
						p.Relation, p.PlanRows, p.TotalCost))
			}
		}
		for _, c := range p.Plans {
			walk(c)
		}
	}
	walk(p)
	if q.MaxTotalCost > 0 && p.TotalCost > q.MaxTotalCost {
		problems = append(problems,
			fmt.Sprintf("total cost %.1f exceeds max %.1f", p.TotalCost, q.MaxTotalCost))
	}
	return problems
}

func emitGitHubAnnotation(file, msg string) {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		return
	}
	fmt.Printf("::error file=%s::%s\n", file, msg)
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "plan-check: "+format+"\n", args...)
	os.Exit(2)
}
