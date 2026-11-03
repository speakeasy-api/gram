package demoseed

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
)

// memorySummaries are the masked summaries memory reads, one per entry of
// chatTitles (same index), so a session's summary matches its title.
var memorySummaries = []string{
	"Traced the payment 401s to an expired signing key on the checkout service. Rotated the key and confirmed new charges authorize.",
	"Checked the order against the refund policy, issued a partial refund for the damaged item and logged the reason on the ticket.",
	"Watched the canary through its first hour. Error rate and p95 latency stayed inside the rollout guardrails, so the rollout continued.",
	"Webhook retries were failing on a receiver that rejects payloads over 1 MB. Trimmed the payload and replayed the failed deliveries.",
	"The /orders latency regression came from an N+1 query added in the last release. Batched the lookups and latency returned to baseline.",
	"Pulled the charge history for the dispute, matched it to the signed order and drafted the evidence response for review.",
	"Reviewed the migration plan. Asked for the new index to be built concurrently and for the column backfill to run in batches.",
	"Summarized the overnight escalation for the incoming on-call: what paged, what was tried, and the open follow-up on the queue alarms.",
	"Cache hit rate dropped after keys started including a request id. Removed the id from the key and the hit rate recovered.",
	"Raised the per-key rate limit for the reporting client after confirming its burst pattern, and left the global limit unchanged.",
}

// memoryTopics are the short phrases digests use for each chatTitles entry.
var memoryTopics = []string{
	"rotating the checkout signing key behind the payment 401s",
	"refunds handled against the damaged-item policy",
	"canary rollouts that stayed inside their guardrails",
	"replaying webhook deliveries after trimming oversized payloads",
	"fixing an N+1 query behind the /orders latency regression",
	"assembling evidence for billing disputes",
	"migration reviews asking for concurrent index builds",
	"on-call handoffs for the queue alarms",
	"restoring the cache hit rate by dropping request ids from keys",
	"tuning the reporting client's rate limit",
}

// memoryMemos are the facts agents saved on purpose. topic indexes
// chatTitles: each memo cites the newest remembered session on that topic.
var memoryMemos = []struct {
	topic   int
	content string
}{
	{0, "The checkout signing key expires every 90 days. Rotation steps live in the payments runbook."},
	{1, "Damaged-item refunds under $50 are approved without a photo."},
	{2, "Rollouts pause automatically when canary error rate passes 1% for five minutes."},
	{4, "The orders service must batch customer lookups; per-row lookups caused the last latency regression."},
	{6, "Index changes on large tables are built concurrently and backfills run in batches of 10k rows."},
	{8, "Cache keys must not include request ids or timestamps."},
}

