package sessionhandoff

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type cleanerRepositoryStub struct {
	projects   []uuid.UUID
	rows       []ExpiredUnread
	listErr    error
	deleteErr  error
	claimLost  bool
	deletedIDs []uuid.UUID
}

func (s *cleanerRepositoryStub) ListExpiredUnreadProjects(context.Context, *uuid.UUID, int32) ([]uuid.UUID, error) {
	return s.projects, s.listErr
}

func (s *cleanerRepositoryStub) ListExpiredUnread(context.Context, uuid.UUID, *ExpiredUnreadCursor, int32) ([]ExpiredUnread, error) {
	return s.rows, s.listErr
}

func (s *cleanerRepositoryStub) ClaimExpiredUnread(_ context.Context, row ExpiredUnread) (ExpiredUnread, bool, error) {
	return row, !s.claimLost, nil
}

func (s *cleanerRepositoryStub) FinalizeExpiredUnread(_ context.Context, row ExpiredUnread) (bool, error) {
	if s.deleteErr != nil {
		return false, s.deleteErr
	}
	s.deletedIDs = append(s.deletedIDs, row.ID)
	return true, nil
}

func (s *cleanerRepositoryStub) QuarantineExpiredUnread(_ context.Context, _ ExpiredUnread, _ string) error {
	return nil
}

type blobDeleterStub struct {
	err     error
	deleted []*url.URL
}

func (s *blobDeleterStub) Delete(_ context.Context, objectURL *url.URL) error {
	if s.err != nil {
		return s.err
	}
	s.deleted = append(s.deleted, objectURL)
	return nil
}

func TestExpiredUnreadCleanerListProjects(t *testing.T) {
	t.Parallel()
	projects := []uuid.UUID{uuid.New(), uuid.New()}
	listed, more, err := NewExpiredUnreadCleaner(&cleanerRepositoryStub{projects: projects}, nil).ListProjects(t.Context(), nil, 2)
	require.NoError(t, err)
	require.Equal(t, projects, listed)
	require.True(t, more)
}

func TestExpiredUnreadCleanerListProjectsRejectsMissingRepository(t *testing.T) {
	t.Parallel()
	for _, cleaner := range []*ExpiredUnreadCleaner{nil, NewExpiredUnreadCleaner(nil, nil)} {
		_, _, err := cleaner.ListProjects(t.Context(), nil, 100)
		require.ErrorContains(t, err, "not configured")
	}
}

func TestExpiredUnreadCleanerClean(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	repository := &cleanerRepositoryStub{rows: []ExpiredUnread{{ID: id, BlobURL: "s3://handoffs/expired.md"}}}
	blobs := &blobDeleterStub{}
	cleaner := NewExpiredUnreadCleaner(repository, blobs)

	_, more, err := cleaner.CleanProjectAfter(t.Context(), uuid.New(), nil)
	require.NoError(t, err)
	require.False(t, more)
	require.Equal(t, []uuid.UUID{id}, repository.deletedIDs)
	require.Len(t, blobs.deleted, 1)
	require.Equal(t, "s3://handoffs/expired.md", blobs.deleted[0].String())
}

func TestExpiredUnreadCleanerDoesNotDeleteRowWhenBlobDeleteFails(t *testing.T) {
	t.Parallel()
	repository := &cleanerRepositoryStub{rows: []ExpiredUnread{{ID: uuid.New(), BlobURL: "s3://handoffs/expired.md"}}}
	cleaner := NewExpiredUnreadCleaner(repository, &blobDeleterStub{err: errors.New("storage unavailable")})

	_, _, err := cleaner.CleanProjectAfter(t.Context(), uuid.New(), nil)
	require.ErrorContains(t, err, "blob")
	require.Empty(t, repository.deletedIDs)
}

func TestExpiredUnreadCleanerReportsFullBatch(t *testing.T) {
	t.Parallel()
	rows := make([]ExpiredUnread, expiredUnreadCleanupBatchSize)
	for i := range rows {
		rows[i] = ExpiredUnread{ID: uuid.New(), BlobURL: "s3://handoffs/expired.md"}
	}
	cleaner := NewExpiredUnreadCleaner(&cleanerRepositoryStub{rows: rows}, &blobDeleterStub{})

	_, more, err := cleaner.CleanProjectAfter(t.Context(), uuid.New(), nil)
	require.NoError(t, err)
	require.True(t, more)
}

func TestExpiredUnreadCleanerContinuesAfterMalformedURL(t *testing.T) {
	t.Parallel()
	badID, goodID := uuid.New(), uuid.New()
	repository := &cleanerRepositoryStub{rows: []ExpiredUnread{
		{ID: badID, BlobURL: "://malformed"},
		{ID: goodID, BlobURL: "s3://handoffs/good.md"},
	}}
	blobs := &blobDeleterStub{}

	_, _, err := NewExpiredUnreadCleaner(repository, blobs).CleanProjectAfter(t.Context(), uuid.New(), nil)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{goodID}, repository.deletedIDs)
	require.Len(t, blobs.deleted, 1)
}

func TestExpiredUnreadCleanerRetriesAfterDatabaseFailure(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	repository := &cleanerRepositoryStub{
		rows:      []ExpiredUnread{{ID: id, BlobURL: "s3://handoffs/retry.md"}},
		deleteErr: errors.New("database unavailable"),
	}
	blobs := &blobDeleterStub{}
	cleaner := NewExpiredUnreadCleaner(repository, blobs)

	_, _, err := cleaner.CleanProjectAfter(t.Context(), uuid.New(), nil)
	require.ErrorContains(t, err, "database unavailable")
	require.Len(t, blobs.deleted, 1)
	require.Empty(t, repository.deletedIDs)

	repository.deleteErr = nil
	_, _, err = cleaner.CleanProjectAfter(t.Context(), uuid.New(), nil)
	require.NoError(t, err)
	require.Len(t, blobs.deleted, 2)
	require.Equal(t, []uuid.UUID{id}, repository.deletedIDs)
}

func TestExpiredUnreadCleanerConsumerClaimWinsRace(t *testing.T) {
	t.Parallel()
	repository := &cleanerRepositoryStub{
		rows:      []ExpiredUnread{{ID: uuid.New(), ProjectID: uuid.New(), BlobURL: "s3://handoffs/live.md", ExpiresAt: time.Now().Add(-time.Minute)}},
		claimLost: true,
	}
	blobs := &blobDeleterStub{}
	_, _, err := NewExpiredUnreadCleaner(repository, blobs).CleanProjectAfter(t.Context(), uuid.New(), nil)
	require.NoError(t, err)
	require.Empty(t, blobs.deleted, "cleaner must not delete after losing the conditional claim")
	require.Empty(t, repository.deletedIDs)
}
