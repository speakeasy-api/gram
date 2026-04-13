package corpus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/corpus"
	srv "github.com/speakeasy-api/gram/server/gen/http/corpus/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/corpus/annotations"
	"github.com/speakeasy-api/gram/server/internal/corpus/autopublish"
	"github.com/speakeasy-api/gram/server/internal/corpus/drafts"
	"github.com/speakeasy-api/gram/server/internal/corpus/feedback"
	corpusgit "github.com/speakeasy-api/gram/server/internal/corpus/git"
	"github.com/speakeasy-api/gram/server/internal/corpus/observability"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

var _ gen.Service = (*GoaService)(nil)
var _ gen.Auther = (*GoaService)(nil)

// GoaService implements the generated gen/corpus.Service interface by
// delegating to the internal drafts service.
type GoaService struct {
	tracer          trace.Tracer
	logger          *slog.Logger
	auth            *auth.Auth
	db              *pgxpool.Pool
	repoBase        string
	feedback        *feedback.Service
	annotations     *annotations.Service
	autoPublish     *autopublish.Service
	observability   *observability.Service
	mu              sync.Mutex
	draftsByProject map[string]*drafts.Service
}

// NewGoaService creates a new corpus Goa service.
func NewGoaService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	sessionManager *sessions.Manager,
	accessLoader auth.AccessLoader,
	db *pgxpool.Pool,
	chConn clickhouse.Conn,
	repoBase string,
) *GoaService {
	return &GoaService{
		tracer:          tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/corpus"),
		logger:          logger.With(attr.SlogComponent("corpus")),
		auth:            auth.New(logger, db, sessionManager, accessLoader),
		db:              db,
		repoBase:        repoBase,
		feedback:        feedback.NewService(db),
		annotations:     annotations.NewService(db),
		autoPublish:     autopublish.NewService(db),
		observability:   observability.NewService(logger, chConn),
		mu:              sync.Mutex{},
		draftsByProject: make(map[string]*drafts.Service),
	}
}

// Attach mounts the corpus Goa service endpoints on the mux.
func Attach(mux goahttp.Muxer, service *GoaService) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *GoaService) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// draftsService returns the drafts.Service for a given project, lazily
// initialising the backing git repo on first access.
func (s *GoaService) draftsService(projectID uuid.UUID) (*drafts.Service, error) {
	key := projectID.String()

	s.mu.Lock()
	defer s.mu.Unlock()

	if svc, ok := s.draftsByProject[key]; ok {
		return svc, nil
	}

	repoPath := filepath.Join(s.repoBase, "repos", key+".git")

	var gitRepo *corpusgit.Repo
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		r, err := corpusgit.InitBareRepo(repoPath)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "init corpus repo")
		}
		gitRepo = r
	} else {
		r, err := corpusgit.OpenRepo(repoPath)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "open corpus repo")
		}
		gitRepo = r
	}

	svc := drafts.NewService(s.db, gitRepo, drafts.NewMutexWriteLock())
	s.draftsByProject[key] = svc
	return svc, nil
}

func (s *GoaService) getAuthContext(ctx context.Context) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	return authCtx, nil
}

func (s *GoaService) CreateDraft(ctx context.Context, p *gen.CreateDraftPayload) (*gen.CorpusDraftResult, error) {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	var labelsJSON []byte
	if len(p.Labels) > 0 {
		labelsJSON, err = json.Marshal(p.Labels)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid labels").Log(ctx, s.logger)
		}
	}

	svc, err := s.draftsService(*authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "corpus service unavailable").Log(ctx, s.logger)
	}

	d, err := svc.Create(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, drafts.CreateDraftParams{
		FilePath:   p.FilePath,
		Content:    p.Content,
		Operation:  p.Operation,
		Source:     p.Source,
		AuthorType: p.AuthorType,
		Labels:     labelsJSON,
	})
	if err != nil {
		if errors.Is(err, drafts.ErrInvalidOperation) || errors.Is(err, drafts.ErrEmptyFilePath) {
			return nil, oops.E(oops.CodeBadRequest, err, "%s", err.Error()).Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create draft").Log(ctx, s.logger)
	}

	return draftToResult(d), nil
}

