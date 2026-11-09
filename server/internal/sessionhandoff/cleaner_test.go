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
	rows       []ExpiredUnread
	listErr    error
	deleteErr  error
	claimLost  bool
	deletedIDs []uuid.UUID
}

func (s *cleanerRepositoryStub) ListExpiredUnread(context.Context, *ExpiredUnreadCursor, int32) ([]ExpiredUnread, error) {
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

func TestExpiredUnreadCleanerClean(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	repository := &cleanerRepositoryStub{rows: []ExpiredUnread{{ID: id, BlobURL: "s3://handoffs/expired.md"}}}
	blobs := &blobDeleterStub{}
	cleaner := NewExpiredUnreadCleaner(repository, blobs)

	more, err := cleaner.Clean(t.Context())
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

	_, err := cleaner.Clean(t.Context())
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

	more, err := cleaner.Clean(t.Context())
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

	_, err := NewExpiredUnreadCleaner(repository, blobs).Clean(t.Context())
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

	_, err := cleaner.Clean(t.Context())
	require.ErrorContains(t, err, "database unavailable")
	require.Len(t, blobs.deleted, 1)
	require.Empty(t, repository.deletedIDs)

	repository.deleteErr = nil
	_, err = cleaner.Clean(t.Context())
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
	_, err := NewExpiredUnreadCleaner(repository, blobs).Clean(t.Context())
	require.NoError(t, err)
	require.Empty(t, blobs.deleted, "cleaner must not delete after losing the conditional claim")
	require.Empty(t, repository.deletedIDs)
}
