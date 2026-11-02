package bench

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chatredact"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/memorydigest"
	digestrepo "github.com/speakeasy-api/gram/server/internal/memorydigest/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// maxDigestBatches bounds Build calls per level so a job that keeps
// reporting more work fails the run instead of looping. The corpus needs a
// handful.
const maxDigestBatches = 50

// MemoryModels are the models that write what team memory reads.
type MemoryModels struct {
	// Summary writes each chat's masked summary.
	Summary string `json:"summary_model"`

	// Digest writes the day, week and month digests.
	Digest string `json:"digest_model"`
}

// DefaultMemoryModels are the models the live summary and digest jobs use.
var DefaultMemoryModels = MemoryModels{Summary: chat.SummaryModel, Digest: activities.MemoryDigestModel}

// Preparation is what writing team memory for a target did and cost.
type Preparation struct {
	Models    MemoryModels `json:"models"`
	Summaries int          `json:"summaries"`
	Failed    int          `json:"failed_summaries"`
	Digests   int          `json:"digests"`

	SummarySpend ModelSpend `json:"summary_spend"`
	DigestSpend  ModelSpend `json:"digest_spend"`
	Duration     int64      `json:"duration_ms"`
}

// Prepare writes team memory for the target with the given models, the way
// the live jobs would: a masked summary for every team chat, placed on the
// day its chat ended, then day, week and month digests as of the corpus's
// now. Private sessions get no summary, as in the live sweep. Existing
// summaries and digests for the target are replaced, so runs with different
// models do not mix.
func Prepare(ctx context.Context, logger *slog.Logger, client openrouter.CompletionClient, target Target, models MemoryModels, concurrency int) (Preparation, error) {
	started := time.Now()
	prep := Preparation{Models: models, Summaries: 0, Failed: 0, Digests: 0, SummarySpend: ModelSpend{Model: models.Summary, Calls: 0, PromptTokens: 0, CompletionTokens: 0, CostUSD: 0}, DigestSpend: ModelSpend{Model: models.Digest, Calls: 0, PromptTokens: 0, CompletionTokens: 0, CostUSD: 0}, Duration: 0}
	if !openrouter.IsModelAllowed(models.Summary) || !openrouter.IsModelAllowed(models.Digest) {
		return prep, fmt.Errorf("summary model %q and digest model %q must both be allowed", models.Summary, models.Digest)
	}
	if concurrency < 1 {
		return prep, errors.New("concurrency must be positive")
	}
	metered := newMeteredClient(client)

	if err := clearMemory(ctx, target); err != nil {
		return prep, err
	}

	generator := chat.NewSummaryGenerator(target.DB, metered, chatredact.New()).WithModel(models.Summary)
	queries := chatrepo.New(target.DB)
	var written, failed atomic.Int64
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(concurrency)
	for _, c := range target.Corpus.Chats {
		if c.Private {
			continue
		}
		projectID := target.Projects[c.Project].ID
		group.Go(func() error {
			if _, err := generator.Generate(groupCtx, target.OrganizationID, projectID, c.ID); err != nil {
				failed.Add(1)
				logger.WarnContext(groupCtx, "memory eval summary failed", attr.SlogChatID(c.ID.String()), attr.SlogError(err))
				return nil
			}
			last := c.Messages[len(c.Messages)-1].At
			err := queries.BackdateChatSummary(groupCtx, chatrepo.BackdateChatSummaryParams{GeneratedAt: conv.ToPGTimestamptz(last), ID: c.ID, ProjectID: projectID})
			if err != nil {
				return fmt.Errorf("backdate summary for %s: %w", c.Key, err)
			}
			written.Add(1)
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return prep, fmt.Errorf("write summaries: %w", err)
	}
	prep.Summaries, prep.Failed = int(written.Load()), int(failed.Load())
	if prep.Summaries == 0 {
		return prep, errors.New("every summary failed")
	}
	prep.SummarySpend = metered.take(models.Summary)

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagTeamMemory, target.OrganizationID, true)
	digests := activities.NewMemoryDigests(logger, target.DB, metered, flags).WithModel(models.Digest)
	for _, level := range []memorydigest.Level{memorydigest.LevelDay, memorydigest.LevelWeek, memorydigest.LevelMonth} {
		built, err := buildLevel(ctx, digests, level, target.Corpus.Now)
		prep.Digests += built
		if err != nil {
			return prep, err
		}
	}
	prep.DigestSpend = metered.take(models.Digest)
	prep.Duration = time.Since(started).Milliseconds()
	return prep, nil
}

// buildLevel runs the digest job for one level until it reports no more
// work, and fails when any period failed, since a missing digest would skew
// the arms that read them.
func buildLevel(ctx context.Context, digests *activities.MemoryDigests, level memorydigest.Level, now time.Time) (int, error) {
	built := 0
	for range maxDigestBatches {
		result, err := digests.Build(ctx, activities.BuildMemoryDigestsArgs{Level: string(level), Now: now})
		if err != nil {
			return built, fmt.Errorf("build %s digests: %w", level, err)
		}
		built += result.Built
		if result.Failed > 0 {
			return built, fmt.Errorf("%d %s digests failed", result.Failed, level)
		}
		if !result.More {
			return built, nil
		}
	}
	return built, fmt.Errorf("%s digests still had work after %d batches", level, maxDigestBatches)
}

// clearMemory deletes the target's digests so the next build starts empty.
// Summaries need no clearing: Generate overwrites every team chat's summary
// and private sessions never get one.
func clearMemory(ctx context.Context, target Target) error {
	projectIDs := make([]uuid.UUID, 0, len(target.Projects))
	for _, p := range target.Projects {
		projectIDs = append(projectIDs, p.ID)
	}
	if _, err := digestrepo.New(target.DB).DeleteProjectDigests(ctx, digestrepo.DeleteProjectDigestsParams{OrganizationID: target.OrganizationID, ProjectIds: projectIDs}); err != nil {
		return fmt.Errorf("clear digests: %w", err)
	}
	return nil
}