func (s *GoaService) GetDraft(ctx context.Context, p *gen.GetDraftPayload) (*gen.CorpusDraftResult, error) {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	draftID, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid draft ID").Log(ctx, s.logger)
	}

	svc, err := s.draftsService(*authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "corpus service unavailable").Log(ctx, s.logger)
	}

	d, err := svc.Get(ctx, *authCtx.ProjectID, draftID)
	if err != nil {
		if errors.Is(err, drafts.ErrNotFound) {
			return nil, oops.E(oops.CodeNotFound, err, "draft not found").Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get draft").Log(ctx, s.logger)
	}

	return draftToResult(d), nil
}

func (s *GoaService) ListDrafts(ctx context.Context, p *gen.ListDraftsPayload) (*gen.ListCorpusDraftsResult, error) {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	svc, err := s.draftsService(*authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "corpus service unavailable").Log(ctx, s.logger)
	}

	list, err := svc.List(ctx, *authCtx.ProjectID, p.Status)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list drafts").Log(ctx, s.logger)
	}

	results := make([]*gen.CorpusDraftResult, 0, len(list))
	for i := range list {
		results = append(results, draftToResult(&list[i]))
	}

	return &gen.ListCorpusDraftsResult{
		Drafts: results,
	}, nil
}

func (s *GoaService) UpdateDraft(ctx context.Context, p *gen.UpdateDraftPayload) (*gen.CorpusDraftResult, error) {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	draftID, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid draft ID").Log(ctx, s.logger)
	}

	svc, err := s.draftsService(*authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "corpus service unavailable").Log(ctx, s.logger)
	}

	d, err := svc.UpdateContent(ctx, *authCtx.ProjectID, draftID, p.Content)
	if err != nil {
		if errors.Is(err, drafts.ErrNotFound) {
			return nil, oops.E(oops.CodeNotFound, err, "draft not found").Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update draft").Log(ctx, s.logger)
	}

	return draftToResult(d), nil
}

func (s *GoaService) DeleteDraft(ctx context.Context, p *gen.DeleteDraftPayload) (*gen.CorpusDraftResult, error) {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	draftID, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid draft ID").Log(ctx, s.logger)
	}

	svc, err := s.draftsService(*authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "corpus service unavailable").Log(ctx, s.logger)
	}

	d, err := svc.Delete(ctx, *authCtx.ProjectID, draftID)
	if err != nil {
		if errors.Is(err, drafts.ErrNotFound) {
			return nil, oops.E(oops.CodeNotFound, err, "draft not found").Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "delete draft").Log(ctx, s.logger)
	}

	return draftToResult(d), nil
}

func (s *GoaService) PublishDrafts(ctx context.Context, p *gen.PublishDraftsPayload) (*gen.PublishCorpusDraftsResult, error) {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	ids := make([]uuid.UUID, 0, len(p.DraftIds))
	for _, idStr := range p.DraftIds {
		id, err := uuid.Parse(idStr)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid draft ID: %s", idStr).Log(ctx, s.logger)
		}
		ids = append(ids, id)
	}

	svc, err := s.draftsService(*authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "corpus service unavailable").Log(ctx, s.logger)
	}

	commitSHA, err := svc.Publish(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, ids)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "publish drafts").Log(ctx, s.logger)
	}

	return &gen.PublishCorpusDraftsResult{
		CommitSha: commitSHA,
	}, nil
}

func (s *GoaService) GetEnrichments(ctx context.Context, _ *gen.GetEnrichmentsPayload) (*gen.CorpusEnrichmentsResult, error) {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	svc, err := s.draftsService(*authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "corpus service unavailable").Log(ctx, s.logger)
	}

	enrichments, err := svc.Enrichments(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get enrichments").Log(ctx, s.logger)
	}

	entries := make([]*gen.CorpusEnrichmentEntry, 0, len(enrichments))
	for fp, e := range enrichments {
		entries = append(entries, &gen.CorpusEnrichmentEntry{
			FilePath:   fp,
			OpenDrafts: int(e.OpenDrafts),
		})
	}

	return &gen.CorpusEnrichmentsResult{
		Enrichments: entries,
	}, nil
}

