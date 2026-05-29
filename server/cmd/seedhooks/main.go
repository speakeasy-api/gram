// seedhooks inserts bursts of synthetic hook events into the local ClickHouse
// telemetry_logs table so the onboarding wizard's confirm-traffic step has
// something to render during demos.
//
// Usage:
//
//	go run ./cmd/seedhooks -org-slug acme        # resolves first project of that org
//	go run ./cmd/seedhooks -project-id <uuid>    # targets a specific project directly
//	go run ./cmd/seedhooks -project-id <uuid> -burst-size 8 -bursts 5 -interval 3s
//
// Connection details come from the same env vars the server reads
// (CLICKHOUSE_*, GRAM_DATABASE_URL). Run via `mise run seed:hook-events`.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type sourceProfile struct {
	serviceVersion string
	hookSource     string
	tools          []string
	events         []string
	// weight controls how often this source is selected. Higher = more frequent.
	weight int
}

var sources = []sourceProfile{
	{
		serviceVersion: "claude_code",
		hookSource:     "claude_code",
		tools:          []string{"Read", "Edit", "Write", "Bash", "Grep", "Glob", "WebFetch", "Task"},
		events:         []string{"SessionStart", "PreToolUse", "PostToolUse", "Stop"},
		weight:         6,
	},
	{
		serviceVersion: "cursor",
		hookSource:     "cursor",
		tools:          []string{"read_file", "edit_file", "run_terminal_cmd", "grep_search", "codebase_search"},
		events:         []string{"beforeSubmitPrompt", "preToolUse", "postToolUse", "afterAgentResponse"},
		weight:         3,
	},
	{
		serviceVersion: "codex",
		hookSource:     "codex",
		tools:          []string{"shell", "edit", "fs.read", "fs.write", "search"},
		events:         []string{"SessionStart", "PreToolUse", "PostToolUse", "UserPromptSubmit"},
		weight:         2,
	},
}

// pickSource returns a weighted-random source.
func pickSource(rng *rand.Rand) sourceProfile {
	total := 0
	for _, s := range sources {
		total += s.weight
	}
	pick := rng.IntN(total)
	for _, s := range sources {
		if pick < s.weight {
			return s
		}
		pick -= s.weight
	}
	return sources[0]
}

var demoUsers = []string{
	"adam@speakeasy.com",
	"quinn@speakeasy.com",
	"sagar@speakeasy.com",
	"brian@speakeasy.com",
	"daniel@speakeasy.com",
	"thomas@speakeasy.com",
}

