package feedback

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

var validDirections = map[string]bool{
	DirectionUp:   true,
	DirectionDown: true,
}

var validAuthorTypes = map[string]bool{
	"human": true,
	"agent": true,
}

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

// Vote records an up/down vote on a file. Every vote is persisted as a new row,
// allowing the same user to vote multiple times on the same file.
func (s *Service) Vote(ctx context.Context, projectID uuid.UUID, orgID string, params VoteParams) (*Vote, error) {
	if !validDirections[params.Direction] {
		return nil, ErrInvalidDirection
	}
	if params.FilePath == "" {
		return nil, ErrEmptyFilePath
	}

	v, err := s.repo.CreateVote(ctx, repo.CreateVoteParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
		FilePath:       params.FilePath,
		UserID:         params.UserID,
		Direction:      params.Direction,
	})
	if err != nil {
		return nil, fmt.Errorf("create vote: %w", err)
	}
	return &v, nil
}

// LatestVoteDirection returns the most recent vote direction from the given
// user for a file. It returns nil when the user has never voted on the file.
func (s *Service) LatestVoteDirection(ctx context.Context, projectID uuid.UUID, filePath string, userID string) (*string, error) {
	if filePath == "" {
		return nil, ErrEmptyFilePath
	}
	if userID == "" {
		return nil, nil
	}

	vote, err := s.repo.GetLatestVoteForFileByUser(ctx, repo.GetLatestVoteForFileByUserParams{
		ProjectID: projectID,
		FilePath:  filePath,
		UserID:    userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest vote: %w", err)
	}

	return &vote.Direction, nil
}

// ListFeedback returns aggregated vote counts per file path. If filePath is
// non-nil, only feedback for that file is returned.
func (s *Service) ListFeedback(ctx context.Context, projectID uuid.UUID, filePath *string) ([]FeedbackSummary, error) {
	if filePath != nil {
		rows, err := s.repo.ListFeedbackForFile(ctx, repo.ListFeedbackForFileParams{
			ProjectID: projectID,
			FilePath:  *filePath,
		})
		if err != nil {
			return nil, fmt.Errorf("list feedback for file: %w", err)
		}
		result := make([]FeedbackSummary, 0, len(rows))
		for _, row := range rows {
			result = append(result, FeedbackSummary{
				FilePath:  row.FilePath,
				Upvotes:   row.Upvotes,
				Downvotes: row.Downvotes,
			})
		}
		return result, nil
	}

	rows, err := s.repo.ListFeedbackByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list feedback by project: %w", err)
	}
	result := make([]FeedbackSummary, 0, len(rows))
	for _, row := range rows {
		result = append(result, FeedbackSummary{
			FilePath:  row.FilePath,
			Upvotes:   row.Upvotes,
			Downvotes: row.Downvotes,
		})
	}
	return result, nil
}

// AddComment creates a new comment on a file path.
func (s *Service) AddComment(ctx context.Context, projectID uuid.UUID, orgID string, params AddCommentParams) (*Comment, error) {
	if params.FilePath == "" {
		return nil, ErrEmptyFilePath
	}
	if params.Content == "" {
		return nil, ErrEmptyContent
	}
	if !validAuthorTypes[params.AuthorType] {
		return nil, ErrInvalidAuthorType
	}

	c, err := s.repo.CreateComment(ctx, repo.CreateCommentParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
		FilePath:       params.FilePath,
		AuthorID:       params.AuthorID,
		AuthorType:     params.AuthorType,
		Content:        params.Content,
	})
	if err != nil {
		return nil, fmt.Errorf("create comment: %w", err)
	}
	return &c, nil
}

// ListComments returns all non-deleted comments for a file, ordered by created_at ascending.
func (s *Service) ListComments(ctx context.Context, projectID uuid.UUID, filePath string) ([]Comment, error) {
	comments, err := s.repo.ListComments(ctx, repo.ListCommentsParams{
		ProjectID: projectID,
		FilePath:  filePath,
	})
	if err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	return comments, nil
}