func (s *GoaService) GetFeedback(ctx context.Context, p *gen.GetFeedbackPayload) (*gen.CorpusFeedbackResult, error) {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	summaries, err := s.feedback.ListFeedback(ctx, *authCtx.ProjectID, &p.FilePath)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get feedback").Log(ctx, s.logger)
	}

	return feedbackToResult(summaries, nil), nil
}

func (s *GoaService) VoteFeedback(ctx context.Context, p *gen.VoteFeedbackPayload) (*gen.CorpusFeedbackResult, error) {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	vote, err := s.feedback.Vote(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, feedback.VoteParams{
		FilePath:  p.FilePath,
		UserID:    authCtx.UserID,
		Direction: p.Direction,
	})
	if err != nil {
		if errors.Is(err, feedback.ErrInvalidDirection) || errors.Is(err, feedback.ErrEmptyFilePath) {
			return nil, oops.E(oops.CodeBadRequest, err, "%s", err.Error()).Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "vote feedback").Log(ctx, s.logger)
	}

	summaries, err := s.feedback.ListFeedback(ctx, *authCtx.ProjectID, &p.FilePath)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list feedback").Log(ctx, s.logger)
	}

	var userVote *string
	if vote != nil {
		userVote = conv.PtrEmpty(vote.Direction)
	}

	return feedbackToResult(summaries, userVote), nil
}

func (s *GoaService) ListComments(ctx context.Context, p *gen.ListCommentsPayload) (*gen.ListCorpusCommentsResult, error) {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	comments, err := s.feedback.ListComments(ctx, *authCtx.ProjectID, p.FilePath)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list comments").Log(ctx, s.logger)
	}

	result := make([]*gen.CorpusFeedbackCommentResult, 0, len(comments))
	for i := range comments {
		result = append(result, commentToResult(&comments[i]))
	}

	return &gen.ListCorpusCommentsResult{
		Comments: result,
	}, nil
}

func (s *GoaService) AddComment(ctx context.Context, p *gen.AddCommentPayload) (*gen.CorpusFeedbackCommentResult, error) {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	comment, err := s.feedback.AddComment(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, feedback.AddCommentParams{
		FilePath:   p.FilePath,
		AuthorID:   authCtx.UserID,
		AuthorType: "human",
		Content:    p.Content,
	})
	if err != nil {
		if errors.Is(err, feedback.ErrEmptyFilePath) || errors.Is(err, feedback.ErrEmptyContent) || errors.Is(err, feedback.ErrInvalidAuthorType) {
			return nil, oops.E(oops.CodeBadRequest, err, "%s", err.Error()).Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "add comment").Log(ctx, s.logger)
	}

	return commentToResult(comment), nil
}

func (s *GoaService) ListAnnotations(ctx context.Context, p *gen.ListAnnotationsPayload) (*gen.ListCorpusAnnotationsResult, error) {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	list, err := s.annotations.List(ctx, *authCtx.ProjectID, p.FilePath)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list annotations").Log(ctx, s.logger)
	}

	result := make([]*gen.CorpusAnnotationResult, 0, len(list))
	for i := range list {
		result = append(result, annotationToResult(&list[i]))
	}

	return &gen.ListCorpusAnnotationsResult{
		Annotations: result,
	}, nil
}

func (s *GoaService) CreateAnnotation(ctx context.Context, p *gen.CreateAnnotationPayload) (*gen.CorpusAnnotationResult, error) {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	annotation, err := s.annotations.Create(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, annotations.CreateParams{
		FilePath:   p.FilePath,
		AuthorID:   authCtx.UserID,
		AuthorType: "human",
		Content:    p.Content,
		LineStart:  p.LineStart,
		LineEnd:    p.LineEnd,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create annotation").Log(ctx, s.logger)
	}

	return annotationToResult(annotation), nil
}

func (s *GoaService) DeleteAnnotation(ctx context.Context, p *gen.DeleteAnnotationPayload) (*gen.CorpusAnnotationResult, error) {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	annotationID, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid annotation ID").Log(ctx, s.logger)
	}

	annotation, err := s.annotations.Delete(ctx, *authCtx.ProjectID, annotationID)
	if err != nil {
		if errors.Is(err, annotations.ErrNotFound) {
			return nil, oops.E(oops.CodeNotFound, err, "annotation not found").Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "delete annotation").Log(ctx, s.logger)
	}

	return annotationToResult(annotation), nil
}

