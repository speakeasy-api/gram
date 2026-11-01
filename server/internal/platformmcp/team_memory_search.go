package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryrepo"
)

const (
	searchTeamMemoryToolName = "search_team_memory"
	// teamMemoryDefaultLookback is the window searched when from is omitted.
	teamMemoryDefaultLookback = 90 * 24 * time.Hour
	// teamMemoryMaxInterval bounds the from/to window of one search.
	teamMemoryMaxInterval = 366 * 24 * time.Hour
	// teamMemorySearchTimeout bounds one search. Chat summaries are matched
	// without an index, inside the project and window filters.
	teamMemorySearchTimeout = 10 * time.Second
	teamMemoryDefaultLimit  = 10
	teamMemoryMaxLimit      = 20
	teamMemoryQueryMaxRunes = 256
	// teamMemorySnippetRunes caps the text returned for each hit.
	teamMemorySnippetRunes = 1200
)

var errTeamMemoryTimeout = errors.New("team memory search timed out")

// SearchTeamMemoryInput searches one project's team memory.
type SearchTeamMemoryInput struct {
	ProjectID string `json:"project_id,omitempty"`

	ProjectSlug string `json:"project_slug,omitempty"`

	// Query is web-search style full-text: words, "quoted phrases", OR and
	// -excluded words.
	Query string `json:"query"`

	// From and To bound the search, RFC3339. To defaults to now and From to 90
	// days before To.
	From string `json:"from,omitempty"`

	To string `json:"to,omitempty"`

	// Kinds limits the search to chat, memo or digest items; empty searches all.
	Kinds []string `json:"kinds,omitempty"`

	Limit int `json:"limit,omitempty"`
}

// TeamMemoryHit is one matching summary, memo or digest.
type TeamMemoryHit struct {
	Source TeamMemorySource `json:"source"`

	// Title is the chat's redacted title, for chat hits.
	Title string `json:"title,omitempty"`

	// Text is the redacted summary, memo or digest text, cut around the match.
	Text string `json:"text"`

	Truncated bool `json:"truncated"`

	// At is when the summary was written, the memo saved, or the digest's
	// period started.
	At time.Time `json:"at"`

	// AuthorUserID is who saved a memo.
	AuthorUserID string `json:"author_user_id,omitempty"`

	// Level and PeriodStart describe a digest: day, week or month.
	Level string `json:"level,omitempty"`

	PeriodStart string `json:"period_start,omitempty"`
}

// SearchTeamMemoryOutput is the ranked hits, best first.
type SearchTeamMemoryOutput struct {
	Hits []TeamMemoryHit `json:"hits"`

	From time.Time `json:"from"`

	To time.Time `json:"to"`
}

