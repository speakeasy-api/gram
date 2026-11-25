package cimdcandidates

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/aivendors/cimdcandidates/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

const (
	// BackfillSeedName identifies the one-time startup backfill.
	BackfillSeedName = "cimd-client-candidates"

	// BackfillSeedVersion changes only when the backfill semantics change.
	BackfillSeedVersion = "2026-11-24-v1"

	// backfillIssuerPage is how many issuers one page reads. Each page costs
	// one issuer read and one indexed client read, so a few hundred keeps
	// both queries small.
	backfillIssuerPage = 500
)

// BackfillCounts reports what one backfill run did.
type BackfillCounts struct {
	// Scanned is the number of live CIMD client rows read on open issuers.
	Scanned int64

	// Recorded is the number of sightings written, new or bumped.
	Recorded int64
}

// Backfill records candidates and sightings for the CIMD client rows open-mode
// issuers already hold, judged against catalog by the same rule as sign-in.
// It only upserts, keeping existing decisions and the latest last_seen_at, so
// it is safe to repeat and to run beside live recording.
func Backfill(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, catalog *aivendors.Snapshot) (BackfillCounts, error) {
	counts := BackfillCounts{Scanned: 0, Recorded: 0}
	q := repo.New(db)
	after := uuid.Nil
	for {
		issuers, err := q.ListIssuersForBackfill(ctx, repo.ListIssuersForBackfillParams{AfterID: after, PageSize: backfillIssuerPage})
		if err != nil {
			return counts, fmt.Errorf("list issuers for cimd candidate backfill: %w", err)
		}
		if len(issuers) == 0 {
			break
		}
		after = issuers[len(issuers)-1].ID

		open := make([]uuid.UUID, 0, len(issuers))
		for _, iss := range issuers {
			if mode, _ := admission.ResolveMode(iss.ClientIDMetadataAdmissionMode.String, iss.ClientIDMetadataAdmissionMode.Valid); mode == admission.ModeOpen {
				open = append(open, iss.ID)
			}
		}
		if len(open) == 0 {
			continue
		}

		clients, err := q.ListCIMDClientsForBackfill(ctx, open)
		if err != nil {
			return counts, fmt.Errorf("list cimd clients for candidate backfill: %w", err)
		}
		for _, c := range clients {
			counts.Scanned++
			if !Recordable(catalog, admission.ModeOpen, c.Url) {
				continue
			}
			n, err := q.RecordSighting(ctx, repo.RecordSightingParams{
				Url:                 c.Url,
				ClientName:          conv.ToPGTextEmpty(c.ClientName),
				FirstSeenAt:         c.CreatedAt,
				LastSeenAt:          c.LastSeenAt,
				UserSessionIssuerID: c.UserSessionIssuerID,
				MaxPendingPerIssuer: MaxPendingPerIssuer,
			})
			if err != nil {
				return counts, fmt.Errorf("record cimd candidate backfill sighting: %w", err)
			}
			counts.Recorded += n
		}
	}
	logger.InfoContext(ctx, "cimd client candidate backfill applied",
		attr.SlogCIMDCandidatesScanned(counts.Scanned),
		attr.SlogCIMDCandidatesRecorded(counts.Recorded),
	)
	return counts, nil
}
