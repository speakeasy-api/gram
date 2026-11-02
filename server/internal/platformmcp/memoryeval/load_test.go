package memoryeval_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/bench"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// loadedCorpus is a corpus written to one test database.
type loadedCorpus struct {
	db             *pgxpool.Pool
	corpus         *memoryeval.Corpus
	organizationID string

	// projects maps corpus projects to their database rows.
	projects map[memoryeval.ProjectKey]projectsrepo.Project
}

// userID is the stored user ID for a corpus handle.
func (l loadedCorpus) userID(handle string) string {
	return "user_memoryeval_" + handle
}

// target is the loaded corpus as the bench sees it.
func (l loadedCorpus) target() bench.Target {
	projects := make(map[memoryeval.ProjectKey]bench.Project, len(l.projects))
	for key, project := range l.projects {
		projects[key] = bench.Project{ID: project.ID, Slug: project.Slug}
	}
	return bench.Target{DB: l.db, Corpus: l.corpus, OrganizationID: l.organizationID, Projects: projects, UserID: l.userID}
}

// writeProxySummaries stands in for the background summary job until it
// lands: every chat gets its whole transcript, masked by the same redaction
// search_chats uses, as its summary. A real summary carries less than this,
// so leaks and isolation measured here are an upper bound.
func writeProxySummaries(t *testing.T, loaded loadedCorpus) {
	t.Helper()
	ctx := t.Context()
	redactor := platformmcp.NewChatSearchRedactor()
	for _, chat := range loaded.corpus.Chats {
		var body strings.Builder
		for _, m := range chat.Messages {
			body.WriteString(m.Text)
			body.WriteString("\n")
		}
		summary, _, err := redactor.Text(ctx, body.String())
		require.NoError(t, err, chat.Key)
		last := chat.Messages[len(chat.Messages)-1].At
		err = testrepo.New(loaded.db).SetChatMemorySummaryFixture(ctx, testrepo.SetChatMemorySummaryFixtureParams{ID: chat.ID, Summary: conv.ToPGText(summary), SummaryGeneratedAt: conv.ToPGTimestamptz(last)})
		require.NoError(t, err, chat.Key)
	}
}

// loadCorpus writes the corpus into a fresh database: one organization, both
// projects, every chat with its corpus ID, and every message at its corpus
// time. Private sessions are attributed to a personal AI account, which is
// how the product marks a session that must stay out of team memory today.
func loadCorpus(t *testing.T, name string, corpus *memoryeval.Corpus) loadedCorpus {
	t.Helper()
	ctx := t.Context()
	db, err := testInfra(t).CloneTestDatabase(t, name)
	require.NoError(t, err)

	organizationID := "org_memoryeval_" + uuid.NewString()
	_, err = organizationsrepo.New(db).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "Memory eval organization",
		Slug:        "memoryeval-" + uuid.NewString()[:8],
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	loaded := loadedCorpus{db: db, corpus: corpus, organizationID: organizationID, projects: make(map[memoryeval.ProjectKey]projectsrepo.Project)}
	for _, key := range []memoryeval.ProjectKey{memoryeval.ProjectMain, memoryeval.ProjectOther} {
		project, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{
			Name:           "Memory eval " + string(key),
			Slug:           "memoryeval-" + string(key) + "-" + uuid.NewString()[:8],
			OrganizationID: organizationID,
		})
		require.NoError(t, err)
		loaded.projects[key] = project
	}

	personalAccounts := map[string]uuid.UUID{}
	for _, chat := range corpus.Chats {
		account := uuid.NullUUID{}
		if chat.Private {
			id, ok := personalAccounts[chat.Owner]
			if !ok {
				id = seedPersonalAccount(ctx, t, db, organizationID)
				personalAccounts[chat.Owner] = id
			}
			account = uuid.NullUUID{UUID: id, Valid: true}
		}
		loaded.insertChat(ctx, t, chat, account)
	}
	return loaded
}

func seedPersonalAccount(ctx context.Context, t *testing.T, db *pgxpool.Pool, organizationID string) uuid.UUID {
	t.Helper()
	id, err := testrepo.New(db).SeedUserAccountFixture(ctx, testrepo.SeedUserAccountFixtureParams{
		OrganizationID:      organizationID,
		ExternalAccountUuid: uuid.NewString(),
		AccountType:         conv.ToPGText("personal"),
	})
	require.NoError(t, err)
	return id
}

func (l loadedCorpus) insertChat(ctx context.Context, t *testing.T, chat memoryeval.Chat, account uuid.NullUUID) {
	t.Helper()
	project := l.projects[chat.Project]
	queries := testrepo.New(l.db)
	_, err := queries.SeedCapturedAgentChatFixture(ctx, testrepo.SeedCapturedAgentChatFixtureParams{
		ID:             chat.ID,
		ProjectID:      project.ID,
		OrganizationID: l.organizationID,
		UserID:         conv.ToPGText(l.userID(chat.Owner)),
		ExternalChatID: conv.ToPGText(chat.Key),
		Title:          conv.ToPGText(chat.Title),
		Cwd:            pgtype.Text{},
		UserAccountID:  account,
	})
	require.NoError(t, err, chat.Key)

	for _, m := range chat.Messages {
		userID := ""
		if m.Role == memoryeval.RoleUser {
			userID = l.userID(chat.Owner)
		}
		_, err := queries.SeedCapturedAgentChatMessageFixture(ctx, testrepo.SeedCapturedAgentChatMessageFixtureParams{
			ChatID:          chat.ID,
			ProjectID:       uuid.NullUUID{UUID: project.ID, Valid: true},
			Role:            m.Role,
			Content:         m.Text,
			Generation:      0,
			ToolCalls:       nil,
			ContentRaw:      nil,
			UserID:          conv.ToPGTextEmpty(userID),
			Source:          conv.ToPGText("claude-code"),
			ContentAssetUrl: pgtype.Text{},
			RiskAnalyzedAt:  pgtype.Timestamptz{},
			CreatedAt:       conv.ToPGTimestamptz(m.At),
		})
		require.NoError(t, err, chat.Key)
	}
}
