package intentpolicy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/classifier"
)

// JevModel pins the System One model so labels stay comparable across runs.
const JevModel = "jev-1.13"

// jevPromptVersion identifies the label-question compilation below.
const jevPromptVersion = "intent-labels-v1"

// noulThreshold is the probability at which a yes/no label reads as true.
const noulThreshold = 0.5

// JevEvaluator labels calls with a classifier.Classifier (Jev in production).
type JevEvaluator struct {
	classifier classifier.Classifier
}

var _ Evaluator = (*JevEvaluator)(nil)

// NewJevEvaluator wraps a classifier configured for JevModel.
func NewJevEvaluator(c classifier.Classifier) *JevEvaluator {
	return &JevEvaluator{classifier: c}
}

// Descriptor identifies the Jev evaluator.
func (e *JevEvaluator) Descriptor() Descriptor {
	return Descriptor{Evaluator: "jev", Provider: "openrouter", Model: JevModel, PromptVersion: jevPromptVersion}
}

func memberKey(label, option string) classifier.QuestionKey {
	return classifier.QuestionKey(label + "." + option)
}

func questionText(q LabelQuestion) string {
	return Framing + "\n\nQuestion: " + q.Instructions
}

// CompileJevRequest compiles one call's label questions into a classifier batch.
func CompileJevRequest(in LabelInput) (*classifier.Request, error) {
	state, err := RenderState(in)
	if err != nil {
		return nil, err
	}
	entry, err := classifier.ParseEntry(state)
	if err != nil {
		return nil, fmt.Errorf("parse evaluator state: %w", err)
	}

	req := classifier.NewRequest(entry)
	for _, q := range in.Questions {
		switch q.Kind {
		case KindNoul:
			req.Ask(classifier.Noul(classifier.QuestionKey(q.Key), classifier.Text(questionText(q))))
		case KindMultiNoul:
			for _, o := range q.Options {
				text := questionText(q) + "\n\nEffect class \"" + o.Key + "\": " + o.Description + "\nDoes the call have this effect?"
				req.Ask(classifier.Noul(memberKey(q.Key, o.Key), classifier.Text(text),
					classifier.WithPositive(classifier.Text(o.Description)),
				))
			}
		case KindChoice:
			options := make([]classifier.Option, 0, len(q.Options))
			for _, o := range q.Options {
				options = append(options, classifier.NewOption(classifier.OptionKey(o.Key), classifier.Text(o.Description)))
			}
			req.Ask(classifier.Choice(classifier.QuestionKey(q.Key), classifier.Text(questionText(q)), options...))
		default:
			return nil, fmt.Errorf("unsupported label kind %q", q.Kind)
		}
	}
	return req, nil
}

// Label runs one classifier call and folds its answers into labels.
func (e *JevEvaluator) Label(ctx context.Context, in LabelInput) (LabelSet, Usage, error) {
	req, err := CompileJevRequest(in)
	if err != nil {
		return nil, Usage{}, err
	}

	start := time.Now()
	result := e.classifier.Classify(ctx, req)
	usage := Usage{
		InputTokens:  result.Usage.InputTokens,
		OutputTokens: result.Usage.OutputTokens,
		CostUSD:      0,
		LatencyMs:    time.Since(start).Milliseconds(),
	}

	answers := make(map[classifier.QuestionKey]*classifier.Answer, len(result.Outcomes))
	var failures []string
	for outcome := range result.Iter() {
		if outcome.Failure != nil {
			failures = append(failures, fmt.Sprintf("%s: %s", outcome.Key, outcome.Failure.Message))
			continue
		}
		answers[outcome.Key] = outcome.Answer
	}

	labels := FoldJevAnswers(in.Questions, answers)
	var errs []error
	if err := result.Err(); err != nil {
		errs = append(errs, err)
	}
	if len(failures) > 0 {
		errs = append(errs, errors.New(strings.Join(failures, "; ")))
	}
	return labels, usage, errors.Join(errs...)
}

// FoldJevAnswers maps classifier answers back to labels. A label whose answers
// are incomplete is omitted so the contracts reading it fail closed.
func FoldJevAnswers(qs []LabelQuestion, answers map[classifier.QuestionKey]*classifier.Answer) LabelSet {
	labels := LabelSet{}
	for _, q := range qs {
		switch q.Kind {
		case KindNoul:
			a := answers[classifier.QuestionKey(q.Key)]
			if a == nil || a.Noul == nil {
				continue
			}
			p := a.Noul.Probability
			value := p >= noulThreshold
			labels[q.Key] = LabelValue{Value: value, Rank: -1, Probability: selectedProbability(value, p), Distribution: nil, Rationale: ""}
		case KindMultiNoul:
			members := []string{}
			dist := make(map[string]float64, len(q.Options))
			confidence := 1.0
			complete := true
			for _, o := range q.Options {
				a := answers[memberKey(q.Key, o.Key)]
				if a == nil || a.Noul == nil {
					complete = false
					break
				}
				p := a.Noul.Probability
				dist[o.Key] = p
				if p >= noulThreshold {
					members = append(members, o.Key)
				}
				confidence = min(confidence, selectedProbability(p >= noulThreshold, p))
			}
			if !complete {
				continue
			}
			labels[q.Key] = LabelValue{Value: members, Rank: -1, Probability: confidence, Distribution: dist, Rationale: ""}
		case KindChoice:
			a := answers[classifier.QuestionKey(q.Key)]
			if a == nil || a.Choice == nil {
				continue
			}
			dist := make(map[string]float64, len(a.Choice.Distribution))
			for _, p := range a.Choice.Distribution {
				dist[string(p.Option)] = p.Value
			}
			selected := string(a.Choice.Selected)
			labels[q.Key] = LabelValue{Value: selected, Rank: optionRank(q, selected), Probability: dist[selected], Distribution: dist, Rationale: ""}
		}
	}
	return labels
}

func selectedProbability(value bool, p float64) float64 {
	if value {
		return p
	}
	return 1 - p
}

func optionRank(q LabelQuestion, key string) int {
	for _, o := range q.Options {
		if o.Key == key {
			if o.Uncertain {
				return -1
			}
			return o.Ordinal
		}
	}
	return -1
}
