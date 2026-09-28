package jev

import (
	"math"
	"strconv"

	"github.com/speakeasy-api/gram/server/internal/classifier"
)

type wireAnswer struct {
	Type          string              `json:"type"`
	Noul          *float64            `json:"noul"`
	Choice        *string             `json:"choice"`
	Score         *float64            `json:"score"`
	Probabilities map[string]*float64 `json:"probabilities"`
	Confidence    *float64            `json:"confidence"`
}

const probabilityTolerance = 1e-5

func probability(value *float64) bool {
	return value != nil && !math.IsNaN(*value) && !math.IsInf(*value, 0) && *value >= 0 && *value <= 1
}

func decodeAnswer(q plannedQuestion, wire wireAnswer) *classifier.Answer {
	if wire.Type != q.wire.Type {
		return nil
	}
	var answer classifier.Answer
	if q.question.Noul != nil {
		if !probability(wire.Noul) {
			return nil
		}
		answer.Noul = &classifier.NoulAnswer{Probability: *wire.Noul}
		return &answer
	}
	if !probability(wire.Confidence) {
		return nil
	}
	var options []classifier.Option
	if q.question.Choice != nil {
		options = q.question.Choice.Options
	} else {
		options = q.question.Score.Levels
	}
	if len(wire.Probabilities) != len(options) {
		return nil
	}
	distribution := make([]classifier.Probability, 0, len(options))
	sum, expected, maximum := 0.0, 0.0, 0.0
	for i, option := range options {
		p := wire.Probabilities[strconv.Itoa(i)]
		if !probability(p) {
			return nil
		}
		sum += *p
		expected += float64(i) * *p
		maximum = max(maximum, *p)
		distribution = append(distribution, classifier.Probability{Option: option.Key, Value: *p})
	}
	if math.Abs(sum-1) > probabilityTolerance {
		return nil
	}
	if q.question.Choice != nil {
		if wire.Choice == nil {
			return nil
		}
		index, err := strconv.Atoi(*wire.Choice)
		if err != nil || index < 0 || index >= len(options) || strconv.Itoa(index) != *wire.Choice || distribution[index].Value+probabilityTolerance < maximum {
			return nil
		}
		answer.Choice = &classifier.ChoiceAnswer{Selected: options[index].Key, Distribution: distribution, Confidence: wire.Confidence}
	} else {
		if wire.Score == nil || math.IsNaN(*wire.Score) || math.IsInf(*wire.Score, 0) || *wire.Score < 0 || *wire.Score > float64(len(options)-1) || math.Abs(*wire.Score-expected) > probabilityTolerance*float64(len(options)) {
			return nil
		}
		answer.Score = &classifier.ScoreAnswer{ExpectedIndex: *wire.Score, Distribution: distribution, Confidence: wire.Confidence}
	}
	return &answer
}
