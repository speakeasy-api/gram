package adminmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	admingen "github.com/speakeasy-api/gram/server/gen/admin_assets"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type fakePlatformImages struct {
	mu       sync.Mutex
	calls    int
	data     []byte
	operator string
}

func (f *fakePlatformImages) StorePlatformImage(_ context.Context, _ *slog.Logger, operatorEmail string, reader io.Reader, _ int64, contentType string) (*admingen.UploadImageResult, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.data, f.operator = data, operatorEmail
	return &admingen.UploadImageResult{Asset: &admingen.Asset{ID: "6f1c3a52-6b8e-4d5e-9a51-2b1f0f6d7c11", Kind: "image", Sha256: "stored-hash", ContentType: contentType, ContentLength: int64(len(data))}}, nil
}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, width, height))))
	return buf.Bytes()
}

type platformImageHarness struct {
	writer *platformImageWriter
	tools  *writeTools
	images *fakePlatformImages
	mux    *http.ServeMux
}

func newPlatformImageHarness(t *testing.T, f proposalFixture) platformImageHarness {
	t.Helper()
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationPublishPlatformImage: true}} //nolint:exhaustive // Only the reviewed global operation is enabled.
	images := &fakePlatformImages{}
	writer := &platformImageWriter{store: f.store, cache: testenv.NewMemoryCache(), images: images, siteURL: &url.URL{Scheme: "https", Host: "app.example.test"}, writes: writes, baseURL: "https://staff.example.test" + Path, logger: testenv.NewLogger(t), now: time.Now}
	tools := newWriteTools(f.store, writes, writer.baseURL, map[WriteOperation]operationWriter{OperationPublishPlatformImage: writer}) //nolint:exhaustive // Only the reviewed operation is dispatched.
	mux := http.NewServeMux()
	mux.Handle("PUT "+Path+"/uploads/{id}/{token}", writer.UploadHandler())
	return platformImageHarness{writer: writer, tools: tools, images: images, mux: mux}
}

func (h platformImageHarness) put(t *testing.T, uploadURL string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	parsed, err := url.Parse(uploadURL)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPut, parsed.Path, bytes.NewReader(body))
	response := httptest.NewRecorder()
	h.mux.ServeHTTP(response, request)
	return response
}

func TestPlatformImageUploadApproveExecuteAndReplay(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_platform_image")
	h := newPlatformImageHarness(t, f)
	ctx := writeContext(t, f)
	image := testPNG(t, 3, 2)

	upload, err := h.writer.prepare(ctx)
	require.NoError(t, err)
	require.Equal(t, http.MethodPut, upload.Method)
	require.True(t, strings.HasPrefix(upload.UploadURL, "https://staff.example.test"+Path+"/uploads/"))

	response := h.put(t, upload.UploadURL, image)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var proposal ProposalOutput
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &proposal))
	require.Equal(t, string(ProposalPendingApproval), proposal.Status)
	require.Equal(t, string(OperationPublishPlatformImage), proposal.Operation)
	require.NotEmpty(t, proposal.ApprovalURL)

	again := h.put(t, upload.UploadURL, image)
	require.Equal(t, http.StatusGone, again.Code, "an upload URL accepts one upload")

	proposalID, err := uuid.Parse(proposal.ProposalID)
	require.NoError(t, err)
	stored, err := f.store.GetForOwner(t.Context(), proposalID, f.owner)
	require.NoError(t, err)
	view, err := h.writer.view(t.Context(), stored)
	require.NoError(t, err)
	require.True(t, view.PlatformGlobal)
	require.True(t, strings.HasPrefix(string(view.Image), "data:image/png;base64,"))
	require.Equal(t, "3×2", view.Changes[2].After)

	_, err = h.tools.execute(ctx, ProposalIDInput{ProposalID: proposal.ProposalID})
	require.ErrorIs(t, err, ErrProposalNotApproved)
	_, err = f.store.Approve(t.Context(), proposalID, f.owner.SubjectURN, stored.ProposalDigest, time.Now(), allowProposalBrowser, h.writer.revalidate)
	require.NoError(t, err)
	result, err := h.tools.execute(ctx, ProposalIDInput{ProposalID: proposal.ProposalID})
	require.NoError(t, err)
	require.Equal(t, string(ProposalSucceeded), result.Status)
	var receipt platformImageReceipt
	require.NoError(t, json.Unmarshal(result.Result, &receipt))
	require.Equal(t, "https://app.example.test/rpc/assets.serveImage?id=6f1c3a52-6b8e-4d5e-9a51-2b1f0f6d7c11", receipt.URL)
	require.Equal(t, image, h.images.data)
	require.Equal(t, "staff@example.test", h.images.operator)

	result, err = h.tools.execute(ctx, ProposalIDInput{ProposalID: proposal.ProposalID})
	require.NoError(t, err)
	require.True(t, result.Replay)
	require.Equal(t, 1, h.images.calls, "a replayed receipt does not store again")
}

