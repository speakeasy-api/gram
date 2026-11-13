package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/memorydigest"
	memorydigestrepo "github.com/speakeasy-api/gram/server/internal/memorydigest/repo"
)

const (
	zoomTeamMemoryToolName = "zoom"
	// teamMemoryZoomItemRunes caps the text of the item zoomed into.
	teamMemoryZoomItemRunes = 8000
	// teamMemoryZoomChildRunes caps each child's text. A month folds about
	// five weeks and a week seven days, so the reply stays a few thousand
	// tokens.
	teamMemoryZoomChildRunes = 2400
)

var errTeamMemoryItemNotFound = errors.New("team memory item not found")

// ZoomTeamMemoryInput opens one cited item one level down.
type ZoomTeamMemoryInput struct {
	ProjectID string `json:"project_id,omitempty"`

	ProjectSlug string `json:"project_slug,omitempty"`

	// Source is a citation from recall_team_history or search_team_memory:
	// digest:<id>, chat:<id> or memo:<id>, with or without the brackets.
	Source string `json:"source"`
}

// TeamMemoryItem is one digest, session summary or memo shown by zoom.
type TeamMemoryItem struct {
	Source TeamMemorySource `json:"source"`

	// Heading names the period or date, as the recall block does.
	Heading string `json:"heading"`

	// At is when the digest's period started, the summary was written, or
	// the memo was saved.
	At time.Time `json:"at"`

	// Level is day, week or month for a digest.
	Level string `json:"level,omitempty"`

	// AuthorName is who saved a memo.
	AuthorName string `json:"author_name,omitempty"`

	// Text is the masked digest, summary or memo text.
	Text string `json:"text"`

	Truncated bool `json:"truncated"`
}

// ZoomTeamMemoryOutput is the item and what it folded, oldest first.
type ZoomTeamMemoryOutput struct {
	Item TeamMemoryItem `json:"item"`

	// Children are the digests a week or month digest folded, or the
	// summaries and memos a day digest folded. A chat or memo has none.
	// Folded items that are no longer eligible are left out.
	Children []TeamMemoryItem `json:"children"`
}

// Zoom opens one team memory item. A month or week digest returns the
// digests it folded, a day digest the session summaries and memos it folded,
// and a chat or memo its full masked text. A stale digest is not found, like
// in recall, so an exclusion hides it before the nightly job deletes it.
// Every text is masked again before it is cut.
func (s *TeamMemoryService) Zoom(ctx context.Context, principal Principal, input ZoomTeamMemoryInput) (ZoomTeamMemoryOutput, error) {
	var zero ZoomTeamMemoryOutput
	kind, id, err := parseTeamMemorySource(input.Source)
	if err != nil {
		return zero, err
	}
	project, err := s.authorizeRead(ctx, principal, input.ProjectID, input.ProjectSlug)
	if err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, teamMemorySearchTimeout)
	defer cancel()
	output, err := s.zoom(ctx, principal, project, kind, id)
	if errors.Is(err, context.DeadlineExceeded) {
		return zero, errTeamMemoryTimeout
	}
	return output, err
}

func (s *TeamMemoryService) zoom(ctx context.Context, principal Principal, project ResolvedProject, kind string, id uuid.UUID) (ZoomTeamMemoryOutput, error) {
	var zero ZoomTeamMemoryOutput
	var output ZoomTeamMemoryOutput
	var err error
	switch kind {
	case TeamMemorySourceDigest:
		output, err = s.zoomDigest(ctx, principal, project, id)
	case TeamMemorySourceChat:
		var chats []TeamMemoryItem
		chats, err = s.zoomChats(ctx, principal, project, []uuid.UUID{id}, teamMemoryZoomItemRunes)
		if err == nil && len(chats) == 0 {
			err = errTeamMemoryItemNotFound
		}
		if err == nil {
			output = ZoomTeamMemoryOutput{Item: chats[0], Children: []TeamMemoryItem{}}
		}
	default:
		var memos []TeamMemoryItem
		memos, err = s.zoomMemos(ctx, principal, project, []uuid.UUID{id}, teamMemoryZoomItemRunes)
		if err == nil && len(memos) == 0 {
			err = errTeamMemoryItemNotFound
		}
		if err == nil {
			output = ZoomTeamMemoryOutput{Item: memos[0], Children: []TeamMemoryItem{}}
		}
	}
	if err != nil {
		return zero, err
	}

	sources := []TeamMemorySource{output.Item.Source}
	for _, child := range output.Children {
		sources = append(sources, child.Source)
	}
	if err := s.recordRead(ctx, principal, project, zoomTeamMemoryToolName, sources); err != nil {
		return zero, err
	}
	return output, nil
}

