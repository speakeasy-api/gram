package demoseed

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
)

// seeder holds what every seed phase needs: the tenant, the transaction's
// queries, and the run's clock. Every id, user, and email the seed writes is
// built from spec through the helpers below, so one body of Go serves the
// shared demo org, the local development org, and the safety test's
// adversarial tenant.
type seeder struct {
	spec   Spec
	q      *repo.Queries
	logger *slog.Logger
	// now is the Postgres transaction's now(), read once at the start of the
	// run, so Go-computed timestamps agree with any now() a query still uses
	// and ClickHouse rows line up with the Postgres ones.
	now time.Time
}

// orgID is the tenant's organization id.
func (s *seeder) orgID() string { return s.spec.OrgID }

// projectID is the tenant's single project.
func (s *seeder) projectID() uuid.UUID { return s.fixed("0000-4000-a000-000000000001") }

// fixed is one of the seed's fixed uuids: the tenant's prefix plus the given
// last four groups, e.g. s.fixed("0000-4000-a000-00000000f001").
func (s *seeder) fixed(rest string) uuid.UUID { return uuid.MustParse(s.spec.FixedUUID(rest)) }

// det is the deterministic uuid for a name, namespaced by the tenant:
// s.det("chat-1") replaces the old demo.det_uuid('gram-demo-chat-1').
func (s *seeder) det(name string) uuid.UUID { return detUUID(s.name(name)) }

// name namespaces a deterministic name (trace ids, hash inputs) by tenant:
// s.name("tooltrace-1-2") replaces the old 'gram-demo-tooltrace-1-2'.
func (s *seeder) name(name string) string { return s.spec.NameSeed + name }

// userID is a seeded user's id: s.userID("amara") is "user_demo_amara".
func (s *seeder) userID(handle string) string { return s.spec.UserPrefix + handle }

// email is a seeded user's email: s.email("amara") is "amara@demo.getgram.ai".
func (s *seeder) email(handle string) string { return handle + s.spec.EmailDomain }

// ago is the timestamp d before the transaction's now().
func (s *seeder) ago(d time.Duration) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: s.now.Add(-d), InfinityModifier: pgtype.Finite, Valid: true}
}

// agoOrNull is ago, except that a zero d is NULL: optional lifecycle stamps
// such as suspended_at and deleted_at.
func (s *seeder) agoOrNull(d time.Duration) pgtype.Timestamptz {
	if d == 0 {
		return pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	}
	return s.ago(d)
}

// phase is one step of the Postgres seed, run in order inside one
// transaction. Each statement gets its own statement_timeout, so no single
// call has to fit the whole seed inside one timer.
type phase struct {
	name string
	run  func(context.Context) error
}

func (s *seeder) runPostgres(ctx context.Context) error {
	// The pool caps each statement at 60s (newDBClient). The purge's cascading
	// deletes run against prod-sized tables, so give every statement of this
	// transaction more room while still failing a wedged run.
	if err := s.q.SetSeedStatementTimeout(ctx); err != nil {
		return fmt.Errorf("raise statement timeout: %w", err)
	}

	now, err := s.q.Now(ctx)
	if err != nil {
		return fmt.Errorf("read transaction time: %w", err)
	}
	s.now = now.Time

	phases := []phase{
		{"preflight and org", s.seedOrg},
		{"roles and directory", s.seedRoles},
		{"deployment and connections", s.seedDeployment},
		{"mcp servers", s.seedServers},
		{"prompts and skills", s.seedSkills},
		{"risk and chats", s.seedRiskAndChats},
		{"activity and billing", s.seedActivity},
		{"postflight", s.postflight},
	}
	for _, p := range phases {
		start := time.Now()
		if err := p.run(ctx); err != nil {
			s.logger.ErrorContext(ctx, fmt.Sprintf("demo seed phase %q failed after %s", p.name, time.Since(start).Round(time.Millisecond)), attr.SlogName(p.name), attr.SlogError(err))
			return fmt.Errorf("demo seed phase %q: %w", p.name, err)
		}
		s.logger.InfoContext(ctx, fmt.Sprintf("demo seed phase %q done in %s", p.name, time.Since(start).Round(time.Millisecond)), attr.SlogName(p.name))
	}
	return nil
}