func (s *GoaService) GetAutoPublishConfig(ctx context.Context, _ *gen.GetAutoPublishConfigPayload) (*gen.CorpusAutoPublishConfigResult, error) {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	cfg, err := s.autoPublish.GetConfig(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get auto-publish config").Log(ctx, s.logger)
	}

	result, err := autoPublishConfigToResult(cfg)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "decode auto-publish config").Log(ctx, s.logger)
	}

	return result, nil
}

func (s *GoaService) SetAutoPublishConfig(ctx context.Context, p *gen.SetAutoPublishConfigPayload) (*gen.CorpusAutoPublishConfigResult, error) {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	labelFilter, err := json.Marshal(p.LabelFilter)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid label filter").Log(ctx, s.logger)
	}
	if len(p.LabelFilter) == 0 {
		labelFilter = nil
	}

	cfg, err := s.autoPublish.SetConfig(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, autopublish.Config{
		Enabled:          p.Enabled,
		IntervalMinutes:  p.IntervalMinutes,
		MinUpvotes:       p.MinUpvotes,
		AuthorTypeFilter: p.AuthorTypeFilter,
		LabelFilter:      labelFilter,
		MinAgeHours:      p.MinAgeHours,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "set auto-publish config").Log(ctx, s.logger)
	}

	result, err := autoPublishConfigToResult(cfg)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "decode auto-publish config").Log(ctx, s.logger)
	}

	return result, nil
}

func (s *GoaService) SearchLogs(ctx context.Context, p *gen.SearchLogsPayload) (*gen.CorpusSearchLogsResult, error) {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	cursor := ""
	if p.Cursor != nil {
		cursor = *p.Cursor
	}

	result, err := s.observability.SearchLogs(ctx, authCtx.ProjectID.String(), p.Limit, cursor)
	if err != nil {
		if isBadCursorError(err) {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "search logs").Log(ctx, s.logger)
	}

	logs := make([]*gen.CorpusSearchLogResult, 0, len(result.Logs))
	for i := range result.Logs {
		logs = append(logs, searchLogToResult(&result.Logs[i]))
	}

	return &gen.CorpusSearchLogsResult{
		Logs:       logs,
		NextCursor: conv.PtrEmpty(result.NextCursor),
	}, nil
}

func (s *GoaService) SearchStats(ctx context.Context, _ *gen.SearchStatsPayload) (*gen.CorpusSearchStatsResult, error) {
	authCtx, err := s.getAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	stats, err := s.observability.SearchStats(ctx, authCtx.ProjectID.String())
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "search stats").Log(ctx, s.logger)
	}

	topQueries := make([]*gen.CorpusQueryFrequencyResult, 0, len(stats.TopQueries))
	for i := range stats.TopQueries {
		count, err := safeUint64ToInt64(stats.TopQueries[i].Count)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "search stats count overflow").Log(ctx, s.logger)
		}

		topQueries = append(topQueries, &gen.CorpusQueryFrequencyResult{
			Query: stats.TopQueries[i].Query,
			Count: count,
		})
	}

	totalEvents, err := safeUint64ToInt64(stats.TotalEvents)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "search stats total overflow").Log(ctx, s.logger)
	}

	return &gen.CorpusSearchStatsResult{
		TopQueries:  topQueries,
		LatencyP50:  stats.LatencyP50,
		LatencyP95:  stats.LatencyP95,
		LatencyP99:  stats.LatencyP99,
		TotalEvents: totalEvents,
	}, nil
}