func (s *TeamMemoryService) zoomDigest(ctx context.Context, principal Principal, project ResolvedProject, id uuid.UUID) (ZoomTeamMemoryOutput, error) {
	var zero ZoomTeamMemoryOutput
	queries := memorydigestrepo.New(s.db)
	row, err := queries.GetDigest(ctx, memorydigestrepo.GetDigestParams{ProjectID: project.ID, ID: id, OrganizationID: principal.OrganizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, errTeamMemoryItemNotFound
	}
	if err != nil {
		return zero, fmt.Errorf("get digest: %w", err)
	}
	item, err := s.digestItem(ctx, row.ID, row.Level, row.PeriodStart.Time, row.Content, teamMemoryZoomItemRunes)
	if err != nil {
		return zero, err
	}

	var children []TeamMemoryItem
	if row.Level == string(memorydigest.LevelDay) {
		chats, err := s.zoomChats(ctx, principal, project, row.ChatIds, teamMemoryZoomChildRunes)
		if err != nil {
			return zero, err
		}
		// A day digest covers a chat only for the day of its current
		// summary, as in recall: a summary regenerated on a later day belongs
		// to that day, not to this snapshot.
		day := row.PeriodStart.Time.UTC().Format(time.DateOnly)
		chats = slices.DeleteFunc(chats, func(chat TeamMemoryItem) bool { return chat.At.Format(time.DateOnly) != day })
		memos, err := s.zoomMemos(ctx, principal, project, row.MemoIds, teamMemoryZoomChildRunes)
		if err != nil {
			return zero, err
		}
		children = slices.Concat(chats, memos)
	} else {
		rows, err := queries.ListDigestsByID(ctx, memorydigestrepo.ListDigestsByIDParams{ProjectID: project.ID, Ids: row.ChildDigestIds, OrganizationID: principal.OrganizationID})
		if err != nil {
			return zero, fmt.Errorf("list child digests: %w", err)
		}
		children = make([]TeamMemoryItem, 0, len(rows))
		for _, child := range rows {
			childItem, err := s.digestItem(ctx, child.ID, child.Level, child.PeriodStart.Time, child.Content, teamMemoryZoomChildRunes)
			if err != nil {
				return zero, err
			}
			children = append(children, childItem)
		}
	}
	slices.SortStableFunc(children, func(a, b TeamMemoryItem) int { return a.At.Compare(b.At) })
	if children == nil {
		children = []TeamMemoryItem{}
	}
	return ZoomTeamMemoryOutput{Item: item, Children: children}, nil
}

func (s *TeamMemoryService) digestItem(ctx context.Context, id uuid.UUID, rawLevel string, periodStart time.Time, content string, maxRunes int) (TeamMemoryItem, error) {
	var zero TeamMemoryItem
	level, err := memorydigest.ParseLevel(rawLevel)
	if err != nil {
		return zero, fmt.Errorf("zoom digest level: %w", err)
	}
	text, truncated, err := s.maskedCut(ctx, content, maxRunes)
	if err != nil {
		return zero, err
	}
	start := periodStart.UTC()
	return TeamMemoryItem{
		Source:  TeamMemorySource{Kind: TeamMemorySourceDigest, ID: id.String(), ChatID: ""},
		Heading: fmt.Sprintf("%s (%s digest)", level.Label(start), level), At: start, Level: string(level),
		AuthorName: "", Text: text, Truncated: truncated,
	}, nil
}

// zoomChats returns the still-eligible chats among ids with their masked
// summaries. Titles are left out: they are not masked at rest.
func (s *TeamMemoryService) zoomChats(ctx context.Context, principal Principal, project ResolvedProject, ids []uuid.UUID, maxRunes int) ([]TeamMemoryItem, error) {
	if len(ids) == 0 {
		return []TeamMemoryItem{}, nil
	}
	rows, err := memorydigestrepo.New(s.db).ListZoomChats(ctx, memorydigestrepo.ListZoomChatsParams{Ids: ids, ProjectID: project.ID, OrganizationID: principal.OrganizationID})
	if err != nil {
		return nil, fmt.Errorf("list zoom chats: %w", err)
	}
	items := make([]TeamMemoryItem, 0, len(rows))
	for _, row := range rows {
		text, truncated, err := s.maskedCut(ctx, row.Summary, maxRunes)
		if err != nil {
			return nil, err
		}
		at := row.SummaryGeneratedAt.Time.UTC()
		items = append(items, TeamMemoryItem{
			Source:  TeamMemorySource{Kind: TeamMemorySourceChat, ID: row.ID.String(), ChatID: ""},
			Heading: "Chat " + at.Format(time.DateOnly), At: at, Level: "", AuthorName: "", Text: text, Truncated: truncated,
		})
	}
	return items, nil
}

// zoomMemos returns the still-eligible memos among ids.
func (s *TeamMemoryService) zoomMemos(ctx context.Context, principal Principal, project ResolvedProject, ids []uuid.UUID, maxRunes int) ([]TeamMemoryItem, error) {
	if len(ids) == 0 {
		return []TeamMemoryItem{}, nil
	}
	rows, err := memorydigestrepo.New(s.db).ListZoomMemos(ctx, memorydigestrepo.ListZoomMemosParams{Ids: ids, ProjectID: project.ID, OrganizationID: principal.OrganizationID})
	if err != nil {
		return nil, fmt.Errorf("list zoom memos: %w", err)
	}
	items := make([]TeamMemoryItem, 0, len(rows))
	for _, row := range rows {
		text, truncated, err := s.maskedCut(ctx, row.Content, maxRunes)
		if err != nil {
			return nil, err
		}
		source := TeamMemorySource{Kind: TeamMemorySourceMemo, ID: row.ID.String(), ChatID: ""}
		if row.ChatID.Valid {
			source.ChatID = row.ChatID.UUID.String()
		}
		at := row.CreatedAt.Time.UTC()
		items = append(items, TeamMemoryItem{
			Source: source, Heading: "Memo " + at.Format(time.DateOnly), At: at, Level: "",
			AuthorName: row.AuthorName, Text: text, Truncated: truncated,
		})
	}
	return items, nil
}

// maskedCut masks text and then cuts it, so a cut never splits a secret the
// masker would otherwise catch.
func (s *TeamMemoryService) maskedCut(ctx context.Context, text string, maxRunes int) (string, bool, error) {
	masked, _, err := s.redactor.Text(ctx, strings.TrimSpace(text))
	if err != nil {
		return "", false, fmt.Errorf("redact team memory item: %w", err)
	}
	cut, truncated := chatSnippet(masked, "", maxRunes)
	return cut, truncated, nil
}

// parseTeamMemorySource reads a citation: digest:<id>, chat:<id> or
// memo:<id>, optionally in brackets as the recall block prints it.
func parseTeamMemorySource(raw string) (string, uuid.UUID, error) {
	trimmed := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(raw), "["), "]")
	kind, rawID, ok := strings.Cut(trimmed, ":")
	if !ok {
		return "", uuid.Nil, errTeamMemoryInvalid
	}
	switch kind {
	case TeamMemorySourceDigest, TeamMemorySourceChat, TeamMemorySourceMemo:
	default:
		return "", uuid.Nil, errTeamMemoryInvalid
	}
	id, err := uuid.Parse(rawID)
	if err != nil {
		return "", uuid.Nil, errTeamMemoryInvalid
	}
	return kind, id, nil
}
