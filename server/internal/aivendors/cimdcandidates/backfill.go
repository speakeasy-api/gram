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
//
// Rows an issuer at its cap cannot use to create a URL are held back and,
// after every page has run, attached to whichever of those URLs another
// issuer created, so one run converges and a second run changes nothing.
func Backfill(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, catalog *aivendors.Snapshot, assistantOrigin *url.URL) (BackfillCounts, error) {
	return backfill(ctx, logger, db, catalog, assistantOrigin, backfillIssuerPage, backfillClientPage)
}

// backfill is Backfill with its page sizes as parameters, so tests can cross
// page boundaries with a handful of rows.
func backfill(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, catalog *aivendors.Snapshot, assistantOrigin *url.URL, issuerPage, clientPage int32) (BackfillCounts, error) {
	counts := BackfillCounts{Scanned: 0, Recorded: 0, Skipped: 0}
	compiled := aivendors.CompiledSnapshot()
	q := repo.New(db)
	var deferred []repo.ListCIMDClientsForBackfillRow
	after := uuid.Nil
	for {
		issuers, err := q.ListIssuersForBackfill(ctx, repo.ListIssuersForBackfillParams{AfterID: after, PageSize: issuerPage})
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
				PageSize:             clientPage,
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

			var uncovered []repo.ListCIMDClientsForBackfillRow
			for _, c := range clients {
				if Recordable(catalog, admission.ModeOpen, assistantOrigin, c.Url) && Recordable(compiled, admission.ModeOpen, assistantOrigin, c.Url) {
					uncovered = append(uncovered, c)
				}
			}
			include, held, err := planBatch(ctx, q, uncovered, budget)
			if err != nil {
				return counts, err
			}
			deferred = append(deferred, held...)
			n, err := writeBatch(ctx, q, include)
			if err != nil {
				return counts, err
			}
			counts.Recorded += n
			if len(clients) < int(clientPage) {
				break
			}
		}
		logger.InfoContext(ctx, "cimd client candidate backfill progress",
			attr.SlogCIMDCandidatesScanned(counts.Scanned),
			attr.SlogCIMDCandidatesRecorded(counts.Recorded),
			attr.SlogCIMDCandidatesSkipped(int64(len(deferred))),
		)
	}

	for start := 0; start < len(deferred); start += int(clientPage) {
		chunk := deferred[start:min(start+int(clientPage), len(deferred))]
		existing, err := existingCandidates(ctx, q, chunk)
		if err != nil {
			return counts, err
		}
		var attach []repo.ListCIMDClientsForBackfillRow
		for _, c := range chunk {
			if _, ok := existing[c.Url]; ok {
				attach = append(attach, c)
			} else {
				counts.Skipped++
			}
		}
		n, err := writeBatch(ctx, q, attach)
		if err != nil {
			return counts, err
		}
		counts.Recorded += n
	}

	logger.InfoContext(ctx, "cimd client candidate backfill applied",
		attr.SlogCIMDCandidatesScanned(counts.Scanned),
		attr.SlogCIMDCandidatesRecorded(counts.Recorded),
		attr.SlogCIMDCandidatesSkipped(counts.Skipped),
	)
	return counts, nil
}

type sightingKey struct {
	url      string
	issuerID uuid.UUID
}

// planBatch splits uncovered rows into those to write now and those held back
// because their issuer is out of budget for a URL that is not a candidate yet.
//
// Every new pending sighting spends one unit of its issuer's budget, whether
// it creates the URL or joins a pending candidate, matching the pending count
// the live cap reads. Joins to URLs that already exist go first, so an issuer
// cannot join its way past the cap and then still create a URL. A join to an
// existing URL is never held back.
func planBatch(ctx context.Context, q *repo.Queries, rows []repo.ListCIMDClientsForBackfillRow, budget map[uuid.UUID]int64) (include, held []repo.ListCIMDClientsForBackfillRow, err error) {
	if len(rows) == 0 {
		return nil, nil, nil
	}
	status, err := existingCandidates(ctx, q, rows)
	if err != nil {
		return nil, nil, err
	}
	urls := make([]string, 0, len(rows))
	issuers := make([]uuid.UUID, 0, len(rows))
	for _, c := range rows {
		urls = append(urls, c.Url)
		issuers = append(issuers, c.UserSessionIssuerID)
	}
	sightingRows, err := q.ListSightingsForBackfill(ctx, repo.ListSightingsForBackfillParams{Urls: urls, UserSessionIssuerIds: issuers})
	if err != nil {
		return nil, nil, fmt.Errorf("list cimd candidate sightings: %w", err)
	}
	sighted := make(map[sightingKey]bool, len(sightingRows))
	for _, s := range sightingRows {
		sighted[sightingKey{url: s.Url, issuerID: s.UserSessionIssuerID}] = true
	}

	join := func(c repo.ListCIMDClientsForBackfillRow) {
		key := sightingKey{url: c.Url, issuerID: c.UserSessionIssuerID}
		if status[c.Url] == string(StatusPending) && !sighted[key] {
			budget[c.UserSessionIssuerID]--
		}
		sighted[key] = true
		include = append(include, c)
	}

	done := make([]bool, len(rows))
	for i, c := range rows {
		if _, ok := status[c.Url]; ok {
			join(c)
			done[i] = true
		}
	}
	for i, c := range rows {
		if _, ok := status[c.Url]; !done[i] && !ok && budget[c.UserSessionIssuerID] > 0 {
			status[c.Url] = string(StatusPending)
			join(c)
			done[i] = true
		}
	}
	for i, c := range rows {
		if done[i] {
			continue
		}
		if _, ok := status[c.Url]; ok {
			join(c)
		} else {
			held = append(held, c)
		}
	}
	return include, held, nil
}

// existingCandidates maps the rows' URLs that are candidates to their status.
func existingCandidates(ctx context.Context, q *repo.Queries, rows []repo.ListCIMDClientsForBackfillRow) (map[string]string, error) {
	urls := make([]string, 0, len(rows))
	for _, c := range rows {
		urls = append(urls, c.Url)
	}
	found, err := q.ListExistingCandidates(ctx, urls)
	if err != nil {
		return nil, fmt.Errorf("list existing cimd candidates: %w", err)
	}
	status := make(map[string]string, len(found))
	for _, f := range found {
		status[f.Url] = f.Status
	}
	return status, nil
}

func writeBatch(ctx context.Context, q *repo.Queries, rows []repo.ListCIMDClientsForBackfillRow) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	batch := repo.BackfillSightingsParams{Urls: nil, ClientNames: nil, FirstSeenAts: nil, LastSeenAts: nil, UserSessionIssuerIds: nil}
	for _, c := range rows {
		batch.Urls = append(batch.Urls, c.Url)
		batch.ClientNames = append(batch.ClientNames, c.ClientName)
		batch.FirstSeenAts = append(batch.FirstSeenAts, c.CreatedAt)
		batch.LastSeenAts = append(batch.LastSeenAts, c.LastSeenAt)
		batch.UserSessionIssuerIds = append(batch.UserSessionIssuerIds, c.UserSessionIssuerID)
	}
	n, err := q.BackfillSightings(ctx, batch)
	if err != nil {
		return 0, fmt.Errorf("write cimd candidate backfill batch: %w", err)
	}
	return n, nil
}
