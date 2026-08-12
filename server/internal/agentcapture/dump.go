package agentcapture

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// logRow is one dumped telemetry_logs row. It carries the table's physical
// columns (materialized columns are recomputed by ClickHouse when a fixture
// is re-inserted) plus event_urn, which is derived but included so consumers
// can route rows without re-parsing attributes; drop it when re-inserting.
type logRow struct {
	ID                   string          `json:"id"`
	TimeUnixNano         int64           `json:"time_unix_nano"`
	ObservedTimeUnixNano int64           `json:"observed_time_unix_nano"`
	SeverityText         *string         `json:"severity_text"`
	Body                 string          `json:"body"`
	TraceID              *string         `json:"trace_id"`
	SpanID               *string         `json:"span_id"`
	Attributes           json.RawMessage `json:"attributes"`
	ResourceAttributes   json.RawMessage `json:"resource_attributes"`
	GramProjectID        string          `json:"gram_project_id"`
	GramDeploymentID     *string         `json:"gram_deployment_id"`
	GramFunctionID       *string         `json:"gram_function_id"`
	GramURN              string          `json:"gram_urn"`
	ServiceName          string          `json:"service_name"`
	ServiceVersion       *string         `json:"service_version"`
	GramChatID           *string         `json:"gram_chat_id"`
	EventURN             string          `json:"event_urn"`
}

const dumpQuery = `
SELECT
    toString(id) AS id,
    time_unix_nano,
    observed_time_unix_nano,
    severity_text,
    body,
    toString(trace_id) AS trace_id,
    toString(span_id) AS span_id,
    toJSONString(attributes) AS attributes,
    toJSONString(resource_attributes) AS resource_attributes,
    -- Aliased away from the column name: ClickHouse resolves identifiers in
    -- WHERE to SELECT aliases first, and a same-named String alias would
    -- shadow the UUID column in the project filter.
    toString(gram_project_id) AS project_id_str,
    toString(gram_deployment_id) AS gram_deployment_id,
    toString(gram_function_id) AS gram_function_id,
    gram_urn,
    service_name,
    service_version,
    gram_chat_id,
    event_urn
FROM telemetry_logs
WHERE gram_project_id = toUUID(?)
  AND time_unix_nano >= ?
  AND time_unix_nano < ?
ORDER BY time_unix_nano, id
`

// manifest describes one capture dump so consumers know the window, scope,
// and file layout without inspecting rows.
type manifest struct {
	GeneratedAt    time.Time      `json:"generated_at"`
	ProjectID      string         `json:"project_id"`
	ProjectSlug    string         `json:"project_slug"`
	OrganizationID string         `json:"organization_id"`
	WindowSince    time.Time      `json:"window_since"`
	WindowUntil    time.Time      `json:"window_until"`
	Anonymized     bool           `json:"anonymized"`
	RowsTotal      int            `json:"rows_total"`
	RowsByEventURN map[string]int `json:"rows_by_event_urn"`
	Files          map[string]int `json:"files"`
}