func TestPlatformImageUploadRejectsBadRequests(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_platform_image_rejects")
	h := newPlatformImageHarness(t, f)
	ctx := writeContext(t, f)

	upload, err := h.writer.prepare(ctx)
	require.NoError(t, err)
	parsed, err := url.Parse(upload.UploadURL)
	require.NoError(t, err)
	wrongToken := strings.TrimSuffix(parsed.Path, parsed.Path[strings.LastIndex(parsed.Path, "/")+1:]) + "wrong"
	require.Equal(t, http.StatusNotFound, h.put(t, "https://staff.example.test"+wrongToken, testPNG(t, 1, 1)).Code)
	require.Equal(t, http.StatusNotFound, h.put(t, "https://staff.example.test"+Path+"/uploads/"+uuid.NewString()+"/token", testPNG(t, 1, 1)).Code)

	svg, err := h.writer.prepare(ctx)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnsupportedMediaType, h.put(t, svg.UploadURL, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)).Code)
	require.Equal(t, http.StatusGone, h.put(t, svg.UploadURL, testPNG(t, 1, 1)).Code, "a failed upload still spends the URL")

	large, err := h.writer.prepare(ctx)
	require.NoError(t, err)
	require.Equal(t, http.StatusRequestEntityTooLarge, h.put(t, large.UploadURL, make([]byte, maxPlatformImageBytes+1)).Code)

	h.writer.writes = WriteConfig{}
	_, err = h.writer.prepare(ctx)
	require.ErrorIs(t, err, ErrWriteDisabled)
	require.Equal(t, http.StatusForbidden, h.put(t, upload.UploadURL, testPNG(t, 1, 1)).Code)
}

func TestPlatformImageProposalInvalidatedWhenBytesExpire(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_platform_image_expired")
	h := newPlatformImageHarness(t, f)
	ctx := writeContext(t, f)

	upload, err := h.writer.prepare(ctx)
	require.NoError(t, err)
	response := h.put(t, upload.UploadURL, testPNG(t, 1, 1))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var proposal ProposalOutput
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &proposal))
	proposalID, err := uuid.Parse(proposal.ProposalID)
	require.NoError(t, err)
	stored, err := f.store.GetForOwner(t.Context(), proposalID, f.owner)
	require.NoError(t, err)

	var args platformImageArguments
	require.NoError(t, json.Unmarshal(stored.Arguments, &args))
	require.NoError(t, h.writer.cache.Delete(t.Context(), platformImageUploadPrefix+args.UploadID))
	_, err = h.writer.view(t.Context(), stored)
	require.ErrorIs(t, err, ErrProposalInvalidated)
	_, err = f.store.Approve(t.Context(), proposalID, f.owner.SubjectURN, stored.ProposalDigest, time.Now(), allowProposalBrowser, h.writer.revalidate)
	require.Error(t, err)
	require.Zero(t, h.images.calls)
}
