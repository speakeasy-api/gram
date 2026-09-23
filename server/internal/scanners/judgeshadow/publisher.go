// Package judgeshadow compares Jev with existing judges without changing enforcement.
package judgeshadow

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	ppopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy/openrouter"
)

const maxStateBytes = 96 << 10

// DefaultSampleRate limits additional traffic within enabled organizations.
const DefaultSampleRate = 0.01

type Publisher struct {
	logger     *slog.Logger
	flags      feature.Provider
	pub        gcp.Publisher[*riskv1.JudgeShadowAnalysis]
	sampleRate float64
}

func NewPublisher(logger *slog.Logger, flags feature.Provider, pub gcp.Publisher[*riskv1.JudgeShadowAnalysis], sampleRate float64) *Publisher {
	return &Publisher{logger: logger, flags: flags, pub: pub, sampleRate: sampleRate}
}

// Enabled evaluates locally; missing flags and provider errors disable shadow work.
// Distinct IDs are organization IDs; these flags deliberately use no group targeting.
func Enabled(ctx context.Context, flags feature.Provider, detector, orgID string) bool {
	if flags == nil || orgID == "" {
		return false
	}
	var flag feature.Flag
	switch detector {
	case promptinjection.Source:
		flag = feature.FlagJevPromptInjectionShadow
	case promptpolicy.Source:
		flag = feature.FlagJevPromptPolicyShadow
	default:
		return false
	}
	enabled, err := flags.IsFlagEnabledLocal(ctx, flag, orgID, nil, nil)
	return err == nil && enabled
}

func (p *Publisher) selected(ctx context.Context, detector, orgID string) bool {
	// #nosec G404 -- Statistical traffic sampling does not require cryptographic randomness.
	return p.sampleRate > 0 && rand.Float64() < p.sampleRate && Enabled(ctx, p.flags, detector, orgID)
}

func (p *Publisher) WrapPolicy(baseline promptpolicy.Evaluator) promptpolicy.Evaluator {
	return func(ctx context.Context, in promptpolicy.Input) (*promptpolicy.Verdict, error) {
		started := time.Now()
		verdict, err := baseline(ctx, in)
		duration := time.Since(started)
		if !p.selected(ctx, promptpolicy.Source, in.OrgID) {
			return verdict, err
		}
		outcome, model := "unavailable", ""
		if verdict != nil {
			model = verdict.Model
			if err == nil && verdict.Completed {
				outcome = "clean"
				if verdict.Matched {
					outcome = "match"
				}
			}
		}
		state := []byte(ppopenrouter.BuildJudgePrompt(in))
		policyHash := fmt.Sprintf("%x", sha256.Sum256([]byte(in.Prompt)))
		p.publish(ctx, promptpolicy.Source, in.OrgID, in.ProjectID, state, outcome, model, duration, policyHash)
		return verdict, err
	}
}

func (p *Publisher) WrapInjection(baseline promptinjection.Classifier) promptinjection.Classifier {
	return func(ctx context.Context, in promptinjection.Request) ([]promptinjection.Result, error) {
		started := time.Now()
		verdicts, err := baseline(ctx, in)
		duration := time.Since(started)
		for i, msg := range in.Messages {
			if !p.selected(ctx, promptinjection.Source, in.OrgID) {
				continue
			}
			outcome, model := "unavailable", ""
			if err == nil && len(verdicts) == len(in.Messages) {
				verdict := verdicts[i]
				model = verdict.Model
				if verdict.Completed {
					switch verdict.Label {
					case promptinjection.LabelSafe:
						outcome = "clean"
					case promptinjection.LabelInjection:
						outcome = "match"
					}
				}
			}
			trajectory := judgemessage.Trajectory{PriorUserRequest: "", RecentUntrustedContent: ""}
			if i < len(in.Trajectories) {
				trajectory = in.Trajectories[i]
			}
			state, _ := piopenrouter.PrepareJudgePayload(msg, trajectory)
			// Batch duration is kept whole, not misreported as per-event latency.
			p.publish(ctx, promptinjection.Source, in.OrgID, in.ProjectID, state, outcome, model, duration, "")
		}
		return verdicts, err
	}
}

func (p *Publisher) publish(ctx context.Context, detector, orgID, projectID string, state []byte, outcome, model string, duration time.Duration, policyHash string) {
	if len(state) > maxStateBytes || ctx.Err() != nil {
		return
	}
	event := riskv1.JudgeShadowAnalysis_builder{
		ComparisonId: new(uuid.NewString()), OrganizationId: new(orgID), ProjectId: new(projectID),
		Detector: new(detector), StateJson: state, BaselineOutcome: new(outcome), BaselineModel: new(model),
		BaselineDurationSeconds: new(duration.Seconds()), BaselineTraceId: new(trace.SpanContextFromContext(ctx).TraceID().String()),
		PolicyHash: new(policyHash), CreatedAt: new(time.Now().UTC().Format(time.RFC3339Nano)),
	}.Build()
	result := p.pub.Publish(ctx, event)
	// The publisher has bounded buffers and signals overflow instead of blocking.
	// Do not wait for network delivery on an enforcement request.
	select {
	case <-result.Ready():
		if _, err := result.Get(ctx); err != nil {
			p.logger.WarnContext(ctx, "enqueue Jev shadow comparison", attr.SlogError(err))
		}
	default:
	}
}
