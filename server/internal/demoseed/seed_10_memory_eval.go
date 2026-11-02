package demoseed

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
)

// memoryEvalSource labels corpus messages as captured Claude Code sessions,
// the source team memory reads.
const memoryEvalSource = "claude-code"

// memoryEvalProject is one corpus project as the seed writes it.
type memoryEvalProject struct {
	key  memoryeval.ProjectKey
	rest string
	name string
	slug string
}

// memoryEvalProjects keeps the corpus apart from the Default project, so the
// demo's other chats never reach the eval's scores, and gives the isolation
// checks their second project.
var memoryEvalProjects = []memoryEvalProject{
	{key: memoryeval.ProjectMain, rest: "0000-4000-a000-0000000000e1", name: "Team memory eval", slug: "team-memory-eval"},
	{key: memoryeval.ProjectOther, rest: "0000-4000-a000-0000000000e2", name: "Team memory eval (other team)", slug: "team-memory-eval-other"},
}

// MemoryEvalProjectID is the stored ID of one corpus project for spec.
func MemoryEvalProjectID(spec Spec, key memoryeval.ProjectKey) (uuid.UUID, bool) {
	for _, p := range memoryEvalProjects {
		if p.key == key {
			return uuid.MustParse(spec.FixedUUID(p.rest)), true
		}
	}
	return uuid.Nil, false
}

// MemoryEvalAnchor is the corpus anchor for a seed run at t: the start of its
// UTC day. A runner recovers it from the corpus project's creation time, so
// it rebuilds the same corpus, with the same answer keys, that the seed wrote.
func MemoryEvalAnchor(t time.Time) time.Time {
	return t.UTC().Truncate(24 * time.Hour)
}

// MemoryEvalCorpus is the corpus the seed writes for spec at anchor. Chat IDs
// are derived from the tenant, so the corpus never collides with another
// tenant's.
func MemoryEvalCorpus(spec Spec, anchor time.Time) *memoryeval.Corpus {
	return memoryeval.Build(anchor).WithChatIDs(func(key string) uuid.UUID { return detUUID(spec.NameSeed + "memoryeval-chat-" + key) })
}

// seedMemoryEval writes the team memory eval corpus anchored at the seed's
// transaction time: both projects, every chat and message, and a personal
// account per private-session owner. The org purge deletes the projects, so
// every run starts clean. Summaries and digests are not seeded; an eval run
// writes them with the models it compares.
func (s *seeder) seedMemoryEval(ctx context.Context) error {
	org := s.orgID()
	projects := make(map[memoryeval.ProjectKey]uuid.UUID, len(memoryEvalProjects))
	for _, p := range memoryEvalProjects {
		id := s.fixed(p.rest)
		if err := s.q.MemoryEvalInsertProject(ctx, repo.MemoryEvalInsertProjectParams{ProjectID: id, Name: p.name, Slug: p.slug, OrgID: org, CreatedAt: conv.ToPGTimestamptz(s.now)}); err != nil {
			return fmt.Errorf("insert memory eval project %s: %w", p.slug, err)
		}
		projects[p.key] = id
	}

	corpus := MemoryEvalCorpus(s.spec, MemoryEvalAnchor(s.now))
	analyzed := pgtype.Timestamptz{Time: s.now, InfinityModifier: pgtype.Finite, Valid: true}
	personal := map[string]uuid.UUID{}
	chats := make([]repo.MemoryEvalCopyChatsParams, 0, len(corpus.Chats))
	var messages []repo.MemoryEvalCopyChatMessagesParams
	for _, chat := range corpus.Chats {
		account := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		if chat.Private {
			id, ok := personal[chat.Owner]
			if !ok {
				var err error
				id, err = s.memoryEvalPersonalAccount(ctx, chat.Owner)
				if err != nil {
					return err
				}
				personal[chat.Owner] = id
			}
			account = uuid.NullUUID{UUID: id, Valid: true}
		}
		project := projects[chat.Project]
		first, last := chat.Messages[0].At, chat.Messages[len(chat.Messages)-1].At
		chats = append(chats, repo.MemoryEvalCopyChatsParams{
			ID:             chat.ID,
			ProjectID:      project,
			OrganizationID: org,
			UserID:         conv.ToPGText(s.userID(chat.Owner)),
			ExternalUserID: conv.ToPGText(s.email(chat.Owner)),
			ExternalChatID: conv.ToPGText(s.name("memoryeval-" + chat.Key)),
			Title:          conv.ToPGText(chat.Title),
			UserAccountID:  account,
			CreatedAt:      conv.ToPGTimestamptz(first),
			UpdatedAt:      conv.ToPGTimestamptz(last),
		})
		for i, m := range chat.Messages {
			userID := pgtype.Text{String: "", Valid: false}
			if m.Role == memoryeval.RoleUser {
				userID = conv.ToPGText(s.userID(chat.Owner))
			}
			messages = append(messages, repo.MemoryEvalCopyChatMessagesParams{
				ID:             s.det("memoryeval-message-" + chat.Key + "-" + strconv.Itoa(i)),
				ChatID:         chat.ID,
				ProjectID:      uuid.NullUUID{UUID: project, Valid: true},
				Role:           m.Role,
				Content:        m.Text,
				UserID:         userID,
				Source:         conv.ToPGText(memoryEvalSource),
				CreatedAt:      conv.ToPGTimestamptz(m.At),
				RiskAnalyzedAt: analyzed,
			})
		}
	}
	if _, err := s.q.MemoryEvalCopyChats(ctx, chats); err != nil {
		return fmt.Errorf("insert memory eval chats: %w", err)
	}
	if _, err := s.q.MemoryEvalCopyChatMessages(ctx, messages); err != nil {
		return fmt.Errorf("insert memory eval chat messages: %w", err)
	}
	return nil
}

// memoryEvalPersonalAccount returns the owner's personal account, reusing the
// one the roster seeded and adding one only for an owner who has none.
func (s *seeder) memoryEvalPersonalAccount(ctx context.Context, handle string) (uuid.UUID, error) {
	existing, err := s.q.MemoryEvalListPersonalAccounts(ctx, repo.MemoryEvalListPersonalAccountsParams{OrganizationID: s.orgID(), UserID: s.userID(handle)})
	if err != nil {
		return uuid.Nil, fmt.Errorf("list personal accounts for %s: %w", handle, err)
	}
	if len(existing) > 0 {
		return existing[0], nil
	}
	id, err := s.q.MemoryEvalInsertPersonalAccount(ctx, repo.MemoryEvalInsertPersonalAccountParams{
		OrganizationID:      s.orgID(),
		UserID:              s.userID(handle),
		ExternalAccountUuid: "demo-acct-" + handle + "-personal",
		ExternalAccountID:   s.userID(handle) + "_personal",
		Email:               handle + "@personal.example",
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert personal account for %s: %w", handle, err)
	}
	return id, nil
}