// seedMemory fills Organizational Memory: most closed-day bulk sessions get a
// masked summary, one is excluded, agents saved a handful of memos, and the
// day, week and month digests are what the nightly rollup would have built.
func (s *seeder) seedMemory(ctx context.Context) error {
	org := s.orgID()
	proj := s.projectID()
	if err := s.q.MemoryEnableForOrganization(ctx, org); err != nil {
		return fmt.Errorf("enable organizational memory: %w", err)
	}
	today := s.now.UTC().Truncate(24 * time.Hour)
	ts := func(t time.Time) pgtype.Timestamptz {
		return pgtype.Timestamptz{Time: t, InfinityModifier: pgtype.Finite, Valid: true}
	}
	date := func(t time.Time) pgtype.Date {
		return pgtype.Date{Time: t, InfinityModifier: pgtype.Finite, Valid: true}
	}
	// A digest is written the night after its period closes, and never after
	// the run's own transaction time.
	writtenAt := func(periodEnd time.Time) time.Time {
		if at := periodEnd.Add(2 * time.Hour); at.Before(s.now) {
			return at
		}
		return s.now
	}

	// Remembered sessions, grouped by the UTC day they started.
	type remembered struct {
		id    uuid.UUID
		topic int
		at    time.Time
		owner demoUser
	}
	byDay := map[time.Time][]remembered{}
	for i := 1; i <= bulkChats; i++ {
		started := s.chatTS(i)
		// Today's sessions are still open; about a third of the older ones
		// were never summarized.
		if !started.Before(today) || hash64(s.chatName(i), "memory")%10 >= 7 {
			continue
		}
		topic := i % len(chatTitles)
		excluded := i%23 == 0
		if err := s.q.MemorySetChatSummary(ctx, repo.MemorySetChatSummaryParams{
			Summary:     memorySummaries[topic],
			GeneratedAt: ts(started.Add(45 * time.Minute)),
			Excluded:    excluded,
			ID:          s.chatID(i),
			ProjectID:   proj,
		}); err != nil {
			return fmt.Errorf("summarize demo chat %d: %w", i, err)
		}
		if excluded {
			continue
		}
		day := started.Truncate(24 * time.Hour)
		byDay[day] = append(byDay[day], remembered{id: s.chatID(i), topic: topic, at: started, owner: s.chatOwner(i)})
	}
	days := make([]time.Time, 0, len(byDay))
	for d := range byDay {
		days = append(days, d)
	}
	slices.SortFunc(days, func(a, b time.Time) int { return a.Compare(b) })

	// Memos cite the newest remembered session on their topic.
	memosByDay := map[time.Time][]uuid.UUID{}
	for n, m := range memoryMemos {
		var source *remembered
		for _, d := range days {
			for _, r := range byDay[d] {
				if r.topic == m.topic && (source == nil || r.at.After(source.at)) {
					source = &r
				}
			}
		}
		if source == nil {
			return fmt.Errorf("demo memo %d: no remembered session on topic %d", n, m.topic)
		}
		id := s.det("memory-memo-" + strconv.Itoa(n))
		if err := s.q.MemoryInsertMemo(ctx, repo.MemoryInsertMemoParams{
			ID:             id,
			OrganizationID: org,
			ProjectID:      proj,
			ChatID:         uuid.NullUUID{UUID: source.id, Valid: true},
			AuthorUserID:   s.userID(source.owner.handle),
			Content:        m.content,
			CreatedAt:      ts(source.at.Add(20 * time.Minute)),
		}); err != nil {
			return fmt.Errorf("insert demo memo %d: %w", n, err)
		}
		day := source.at.Truncate(24 * time.Hour)
		memosByDay[day] = append(memosByDay[day], id)
	}

	insertDigest := func(level string, start, end, closedAt time.Time, content string, chats, memos, children []uuid.UUID) (uuid.UUID, error) {
		id := s.det("memory-digest-" + level + "-" + start.Format(time.DateOnly))
		if err := s.q.MemoryInsertDigest(ctx, repo.MemoryInsertDigestParams{
			ID:             id,
			OrganizationID: org,
			ProjectID:      proj,
			Level:          level,
			PeriodStart:    date(start),
			PeriodEnd:      date(end),
			Content:        content,
			ChatIds:        chats,
			MemoIds:        memos,
			ChildDigestIds: children,
			GeneratedAt:    ts(writtenAt(closedAt)),
		}); err != nil {
			return uuid.Nil, fmt.Errorf("insert demo %s digest %s: %w", level, start.Format(time.DateOnly), err)
		}
		return id, nil
	}

	// Day digests, and the topics each one covered for the levels above.
	dayDigests := map[time.Time]uuid.UUID{}
	dayTopics := map[time.Time][]int{}
	for _, d := range days {
		var chats []uuid.UUID
		var topics []int
		for _, r := range byDay[d] {
			chats = append(chats, r.id)
			if !slices.Contains(topics, r.topic) {
				topics = append(topics, r.topic)
			}
		}
		content := fmt.Sprintf("%d sessions on %s. The team worked on %s.",
			len(chats), d.Format("Monday, January 2"), memoryTopicList(topics, 4))
		id, err := insertDigest("day", d, d.AddDate(0, 0, 1), d.AddDate(0, 0, 1), content, chats, memosByDay[d], nil)
		if err != nil {
			return err
		}
		dayDigests[d] = id
		dayTopics[d] = topics
	}

	// Week digests for ISO weeks that have closed.
	weekStart := func(d time.Time) time.Time {
		return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
	}
	weekDigests := map[time.Time]uuid.UUID{}
	var weeks []time.Time
	for _, d := range days {
		w := weekStart(d)
		if !w.AddDate(0, 0, 7).After(today) && !slices.Contains(weeks, w) {
			weeks = append(weeks, w)
		}
	}
	for _, w := range weeks {
		var children []uuid.UUID
		var topics []int
		for _, d := range days {
			if weekStart(d).Equal(w) {
				children = append(children, dayDigests[d])
				topics = appendNew(topics, dayTopics[d])
			}
		}
		content := fmt.Sprintf("Week of %s, across %d active days. Recurring work: %s.",
			w.Format("January 2"), len(children), memoryTopicList(topics, 5))
		id, err := insertDigest("week", w, w.AddDate(0, 0, 7), w.AddDate(0, 0, 7), content, nil, nil, children)
		if err != nil {
			return err
		}
		weekDigests[w] = id
	}

	// A month digest for each closed month that has week digests.
	var months []time.Time
	for _, w := range weeks {
		m := time.Date(w.Year(), w.Month(), 1, 0, 0, 0, 0, time.UTC)
		// The rollup closes a month once the ISO week holding its last day
		// has closed too.
		lastWeekEnd := weekStart(m.AddDate(0, 1, -1)).AddDate(0, 0, 7)
		if !lastWeekEnd.After(today) && !slices.Contains(months, m) {
			months = append(months, m)
		}
	}
	for _, m := range months {
		var children []uuid.UUID
		var topics []int
		for _, w := range weeks {
			if w.Year() == m.Year() && w.Month() == m.Month() {
				children = append(children, weekDigests[w])
				for _, d := range days {
					if weekStart(d).Equal(w) {
						topics = appendNew(topics, dayTopics[d])
					}
				}
			}
		}
		content := fmt.Sprintf("%s in review. The team's sessions centered on %s.",
			m.Format("January 2006"), memoryTopicList(topics, 6))
		// A month is written once the week holding its last day closes.
		closedAt := weekStart(m.AddDate(0, 1, -1)).AddDate(0, 0, 7)
		if _, err := insertDigest("month", m, m.AddDate(0, 1, 0), closedAt, content, nil, nil, children); err != nil {
			return err
		}
	}
	return nil
}

// appendNew appends the values of add that dst does not hold yet.
func appendNew(dst, add []int) []int {
	for _, v := range add {
		if !slices.Contains(dst, v) {
			dst = append(dst, v)
		}
	}
	return dst
}

// memoryTopicList joins up to limit topic phrases into one sentence clause.
func memoryTopicList(topics []int, limit int) string {
	phrases := make([]string, 0, limit)
	for _, t := range topics[:min(limit, len(topics))] {
		phrases = append(phrases, memoryTopics[t])
	}
	switch len(phrases) {
	case 0:
		return "routine checks"
	case 1:
		return phrases[0]
	default:
		return strings.Join(phrases[:len(phrases)-1], ", ") + " and " + phrases[len(phrases)-1]
	}
}
