package feedback

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/corpus/feedback/repo"
)

const (
	DirectionUp   = "up"
	DirectionDown = "down"
)

var ErrInvalidDirection = errors.New("invalid direction: must be up or down")
var ErrEmptyFilePath = errors.New("file_path must not be empty")
var ErrEmptyContent = errors.New("content must not be empty")
var ErrInvalidAuthorType = errors.New("invalid author_type: must be human or agent")
var errNotImplemented = errors.New("not implemented")

type VoteParams struct {
	FilePath  string
	UserID    string
	Direction string
}

type AddCommentParams struct {
	FilePath   string
	AuthorID   string
	AuthorType string
	Content    string
}

// Vote is the corpus_feedback row type.
type Vote = repo.CorpusFeedback

// Comment is the corpus_feedback_comments row type.
type Comment = repo.CorpusFeedbackComment

// FeedbackSummary is the aggregated feedback for a file path.
type FeedbackSummary struct {
	FilePath  string
	Upvotes   int64
	Downvotes int64
}

type Service struct {
	db   *pgxpool.Pool
	repo *repo.Queries
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{
		db:   db,
		repo: repo.New(db),
	}
}

func (s *Service) Vote(_ context.Context, _ uuid.UUID, _ string, _ VoteParams) (*Vote, error) {
	return nil, errNotImplemented
}

func (s *Service) ListFeedback(_ context.Context, _ uuid.UUID, _ *string) ([]FeedbackSummary, error) {
	return nil, errNotImplemented
}

func (s *Service) AddComment(_ context.Context, _ uuid.UUID, _ string, _ AddCommentParams) (*Comment, error) {
	return nil, errNotImplemented
}

func (s *Service) ListComments(_ context.Context, _ uuid.UUID, _ string) ([]Comment, error) {
	return nil, errNotImplemented
}