// Search ranks one project's masked chat summaries, memos and digests against
// a full-text query. Every returned text is redacted again on the way out, so
// a masking rule added after an item was stored still applies.
func (s *TeamMemoryService) Search(ctx context.Context, principal Principal, input SearchTeamMemoryInput) (SearchTeamMemoryOutput, error) {
	var zero SearchTeamMemoryOutput
	query := strings.TrimSpace(input.Query)
	limit := input.Limit
	if limit == 0 {
		limit = teamMemoryDefaultLimit
	}
	if query == "" || !validChatSearchFilter(query, teamMemoryQueryMaxRunes) || limit < 1 || limit > teamMemoryMaxLimit {
		return zero, errTeamMemoryInvalid
	}
	from, to, err := teamMemoryWindow(input.From, input.To, time.Now())
	if err != nil {
		return zero, err
	}
	includeChats, includeMemos, includeDigests := len(input.Kinds) == 0, len(input.Kinds) == 0, len(input.Kinds) == 0
	for _, kind := range input.Kinds {
		switch kind {
		case TeamMemorySourceChat:
			includeChats = true
		case TeamMemorySourceMemo:
			includeMemos = true
		case TeamMemorySourceDigest:
			includeDigests = true
		default:
			return zero, errTeamMemoryInvalid
		}
	}

	project, err := s.authorizeRead(ctx, principal, input.ProjectID, input.ProjectSlug)
	if err != nil {
		return zero, err
	}
	searchCtx, cancel := context.WithTimeout(ctx, teamMemorySearchTimeout)
	defer cancel()
	rows, err := memoryrepo.New(s.db).SearchTeamMemory(searchCtx, memoryrepo.SearchTeamMemoryParams{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID,
		IncludeChats: includeChats, IncludeMemos: includeMemos, IncludeDigests: includeDigests,
		FromTime: conv.ToPGTimestamptz(from), ToTime: conv.ToPGTimestamptz(to),
		Query: query, RowLimit: int32(limit),
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return zero, errTeamMemoryTimeout
		}
		return zero, fmt.Errorf("search team memory: %w", err)
	}

	output := SearchTeamMemoryOutput{Hits: make([]TeamMemoryHit, 0, len(rows)), From: from, To: to}
	sources := make([]TeamMemorySource, 0, len(rows))
	for _, row := range rows {
		body := row.Body
		if row.Kind == TeamMemorySourceChat {
			body = firstParagraph(body)
		}
		text, _, err := s.redactor.Text(ctx, body)
		if err != nil {
			return zero, fmt.Errorf("redact team memory hit: %w", err)
		}
		title, _, err := s.redactor.Text(ctx, row.Title)
		if err != nil {
			return zero, fmt.Errorf("redact team memory title: %w", err)
		}
		snippet, truncated := chatSnippet(text, firstQueryTerm(query), teamMemorySnippetRunes)
		source := TeamMemorySource{Kind: row.Kind, ID: row.ID.String(), ChatID: ""}
		if row.ChatID.Valid {
			source.ChatID = row.ChatID.UUID.String()
		}
		hit := TeamMemoryHit{Source: source, Title: title, Text: snippet, Truncated: truncated || utf8.RuneCountInString(body) < utf8.RuneCountInString(row.Body), At: row.At.Time.UTC(), AuthorUserID: row.AuthorUserID, Level: row.Level, PeriodStart: ""}
		if row.PeriodStart.Valid {
			hit.PeriodStart = row.PeriodStart.Time.Format(time.DateOnly)
		}
		output.Hits = append(output.Hits, hit)
		sources = append(sources, source)
	}
	if err := s.recordRead(ctx, principal, project, searchTeamMemoryToolName, sources); err != nil {
		return zero, err
	}
	return output, nil
}

// teamMemoryWindow parses the optional from/to bounds.
func teamMemoryWindow(rawFrom, rawTo string, now time.Time) (time.Time, time.Time, error) {
	to := now.UTC()
	if rawTo != "" {
		parsed, err := time.Parse(time.RFC3339Nano, rawTo)
		if err != nil {
			return time.Time{}, time.Time{}, errTeamMemoryInvalid
		}
		to = parsed.UTC()
	}
	from := to.Add(-teamMemoryDefaultLookback)
	if rawFrom != "" {
		parsed, err := time.Parse(time.RFC3339Nano, rawFrom)
		if err != nil {
			return time.Time{}, time.Time{}, errTeamMemoryInvalid
		}
		from = parsed.UTC()
	}
	if !to.After(from) || to.Sub(from) > teamMemoryMaxInterval {
		return time.Time{}, time.Time{}, errTeamMemoryInvalid
	}
	return from, to, nil
}

// firstParagraph returns a summary's first paragraph, which the summary job
// writes to stand alone as the memory snippet.
func firstParagraph(text string) string {
	text = strings.TrimSpace(text)
	if before, _, ok := strings.Cut(text, "\n\n"); ok {
		return strings.TrimSpace(before)
	}
	return text
}

// firstQueryTerm picks a word to centre snippets on.
func firstQueryTerm(query string) string {
	fields := strings.FieldsFunc(query, func(r rune) bool { return r == ' ' || r == '"' || r == '\t' || r == '\n' })
	fields = slices.DeleteFunc(fields, func(field string) bool { return strings.HasPrefix(field, "-") || strings.EqualFold(field, "or") })
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
