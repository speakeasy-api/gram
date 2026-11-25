package cimdcandidates

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/aivendors/cimdcandidates/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

const (
	// BackfillSeedName identifies the one-time startup backfill.
	BackfillSeedName = "cimd-client-candidates"

	// BackfillSeedVersion changes only when the backfill semantics change.
	BackfillSeedVersion = "2026-11-24-v1"

	// backfillIssuerPage is how many issuers one page reads.
	backfillIssuerPage = 500

	// backfillClientPage bounds the client rows one statement reads and one
	// upsert writes, so each statement stays small however many clients a
	// page of issuers holds.
	backfillClientPage = 1000
)

// BackfillCounts reports what one backfill run did.
type BackfillCounts struct {
	// Scanned is the number of live CIMD client rows read on issuers whose
	// mode admits uncovered clients.
	Scanned int64

	// Recorded is the number of sightings written, new or bumped.
	Recorded int64

	// Skipped is the number of uncovered rows not recorded because their
	// issuer is at its pending cap.
	Skipped int64
}

// Backfill records candidates and sightings for the CIMD client rows issuers
// already hold, by the same rule as sign-in. A URL counts as covered when the
// database catalog or the built-in one covers it, so a deploy whose catalog
// seed has not run yet does not record its new documents as pending.
// assistantOrigin is the deployment's outbound origin; nil matches assistant
// documents on any origin.
//
// It only upserts, keeping existing decisions, the earliest first sighting and
// the latest last sighting, so it is safe to repeat and to run beside live
// recording.
func Backfill(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, catalog *aivendors.Snapshot, assistantOrigin *url.URL) (BackfillCounts, error) {
	counts := BackfillCounts{Scanned: 0, Recorded: 0, Skipped: 0}
	compiled := aivendors.CompiledSnapshot()
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

		var ids []uuid.UUID
		for _, iss := range issuers {
			mode, _ := admission.ResolveMode(iss.ClientIDMetadataAdmissionMode.String, iss.ClientIDMetadataAdmissionMode.Valid)
			if admitsUncovered(mode) {
				ids = append(ids, iss.ID)
			}
		}
		if len(ids) == 0 {
			continue
		}

		pending, err := q.CountPendingSightingsByIssuer(ctx, ids)
		if err != nil {
			return counts, fmt.Errorf("count pending cimd candidate sightings: %w", err)
		}
		budget := make(map[uuid.UUID]int64, len(ids))
		for _, id := range ids {
			budget[id] = MaxPendingPerIssuer
		}
		for _, p := range pending {
			budget[p.UserSessionIssuerID] = MaxPendingPerIssuer - p.Pending
		}

		afterIssuer, afterClient := uuid.Nil, uuid.Nil
		for {
			clients, err := q.ListCIMDClientsForBackfill(ctx, repo.ListCIMDClientsForBackfillParams{
				UserSessionIssuerIds: ids,
				AfterIssuerID:        afterIssuer,
				AfterID:              afterClient,
				PageSize:             backfillClientPage,
			})
			if err != nil {
				return counts, fmt.Errorf("list cimd clients for candidate backfill: %w", err)
			}
			if len(clients) == 0 {
				break
			}
			last := clients[len(clients)-1]
			afterIssuer, afterClient = last.UserSessionIssuerID, last.ID
			counts.Scanned += int64(len(clients))

			batch, skipped, err := backfillBatch(ctx, q, clients, budget, catalog, compiled, assistantOrigin)
			if err != nil {
				return counts, err
			}
			counts.Skipped += skipped
			if len(batch.Urls) > 0 {
				n, err := q.BackfillSightings(ctx, batch)
				if err != nil {
					return counts, fmt.Errorf("write cimd candidate backfill batch: %w", err)
				}
				counts.Recorded += n
			}
			if len(clients) < backfillClientPage {
				break
			}
		}
		logger.InfoContext(ctx, "cimd client candidate backfill progress",
			attr.SlogCIMDCandidatesScanned(counts.Scanned),
			attr.SlogCIMDCandidatesRecorded(counts.Recorded),
			attr.SlogCIMDCandidatesSkipped(counts.Skipped),
		)
	}
	logger.InfoContext(ctx, "cimd client candidate backfill applied",
		attr.SlogCIMDCandidatesScanned(counts.Scanned),
		attr.SlogCIMDCandidatesRecorded(counts.Recorded),
		attr.SlogCIMDCandidatesSkipped(counts.Skipped),
	)
	return counts, nil
}

// backfillBatch keeps the uncovered rows and applies each issuer's remaining
// budget to URLs that are not candidates yet, spending it in budget.
func backfillBatch(
	ctx context.Context,
	q *repo.Queries,
	clients []repo.ListCIMDClientsForBackfillRow,
	budget map[uuid.UUID]int64,
	catalog, compiled *aivendors.Snapshot,
	assistantOrigin *url.URL,
) (repo.BackfillSightingsParams, int64, error) {
	batch := repo.BackfillSightingsParams{Urls: nil, ClientNames: nil, FirstSeenAts: nil, LastSeenAts: nil, UserSessionIssuerIds: nil}
	var uncovered []repo.ListCIMDClientsForBackfillRow
	var urls []string
	for _, c := range clients {
		if Recordable(catalog, admission.ModeOpen, assistantOrigin, c.Url) && Recordable(compiled, admission.ModeOpen, assistantOrigin, c.Url) {
			uncovered = append(uncovered, c)
			urls = append(urls, c.Url)
		}
	}
	if len(uncovered) == 0 {
		return batch, 0, nil
	}

	existingURLs, err := q.ListExistingCandidateURLs(ctx, urls)
	if err != nil {
		return batch, 0, fmt.Errorf("list existing cimd candidates: %w", err)
	}
	existing := make(map[string]bool, len(existingURLs))
	for _, u := range existingURLs {
		existing[u] = true
	}

	// New URLs are claimed first by issuers with budget left, so an issuer at
	// its cap still sights a URL another issuer in this batch creates,
	// whatever the row order.
	include := make([]bool, len(uncovered))
	for i, c := range uncovered {
		if !existing[c.Url] && budget[c.UserSessionIssuerID] > 0 {
			budget[c.UserSessionIssuerID]--
			existing[c.Url] = true
			include[i] = true
		}
	}
	var skipped int64
	for i, c := range uncovered {
		if !include[i] && !existing[c.Url] {
			skipped++
			continue
		}
		batch.Urls = append(batch.Urls, c.Url)
		batch.ClientNames = append(batch.ClientNames, c.ClientName)
		batch.FirstSeenAts = append(batch.FirstSeenAts, c.CreatedAt)
		batch.LastSeenAts = append(batch.LastSeenAts, c.LastSeenAt)
		batch.UserSessionIssuerIds = append(batch.UserSessionIssuerIds, c.UserSessionIssuerID)
	}
	return batch, skipped, nil
}