// draftToResult converts an internal Draft to the Goa result type.
func draftToResult(d *drafts.Draft) *gen.CorpusDraftResult {
	var labels []string
	if len(d.Labels) > 0 {
		_ = json.Unmarshal(d.Labels, &labels)
	}

	return &gen.CorpusDraftResult{
		ID:              d.ID.String(),
		ProjectID:       d.ProjectID.String(),
		FilePath:        d.FilePath,
		Title:           nil,
		Content:         conv.FromPGText[string](d.Content),
		OriginalContent: nil,
		Operation:       d.Operation,
		Status:          d.Status,
		Source:          conv.FromPGText[string](d.Source),
		AuthorType:      conv.FromPGText[string](d.AuthorType),
		AuthorUserID:    nil,
		AgentName:       nil,
		Labels:          labels,
		CommitSha:       conv.FromPGText[string](d.CommitSha),
		CreatedAt:       d.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:       d.UpdatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
	}
}

func feedbackToResult(summaries []feedback.FeedbackSummary, userVote *string) *gen.CorpusFeedbackResult {
	result := &gen.CorpusFeedbackResult{
		Upvotes:   0,
		Downvotes: 0,
		Labels:    []string{},
		UserVote:  userVote,
	}

	if len(summaries) > 0 {
		result.Upvotes = int(summaries[0].Upvotes)
		result.Downvotes = int(summaries[0].Downvotes)
	}

	return result
}

func commentToResult(comment *feedback.Comment) *gen.CorpusFeedbackCommentResult {
	return &gen.CorpusFeedbackCommentResult{
		ID:         comment.ID.String(),
		Author:     comment.AuthorID,
		AuthorType: comment.AuthorType,
		Content:    comment.Content,
		CreatedAt:  comment.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
		Upvotes:    0,
		Downvotes:  0,
	}
}

func annotationToResult(annotation *annotations.Annotation) *gen.CorpusAnnotationResult {
	var lineStart *int32
	if annotation.LineStart.Valid {
		lineStart = &annotation.LineStart.Int32
	}

	var lineEnd *int32
	if annotation.LineEnd.Valid {
		lineEnd = &annotation.LineEnd.Int32
	}

	return &gen.CorpusAnnotationResult{
		ID:         annotation.ID.String(),
		Author:     annotation.AuthorID,
		AuthorType: annotation.AuthorType,
		Content:    annotation.Content,
		LineStart:  lineStart,
		LineEnd:    lineEnd,
		CreatedAt:  annotation.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
	}
}

func autoPublishConfigToResult(cfg autopublish.Config) (*gen.CorpusAutoPublishConfigResult, error) {
	var labelFilter []string
	if len(cfg.LabelFilter) > 0 {
		if err := json.Unmarshal(cfg.LabelFilter, &labelFilter); err != nil {
			return nil, fmt.Errorf("decode label filter: %w", err)
		}
	}

	return &gen.CorpusAutoPublishConfigResult{
		Enabled:          cfg.Enabled,
		IntervalMinutes:  cfg.IntervalMinutes,
		MinUpvotes:       cfg.MinUpvotes,
		AuthorTypeFilter: cfg.AuthorTypeFilter,
		LabelFilter:      labelFilter,
		MinAgeHours:      cfg.MinAgeHours,
	}, nil
}

func searchLogToResult(log *observability.SearchLog) *gen.CorpusSearchLogResult {
	filters := any(log.Filters)
	if log.Filters != "" {
		var parsed any
		if err := json.Unmarshal([]byte(log.Filters), &parsed); err == nil {
			filters = parsed
		}
	}

	return &gen.CorpusSearchLogResult{
		ID:          log.ID,
		ProjectID:   log.ProjectID,
		Query:       log.Query,
		Filters:     filters,
		ResultCount: int(log.ResultCount),
		LatencyMs:   log.LatencyMs,
		SessionID:   log.SessionID,
		Agent:       log.Agent,
		Timestamp:   log.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
	}
}

func isBadCursorError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "decode cursor") || strings.Contains(msg, "parse cursor offset")
}

func safeUint64ToInt64(v uint64) (int64, error) {
	if v > math.MaxInt64 {
		return 0, errors.New("value exceeds int64 range")
	}

	return int64(v), nil
}
