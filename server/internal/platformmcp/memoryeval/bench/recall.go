package bench

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
)

// recallQueryMaxRunes keeps question-text queries inside the tool's limit.
const recallQueryMaxRunes = 256

// RecallReport compares how often each arm's search returns an answerable
// question's source chats, with no answering agent or judge involved, so it
// isolates retrieval from answering.
type RecallReport struct {
	Rows []RecallRow `json:"rows"`
}

// RecallRow is one arm's retrieval on the answerable questions.
type RecallRow struct {
	Arm string `json:"arm"`

	// Questions is how many answerable questions were asked.
	Questions int `json:"questions"`

	// Keyword counts questions whose sources all came back across searches on
	// its evidence groups: the query an agent writes when it knows the words.
	Keyword int `json:"keyword_retrieved"`

	// Natural counts questions whose sources all came back when the
	// question's own text was the query: the query an agent writes when it
	// does not know which words the team used.
	Natural int `json:"natural_retrieved"`
}

// QuestionRecall searches each arm for every answerable question with each of
// its evidence groups, pooling the results, and once with the question's text.
func QuestionRecall(ctx context.Context, corpus *memoryeval.Corpus, arms []Arm) (RecallReport, error) {
	report := RecallReport{Rows: make([]RecallRow, 0, len(arms))}
	for _, a := range arms {
		if a.Search == nil {
			continue
		}
		row := RecallRow{Arm: a.Name, Questions: 0, Keyword: 0, Natural: 0}
		for _, q := range corpus.Questions {
			if q.ExpectsNotFound() || len(q.Sources) == 0 || len(q.Evidence) == 0 {
				continue
			}
			row.Questions++
			// A timeline question's sources can need different words, so every
			// evidence group is searched and the results pooled.
			var keywordIDs []uuid.UUID
			for _, group := range q.Evidence {
				keyword, err := a.Search(ctx, strings.Join(group, " "))
				if err != nil {
					return RecallReport{}, fmt.Errorf("%s keyword search for %s: %w", a.Name, q.ID, err)
				}
				keywordIDs = append(keywordIDs, keyword.ChatIDs...)
			}
			if retrievedSources(q, keywordIDs) {
				row.Keyword++
			}
			natural, err := a.Search(ctx, recallQuery(q.Text))
			if err != nil {
				return RecallReport{}, fmt.Errorf("%s question search for %s: %w", a.Name, q.ID, err)
			}
			if retrievedSources(q, natural.ChatIDs) {
				row.Natural++
			}
		}
		report.Rows = append(report.Rows, row)
	}
	return report, nil
}

func recallQuery(text string) string {
	if runes := []rune(text); len(runes) > recallQueryMaxRunes {
		return string(runes[:recallQueryMaxRunes])
	}
	return text
}

func retrievedSources(q memoryeval.Question, found []uuid.UUID) bool {
	for _, key := range q.Sources {
		if !slices.Contains(found, memoryeval.ChatID(key)) {
			return false
		}
	}
	return true
}

// Markdown renders the recall table.
func (r RecallReport) Markdown() string {
	var b strings.Builder
	b.WriteString("## Retrieval recall (top 20, no agent)\n\n")
	b.WriteString("All of a question's source chats returned, querying with its evidence words (keyword) or its own text (natural).\n\n")
	b.WriteString("| Arm | Questions | Keyword | Natural |\n|---|---|---|---|\n")
	for _, row := range r.Rows {
		fmt.Fprintf(&b, "| %s | %d | %s | %s |\n", row.Arm, row.Questions, percent(row.Keyword, row.Questions), percent(row.Natural, row.Questions))
	}
	return b.String()
}

func percent(n, of int) string {
	if of == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%d (%.0f%%)", n, 100*float64(n)/float64(of))
}
