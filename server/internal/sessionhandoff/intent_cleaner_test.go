package sessionhandoff

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type uploadIntentRepositoryStub struct {
	rows       []UploadIntent
	referenced map[uuid.UUID]bool
	cleared    []uuid.UUID
	restored   []uuid.UUID
	quarantine []uuid.UUID
}

func (s *uploadIntentRepositoryStub) ListStaleUploadIntents(context.Context, time.Time, *UploadIntentCursor, int32) ([]UploadIntent, error) {
	return s.rows, nil
}
func (s *uploadIntentRepositoryStub) ClaimStaleUploadIntent(_ context.Context, row UploadIntent, _ time.Time) (UploadIntent, bool, error) {
	return row, true, nil
}
func (s *uploadIntentRepositoryStub) BlobIsReferenced(_ context.Context, row UploadIntent) (bool, error) {
	return s.referenced[row.ID], nil
}
func (s *uploadIntentRepositoryStub) RestoreLinkedUpload(_ context.Context, row UploadIntent) error {
	s.restored = append(s.restored, row.ID)
	return nil
}
func (s *uploadIntentRepositoryStub) ClearUploadIntent(_ context.Context, row UploadIntent) error {
	s.cleared = append(s.cleared, row.ID)
	return nil
}
func (s *uploadIntentRepositoryStub) QuarantineUploadIntent(_ context.Context, row UploadIntent, _ string) error {
	s.quarantine = append(s.quarantine, row.ID)
	return nil
}

func TestUploadIntentCleanerPreservesReferencedBlobAndDeletesOrphan(t *testing.T) {
	t.Parallel()
	referencedID, orphanID := uuid.New(), uuid.New()
	repository := &uploadIntentRepositoryStub{
		rows: []UploadIntent{
			{ID: referencedID, ProjectID: uuid.New(), BlobURL: "s3://handoffs/referenced.md", CreatedAt: time.Now().Add(-time.Hour)},
			{ID: orphanID, ProjectID: uuid.New(), BlobURL: "s3://handoffs/orphan.md", CreatedAt: time.Now().Add(-time.Hour)},
		},
		referenced: map[uuid.UUID]bool{referencedID: true},
	}
	blobs := &blobDeleterStub{}
	_, more, err := NewUploadIntentCleaner(repository, blobs).CleanAfter(t.Context(), time.Now(), nil)
	require.NoError(t, err)
	require.False(t, more)
	require.Len(t, blobs.deleted, 1)
	require.Equal(t, "s3://handoffs/orphan.md", blobs.deleted[0].String())
	require.Equal(t, []uuid.UUID{orphanID}, repository.cleared)
	require.Equal(t, []uuid.UUID{referencedID}, repository.restored)
}

func TestUploadIntentCleanerQuarantinesMalformedWithoutRetryError(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	repository := &uploadIntentRepositoryStub{rows: []UploadIntent{{ID: id, ProjectID: uuid.New(), BlobURL: "://bad", CreatedAt: time.Now().Add(-time.Hour)}}}
	_, _, err := NewUploadIntentCleaner(repository, &blobDeleterStub{}).CleanAfter(t.Context(), time.Now(), nil)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{id}, repository.quarantine)
}
