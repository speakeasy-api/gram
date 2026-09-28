package jev

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/speakeasy-api/gram/server/internal/classifier"
)

type wireQuestion struct {
	Type         string           `json:"type"`
	Instructions classifier.Entry `json:"instructions"`
	Criteria     any              `json:"criteria"`
}

type plannedQuestion struct {
	index    int
	question classifier.Question
	wire     wireQuestion
}

func compileRequest(req *classifier.Request) ([]plannedQuestion, error) {
	if req == nil || len(req.Questions) == 0 {
		return nil, fmt.Errorf("jev: request requires questions")
	}
	state, err := json.Marshal(req.Input)
	if err != nil {
		return nil, fmt.Errorf("jev: encode state: %w", err)
	}
	if string(state) == "null" {
		return nil, fmt.Errorf("jev: state must be a string, object, or array")
	}
	questions := make([]plannedQuestion, 0, len(req.Questions))
	keys := make(map[classifier.QuestionKey]bool, len(req.Questions))
	for i, q := range req.Questions {
		if q.Key == "" || keys[q.Key] {
			return nil, fmt.Errorf("jev: question %d has an empty or duplicate key", i)
		}
		keys[q.Key] = true
		wire, err := compile(q)
		if err != nil {
			return nil, fmt.Errorf("jev: question %d: %w", i, err)
		}
		questions = append(questions, plannedQuestion{index: i, question: q, wire: wire})
	}
	return questions, nil
}

func compile(q classifier.Question) (wireQuestion, error) {
	var wire wireQuestion
	kinds := 0
	if q.Noul != nil {
		kinds++
	}
	if q.Choice != nil {
		kinds++
	}
	if q.Score != nil {
		kinds++
	}
	if kinds != 1 {
		return wire, fmt.Errorf("exactly one question variant is required")
	}
	if q.Noul != nil {
		wire.Type = "noul"
		wire.Instructions = q.Noul.Instructions
		wire.Criteria = map[string]classifier.Entry{"true": q.Noul.Positive, "false": q.Noul.Negative}
		return wire, nil
	}
	var options []classifier.Option
	if q.Choice != nil {
		wire.Type = "choice"
		wire.Instructions = q.Choice.Instructions
		options = q.Choice.Options
		if len(options) < 1 || len(options) > 255 {
			return wire, fmt.Errorf("choice requires 1–255 options")
		}
	} else {
		wire.Type = "score"
		wire.Instructions = q.Score.Instructions
		options = q.Score.Levels
		if len(options) < 2 || len(options) > 10 {
			return wire, fmt.Errorf("score requires 2–10 levels")
		}
	}
	keys := make(map[classifier.OptionKey]bool, len(options))
	criteria := make(map[string]classifier.Entry, len(options))
	levels := make([]classifier.Entry, 0, len(options))
	for i, option := range options {
		if option.Key == "" || keys[option.Key] {
			return wire, fmt.Errorf("option %d has an empty or duplicate key", i)
		}
		keys[option.Key] = true
		// Opaque caller keys must not become semantic choice labels.
		criteria[strconv.Itoa(i)] = option.Description
		levels = append(levels, option.Description)
	}
	if q.Choice != nil {
		wire.Criteria = criteria
	} else {
		wire.Criteria = levels
	}
	return wire, nil
}
