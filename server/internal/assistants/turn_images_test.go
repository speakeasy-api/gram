package assistants

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
)

type stubImageFetcher struct {
	images map[string]*slackapi.ImageFile
	calls  []string
}

func (s *stubImageFetcher) FetchImageFile(_ context.Context, fileID string, _ string) (*slackapi.ImageFile, error) {
	s.calls = append(s.calls, fileID)
	img, ok := s.images[fileID]
	if !ok {
		return nil, errors.New("file not found")
	}
	return img, nil
}

func stubImage(id string, size int) *slackapi.ImageFile {
	return &slackapi.ImageFile{
		FileID:   id,
		Name:     id + ".png",
		Title:    "",
		MimeType: "image/png",
		Data:     bytes.Repeat([]byte{0xAB}, size),
	}
}

func TestImageFileCandidatesFiltersAndCaps(t *testing.T) {
	t.Parallel()

	files := []slackFilePayload{
		{ID: "F1", Name: "", Title: "", Mimetype: "application/pdf", Size: 10, URLPrivateDownload: ""},
		{ID: "F2", Name: "", Title: "", Mimetype: "image/png", Size: 10, URLPrivateDownload: ""},
		{ID: "F3", Name: "", Title: "", Mimetype: "image/jpeg", Size: 10, URLPrivateDownload: ""},
		{ID: "F4", Name: "", Title: "", Mimetype: "image/gif", Size: 10, URLPrivateDownload: ""},
		{ID: "F5", Name: "", Title: "", Mimetype: "image/webp", Size: 10, URLPrivateDownload: ""},
		{ID: "F6", Name: "", Title: "", Mimetype: "image/png", Size: 10, URLPrivateDownload: ""},
		{ID: "", Name: "", Title: "", Mimetype: "image/png", Size: 10, URLPrivateDownload: ""},
	}

	got := imageFileCandidates(files)
	// Non-images and id-less entries drop; the trailing (newest) four stay.
	require.Len(t, got, maxTurnInlineImages)
	require.Equal(t, "F3", got[0].ID)
	require.Equal(t, "F6", got[3].ID)

	require.Empty(t, imageFileCandidates([]slackFilePayload{
		{ID: "F1", Name: "", Title: "", Mimetype: "text/plain", Size: 1, URLPrivateDownload: ""},
	}))
}

func TestFetchInlineImagePartsBuildsDataURIs(t *testing.T) {
	t.Parallel()

	fetcher := &stubImageFetcher{
		images: map[string]*slackapi.ImageFile{
			"F1": stubImage("F1", 16),
			"F2": stubImage("F2", 16),
		},
		calls: nil,
	}
	files := []slackFilePayload{
		{ID: "F1", Name: "", Title: "", Mimetype: "image/png", Size: 16, URLPrivateDownload: ""},
		{ID: "F2", Name: "", Title: "", Mimetype: "image/png", Size: 16, URLPrivateDownload: ""},
	}

	parts := fetchInlineImageParts(t.Context(), testenv.NewLogger(t), fetcher, "tok", files)
	require.Len(t, parts, 2)
	for _, part := range parts {
		require.Equal(t, contentPartTypeImageURL, part.Type)
		require.NotNil(t, part.ImageURL)
		require.Contains(t, part.ImageURL.URL, "data:image/png;base64,")
	}
}

func TestFetchInlineImagePartsSkipsFailuresAndContinues(t *testing.T) {
	t.Parallel()

	fetcher := &stubImageFetcher{
		images: map[string]*slackapi.ImageFile{"F2": stubImage("F2", 16)},
		calls:  nil,
	}
	files := []slackFilePayload{
		{ID: "F1", Name: "", Title: "", Mimetype: "image/png", Size: 16, URLPrivateDownload: ""},
		{ID: "F2", Name: "", Title: "", Mimetype: "image/png", Size: 16, URLPrivateDownload: ""},
	}

	parts := fetchInlineImageParts(t.Context(), testenv.NewLogger(t), fetcher, "tok", files)
	require.Equal(t, []string{"F1", "F2"}, fetcher.calls, "a failed download must not stop later files")
	require.Len(t, parts, 1)
}

func TestFetchInlineImagePartsEnforcesTurnByteBudget(t *testing.T) {
	t.Parallel()

	fetcher := &stubImageFetcher{
		images: map[string]*slackapi.ImageFile{
			// Declared sizes lie low so the budget check has to use actual
			// downloaded bytes.
			"F1": stubImage("F1", maxTurnInlineImageBytes-64),
			"F2": stubImage("F2", 128),
		},
		calls: nil,
	}
	files := []slackFilePayload{
		{ID: "F1", Name: "", Title: "", Mimetype: "image/png", Size: 1, URLPrivateDownload: ""},
		{ID: "F2", Name: "", Title: "", Mimetype: "image/png", Size: 1, URLPrivateDownload: ""},
		// Declared size alone already blows the remaining budget: skipped
		// without a download.
		{ID: "F3", Name: "", Title: "", Mimetype: "image/png", Size: maxTurnInlineImageBytes, URLPrivateDownload: ""},
	}

	parts := fetchInlineImageParts(t.Context(), testenv.NewLogger(t), fetcher, "tok", files)
	require.Len(t, parts, 1, "second image exceeds the remaining budget after the first")
	require.Equal(t, []string{"F1", "F2"}, fetcher.calls, "budget-exceeding declared size must skip the download")
}