func main() {
	var (
		projectID = flag.String("project-id", "", "Target project UUID. Skips org-slug lookup.")
		orgSlug   = flag.String("org-slug", "", "Org slug; uses first project of that org. Ignored if -project-id is set.")
		burstSize = flag.Int("burst-size", 5, "Events per burst.")
		bursts    = flag.Int("bursts", 3, "Number of bursts to emit. Use 0 for infinite.")
		interval  = flag.Duration("interval", 2*time.Second, "Wait between bursts.")
		blockRate = flag.Float64("block-rate", 0.1, "Fraction of events (0-1) marked as blocked.")
	)
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if *projectID == "" && *orgSlug == "" {
		log.Fatal("provide -project-id or -org-slug")
	}

	resolvedProjectID := *projectID
	if resolvedProjectID == "" {
		pgURL := strings.TrimSpace(os.Getenv("GRAM_DATABASE_URL"))
		if pgURL == "" {
			log.Fatal("GRAM_DATABASE_URL is not set; either export it or pass -project-id directly")
		}
		pgCfg, err := pgxpool.ParseConfig(pgURL)
		if err != nil {
			log.Fatalf("parsing GRAM_DATABASE_URL: %v", err)
		}
		pgPool, err := pgxpool.NewWithConfig(ctx, pgCfg)
		if err != nil {
			log.Fatalf("connecting to postgres: %v", err)
		}
		defer pgPool.Close()

		row := pgPool.QueryRow(ctx, `
			SELECT p.id FROM projects p
			JOIN organization_metadata o ON o.id = p.organization_id
			WHERE o.slug = $1 AND p.deleted IS FALSE
			ORDER BY p.created_at ASC
			LIMIT 1
		`, *orgSlug)
		var pid uuid.UUID
		if err := row.Scan(&pid); err != nil {
			log.Fatalf("looking up project for org %q: %v", *orgSlug, err)
		}
		resolvedProjectID = pid.String()
		log.Printf("resolved org %q → project %s", *orgSlug, resolvedProjectID)
	}

	chConn, err := openClickHouse(ctx)
	if err != nil {
		log.Fatalf("connecting to clickhouse: %v", err)
	}
	defer func() { _ = chConn.Close() }()

	rng := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0xC0FFEE)) //nolint:gosec // demo seed

	_ = burstSize // size is uniformly sampled in [1,3] below regardless of flag
	burstIdx := 0
	for {
		// Always 1-3 events per burst (uniform).
		size := 1 + rng.IntN(3)

		inserted, err := emitBurst(ctx, chConn, rng, resolvedProjectID, size, *blockRate)
		if err != nil {
			log.Fatalf("burst %d: %v", burstIdx+1, err)
		}
		burstIdx++
		log.Printf("burst %d: inserted %d events", burstIdx, inserted)

		if *bursts > 0 && burstIdx >= *bursts {
			return
		}
		// Wildly varied cadence so it doesn't read as a metronome:
		//   - 25% chance of a back-to-back rapid burst (30-150ms)
		//   - 5% chance of a longer lull (2x-3x the configured interval)
		//   - otherwise a normal wait spread 20%-130% of the interval
		var wait time.Duration
		switch {
		case rng.IntN(4) == 0:
			wait = time.Duration(30+rng.IntN(120)) * time.Millisecond
		case rng.IntN(20) == 0:
			wait = time.Duration(float64(*interval) * (2.0 + rng.Float64()))
		default:
			wait = time.Duration(float64(*interval) * (0.2 + rng.Float64()*1.1))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func openClickHouse(ctx context.Context) (clickhouse.Conn, error) {
	host := envOr("CLICKHOUSE_HOST", "127.0.0.1")
	port := envOr("CLICKHOUSE_NATIVE_PORT", "9440")
	db := envOr("CLICKHOUSE_DATABASE", "default")
	user := envOr("CLICKHOUSE_USERNAME", "gram")
	pass := envOr("CLICKHOUSE_PASSWORD", "gram")
	insecure := envOr("CLICKHOUSE_INSECURE", "true") == "true"

	opts := &clickhouse.Options{
		Protocol: clickhouse.Native,
		Addr:     []string{fmt.Sprintf("%s:%s", host, port)},
		Auth: clickhouse.Auth{
			Database: db,
			Username: user,
			Password: pass,
		},
		TLS: &tls.Config{
			InsecureSkipVerify: insecure, //nolint:gosec // local dev only
		},
	}
	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("opening clickhouse: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		return nil, fmt.Errorf("pinging clickhouse: %w", err)
	}
	return conn, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func emitBurst(ctx context.Context, conn clickhouse.Conn, rng *rand.Rand, projectID string, burstSize int, blockRate float64) (int, error) {
	batch, err := conn.PrepareBatch(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_urn, service_name, service_version, gram_chat_id
		)
	`)
	if err != nil {
		return 0, fmt.Errorf("preparing batch: %w", err)
	}

	now := time.Now()
	for range burstSize {
		src := pickSource(rng)
		toolName := src.tools[rng.IntN(len(src.tools))]
		eventName := src.events[rng.IntN(len(src.events))]
		userEmail := demoUsers[rng.IntN(len(demoUsers))]
		blocked := rng.Float64() < blockRate

		// Spread events across a 0-1500ms window with random offsets so
		// timestamps cluster organically rather than landing on a 50ms grid.
		offset := time.Duration(rng.IntN(1500)) * time.Millisecond
		ts := now.Add(offset).UnixNano()
		traceID := strings.ReplaceAll(uuid.New().String(), "-", "")
		spanID := strings.ReplaceAll(uuid.New().String(), "-", "")[:16]
		severity := "INFO"
		chatID := uuid.New().String()

		attrs := map[string]any{
			"gram.event.source":      "hook",
			"gram.tool.name":         toolName,
			"gram.hook.source":       src.hookSource,
			"gram.hook.event_name":   eventName,
			"gram.project.id":        projectID,
			"user.email":             userEmail,
			"gen_ai.conversation.id": chatID,
		}
		if blocked {
			attrs["gram.hook.block_reason"] = "policy_denied_demo"
		}
		attrsJSON, err := json.Marshal(attrs)
		if err != nil {
			return 0, fmt.Errorf("marshaling attributes: %w", err)
		}

		resAttrs, err := json.Marshal(map[string]any{
			"service.name":    "gram-hooks",
			"service.version": src.serviceVersion,
		})
		if err != nil {
			return 0, fmt.Errorf("marshaling resource attributes: %w", err)
		}

		pid, err := uuid.Parse(projectID)
		if err != nil {
			return 0, fmt.Errorf("invalid project-id %q: %w", projectID, err)
		}

		if err := batch.Append(
			uuid.New(),
			ts,
			ts,
			&severity,
			fmt.Sprintf("%s hook %s for tool %s", src.serviceVersion, eventName, toolName),
			&traceID,
			&spanID,
			string(attrsJSON),
			string(resAttrs),
			pid,
			fmt.Sprintf("local:tool:%s", toolName),
			"gram-hooks",
			&src.serviceVersion,
			&chatID,
		); err != nil {
			return 0, fmt.Errorf("appending row: %w", err)
		}
	}

	if err := batch.Send(); err != nil {
		return 0, fmt.Errorf("sending batch: %w", err)
	}
	return burstSize, nil
}
