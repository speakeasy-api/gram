// Package agentcapture drives a local-dev capture of agent provider
// telemetry. It polls a provider's admin APIs through the same ingest paths
// production uses — Admin Analytics reports into the ClickHouse
// telemetry_logs bronze table, and (when an external org ID is supplied)
// Compliance API chat transcripts into Postgres chats/chat_messages — and
// then dumps the captured window as an anonymized NDJSON fixture, so
// telemetry features can be developed against realistic data without live
// credentials.
//
// The capture is one leg of local test-data generation: interactive or
// scripted agent sessions (mise hooks:test / hooks:e2e) stream OTel and hook
// events into telemetry_logs live, while this package backfills the
// provider-settled reports and transcripts that production pollers fetch on
// Temporal schedules. The dump covers all of it.
package agentcapture

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// AgentClaude captures Anthropic Admin Analytics reports (Claude usage and
// cost).
const AgentClaude = "claude"

type Options struct {
	// Agent selects the provider integration to poll. Supported: claude.
	Agent string

	// APIKey authenticates against the provider's admin API. Empty skips the
	// poll phase so only rows already in ClickHouse (e.g. from agent
	// sessions) are dumped.
	APIKey string

	// ExternalOrgID is the provider-side organization ID (for claude: the
	// Anthropic organization UUID from the Console). The Admin Analytics
	// reports work without it — their org-scoped admin key already implies
	// the organization — but the Compliance API transcript import requires
	// it, so setting it is what enables that leg. When set it is also
	// stamped on captured rows as gram.external_org_id, matching production
	// configs.
	ExternalOrgID string

	// ProjectSlug names the local project that polled rows are attributed to
	// and that the dump is scoped to.
	ProjectSlug string

	// OutDir receives the NDJSON dump, manifest, and anonymization salt.
	OutDir string

	// Lookback bounds the poll and dump window, ending now.
	Lookback time.Duration

	// Anonymize pseudonymizes provider identities and scrubs free-text
	// content in the dump. It does not alter what lands in ClickHouse.
	Anonymize bool

	// Dump controls the export phase. The capture:agent-telemetry wrapper
	// disables it on the poll leg it runs in parallel with an agent session,
	// then runs a final dump-only pass once the session ends so the export
	// includes the session's rows.
	Dump bool
}

type Service struct {
	logger          *slog.Logger
	db              *pgxpool.Pool
	ch              clickhouse.Conn
	store           *aiintegrations.Store
	guardianPolicy  *guardian.Policy
	telemetryLogger *telemetry.Logger
	enc             *encryption.Client
	written         *writeCounter
}

// NewService wires the capture service and registers its row counter as a
// telemetry log observer, so call it before any rows flow through
// telemetryLogger.
func NewService(
	logger *slog.Logger,
	db *pgxpool.Pool,
	ch clickhouse.Conn,
	store *aiintegrations.Store,
	guardianPolicy *guardian.Policy,
	telemetryLogger *telemetry.Logger,
	enc *encryption.Client,
) *Service {
	written := &writeCounter{mu: sync.Mutex{}, total: 0}
	telemetryLogger.AddObserver(written)
	return &Service{
		logger:          logger.With(attr.SlogComponent("agentcapture")),
		db:              db,
		ch:              ch,
		store:           store,
		guardianPolicy:  guardianPolicy,
		telemetryLogger: telemetryLogger,
		enc:             enc,
		written:         written,
	}
}

// Run executes the capture: poll the provider's admin APIs for the lookback
// window (when an API key is supplied), then dump the project's
// telemetry_logs rows for that window into the output directory. A poll
// failure does not stop the dump — whatever landed is still dumped and the
// poll error is returned alongside.
func (s *Service) Run(ctx context.Context, opts Options) error {
	if opts.Agent != AgentClaude {
		return fmt.Errorf("unsupported agent %q: supported agents: %s", opts.Agent, AgentClaude)
	}
	if opts.Lookback <= 0 {
		return fmt.Errorf("lookback must be positive, got %s", opts.Lookback)
	}
	if !opts.Dump && opts.APIKey == "" {
		return fmt.Errorf("nothing to do: dump disabled and no api key supplied for polling")
	}

	project, err := projectsrepo.New(s.db).GetProjectBySlugAcrossOrgs(ctx, opts.ProjectSlug)
	if err != nil {
		return fmt.Errorf("resolve project %q: %w", opts.ProjectSlug, err)
	}

	until := time.Now().UTC()
	since := until.Add(-opts.Lookback)
	s.logger.InfoContext(ctx, "capturing agent telemetry",
		attr.SlogProjectSlug(project.Slug),
		attr.SlogProjectID(project.ID.String()),
		attr.SlogOrganizationID(project.OrganizationID),
	)

	var pollErr error
	if opts.APIKey == "" {
		s.logger.InfoContext(ctx, "no api key supplied: skipping provider poll phase")
	} else {
		pollErr = s.pollClaude(ctx, project.ID, project.OrganizationID, opts, since, until)
		// The Compliance API needs the explicit org UUID (the admin key
		// alone is not enough to scope its activities feed), so the
		// transcript leg runs only when the flag is supplied.
		if opts.ExternalOrgID == "" {
			s.logger.InfoContext(ctx, "no external org id supplied: skipping claude.ai transcript import (compliance api)")
		} else if err := s.pollClaudeCompliance(ctx, project, opts, since); err != nil {
			s.logger.ErrorContext(ctx, "compliance transcript import failed", attr.SlogError(err))
			pollErr = errors.Join(pollErr, fmt.Errorf("sync anthropic_compliance: %w", err))
		}
	}

	if !opts.Dump {
		return pollErr
	}
	if err := s.dump(ctx, project, since, until, opts); err != nil {
		return errors.Join(pollErr, fmt.Errorf("dump telemetry logs: %w", err))
	}
	return pollErr
}

// writeCounter counts rows submitted to the telemetry logger during the poll
// phase. Observers see batches before per-org feature filtering, but the
// capture command hard-wires those checks to enabled, so submitted equals
// written in practice.
type writeCounter struct {
	mu    sync.Mutex
	total int
}

var _ telemetry.LogObserver = (*writeCounter)(nil)

func (w *writeCounter) OnTelemetryLogsWritten(_ context.Context, params []telemetry.LogParams) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.total += len(params)
}

func (w *writeCounter) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.total
}