// dump exports the project's telemetry_logs rows for the capture window as
// NDJSON, one file per event origin (provider_api, provider_otel, agent_hook,
// gram_service, unknown), with a manifest.json describing the capture.
func (s *Service) dump(ctx context.Context, project projectsrepo.Project, since, until time.Time, opts Options) error {
	// The telemetry logger writes with async_insert=1 / wait_for_async_insert=0,
	// so rows from the poll phase may still sit in ClickHouse's async insert
	// buffer. Flush it so the dump sees everything; failure is non-fatal
	// because the default buffer timeout drains within about a second anyway.
	if err := s.ch.Exec(ctx, "SYSTEM FLUSH ASYNC INSERT QUEUE"); err != nil {
		s.logger.WarnContext(ctx, "failed to flush clickhouse async insert queue", attr.SlogError(err))
	}

	logsDir := filepath.Join(opts.OutDir, "logs")
	if err := os.MkdirAll(logsDir, 0o750); err != nil {
		return fmt.Errorf("create dump directory: %w", err)
	}
	// Each dump fully replaces the export: leftover files from a previous
	// capture whose origins do not occur in this window would otherwise
	// linger next to a manifest that no longer lists them.
	stale, err := filepath.Glob(filepath.Join(logsDir, "*.ndjson"))
	if err != nil {
		return fmt.Errorf("list previous dump files: %w", err)
	}
	for _, path := range stale {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove previous dump file: %w", err)
		}
	}

	var anonymizer *Anonymizer
	if opts.Anonymize {
		salt, err := loadOrCreateSalt(filepath.Join(opts.OutDir, "anonymization-salt"))
		if err != nil {
			return err
		}
		anonymizer = NewAnonymizer(salt)
	}

	rows, err := s.ch.Query(ctx, dumpQuery, project.ID.String(), since.UnixNano(), until.UnixNano())
	if err != nil {
		return fmt.Errorf("query telemetry logs: %w", err)
	}
	defer o11y.NoLogDefer(rows.Close)

	writers := map[string]*dumpWriter{}
	defer func() {
		for _, w := range writers {
			o11y.NoLogDefer(w.file.Close)
		}
	}()

	total := 0
	byURN := map[string]int{}
	for rows.Next() {
		var (
			row              logRow
			attrsJSON        string
			resourceAttrsRaw string
		)
		if err := rows.Scan(
			&row.ID,
			&row.TimeUnixNano,
			&row.ObservedTimeUnixNano,
			&row.SeverityText,
			&row.Body,
			&row.TraceID,
			&row.SpanID,
			&attrsJSON,
			&resourceAttrsRaw,
			&row.GramProjectID,
			&row.GramDeploymentID,
			&row.GramFunctionID,
			&row.GramURN,
			&row.ServiceName,
			&row.ServiceVersion,
			&row.GramChatID,
			&row.EventURN,
		); err != nil {
			return fmt.Errorf("scan telemetry log row: %w", err)
		}
		row.Attributes = json.RawMessage(attrsJSON)
		row.ResourceAttributes = json.RawMessage(resourceAttrsRaw)

		if anonymizer != nil {
			if err := anonymizer.ScrubRow(&row); err != nil {
				return fmt.Errorf("anonymize telemetry log row %s: %w", row.ID, err)
			}
		}

		writer, err := writerFor(writers, logsDir, originFromEventURN(row.EventURN))
		if err != nil {
			return err
		}
		line, err := json.Marshal(row)
		if err != nil {
			return fmt.Errorf("serialize telemetry log row %s: %w", row.ID, err)
		}
		if _, err := writer.buf.Write(append(line, '\n')); err != nil {
			return fmt.Errorf("write dump row: %w", err)
		}
		writer.rows++
		total++
		byURN[row.EventURN]++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate telemetry log rows: %w", err)
	}

	files := map[string]int{}
	for origin, w := range writers {
		if err := w.buf.Flush(); err != nil {
			return fmt.Errorf("flush dump file for %s: %w", origin, err)
		}
		files[filepath.Join("logs", origin+".ndjson")] = w.rows
	}

	m := manifest{
		GeneratedAt:    time.Now().UTC(),
		ProjectID:      project.ID.String(),
		ProjectSlug:    project.Slug,
		OrganizationID: project.OrganizationID,
		WindowSince:    since,
		WindowUntil:    until,
		Anonymized:     opts.Anonymize,
		RowsTotal:      total,
		RowsByEventURN: byURN,
		Files:          files,
	}
	encoded, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("serialize dump manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(opts.OutDir, "manifest.json"), append(encoded, '\n'), 0o600); err != nil {
		return fmt.Errorf("write dump manifest: %w", err)
	}

	s.logger.InfoContext(ctx, "telemetry dump complete",
		attr.SlogTelemetryCHRowCount(total),
		attr.SlogFilePath(opts.OutDir),
	)
	return nil
}

type dumpWriter struct {
	file *os.File
	buf  *bufio.Writer
	rows int
}

func writerFor(writers map[string]*dumpWriter, dir string, origin string) (*dumpWriter, error) {
	if w, ok := writers[origin]; ok {
		return w, nil
	}
	// #nosec G304 -- dump files are created inside the operator-chosen output
	// directory and origin is validated against safeOriginPattern.
	file, err := os.OpenFile(filepath.Join(dir, origin+".ndjson"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create dump file for %s: %w", origin, err)
	}
	w := &dumpWriter{file: file, buf: bufio.NewWriter(file), rows: 0}
	writers[origin] = w
	return w, nil
}

var safeOriginPattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

// originFromEventURN extracts the observation channel from a canonical event
// URN (urn:telemetry:<origin>:<kind>:<type>). Rows written before URN
// stamping existed, and any malformed value, land in "unknown".
func originFromEventURN(eventURN string) string {
	parts := strings.Split(eventURN, ":")
	if len(parts) >= 5 && parts[0] == "urn" && parts[1] == "telemetry" && safeOriginPattern.MatchString(parts[2]) {
		return parts[2]
	}
	return "unknown"
}
