package hooks

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	ahp "github.com/agenthooksprotocol/go-sdk"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

func ahpUploadRequest(t *testing.T, h http.Handler, content []byte, digest string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, ahpUploadPath, bytes.NewReader(content))
	r.Header.Set("Content-Type", "application/octet-stream")
	r.Header.Set("Content-Length", strconv.Itoa(len(content)))
	r.Header.Set("AHP-Content-SHA256", digest)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func ahpDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// ahpUploadRef uploads content and returns the receiver reference.
func ahpUploadRef(t *testing.T, h http.Handler, content []byte) string {
	t.Helper()
	w := ahpUploadRequest(t, h, content, ahpDigest(content), ahpAuthHeaders)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	receipt := ahp.ParseContentUploadReceipt(w.Body.Bytes())
	require.True(t, receipt.OK, w.Body.String())
	var decoded struct {
		Ref    string `json:"ref"`
		Size   int    `json:"size"`
		SHA256 string `json:"sha256"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &decoded))
	require.Equal(t, len(content), decoded.Size)
	require.Equal(t, ahpDigest(content), decoded.SHA256)
	return decoded.Ref
}

func ahpTestAttachmentPart(id, mediaType, ref string) map[string]any {
	return map[string]any{"id": id, "kind": "attachment", "mediaType": mediaType, "selection": "body", "body": map[string]any{"ref": ref}}
}

func TestAHPUploadRejectsInvalidUploads(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.auth = &ahpTestAuthorizer{authCtx: hookAuthContext(t, ctx)}
	h := ti.service.ahpUploadHandler()
	content := []byte("attachment body")

	w := ahpUploadRequest(t, h, content, ahpDigest(content), map[string]string{"Gram-Project": "chosen"})
	require.Equal(t, http.StatusUnauthorized, w.Code)

	w = ahpUploadRequest(t, h, content, ahpDigest([]byte("other")), ahpAuthHeaders)
	require.Equal(t, http.StatusBadRequest, w.Code, "digest mismatch")

	w = ahpUploadRequest(t, h, content, "not-a-digest", ahpAuthHeaders)
	require.Equal(t, http.StatusBadRequest, w.Code, "invalid digest framing")

	oversized := bytes.Repeat([]byte("a"), ahpMaxUploadBytes+1)
	w = ahpUploadRequest(t, h, oversized, ahpDigest(oversized), ahpAuthHeaders)
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

func TestAHPInboundAttachmentPersistsAsPromptAttachment(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx := hookAuthContext(t, ctx)
	ti.service.auth = &ahpTestAuthorizer{authCtx: authCtx}
	eventHandler, err := ti.service.ahpHandler()
	require.NoError(t, err)
	uploadHandler := ti.service.ahpUploadHandler()

	content := []byte("<notes>MARKER_ahp_attachment</notes>\n")
	ref := ahpUploadRef(t, uploadHandler, content)
	require.Contains(t, ref, ahpUploadRefPrefix)
	binary := []byte{0x25, 0x50, 0x44, 0x46, 0x00, 0xff, 0xfe}
	binaryRef := ahpUploadRef(t, uploadHandler, binary)

	event := ahpTestInbound()
	sessionID := uuid.NewString()
	event["session"] = map[string]any{"id": sessionID}
	eventID := uuid.NewString()
	event["id"] = eventID
	event["message"] = map[string]any{"sender": "user", "channel": "cli", "messages": []any{
		map[string]any{"id": "message", "role": "user", "parts": []any{
			ahpTestPart("Summarize the attached notes"),
			ahpTestAttachmentPart("notes", "application/xml", ref),
			ahpTestAttachmentPart("binary", "application/pdf", binaryRef),
			ahpTestAttachmentPart("foreign", "application/xml", "urn:example:host:report"),
		}},
	}}
	w := ahpRequest(t, eventHandler, "hooks/observe", event, nil, ahpAuthHeaders)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	chatID := sessionIDToUUID(sessionID)
	msgs, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{ChatID: chatID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.Equal(t, "Summarize the attached notes", msgs[0].Content)

	parts, err := chatRepo.New(ti.conn).ListChatContentPartsByChatID(ctx, chatRepo.ListChatContentPartsByChatIDParams{
		ChatID:               chatID,
		ProjectID:            *authCtx.ProjectID,
		ParentChatMessageIds: []uuid.UUID{msgs[0].ID},
	})
	require.NoError(t, err)
	require.Len(t, parts, 1, "only the resolvable text attachment persists")
	part := parts[0]
	require.Equal(t, message.PromptAttachment, part.Kind)
	require.Equal(t, msgs[0].ID, part.ParentChatMessageID.UUID)
	require.Equal(t, "example-harness", part.Source.String)
	require.Equal(t, ahpAttachmentEntryID("https://harness.example/runtime", eventID, "message", "notes"), part.ExternalID.String)
	assetURL, err := url.Parse(part.ContentAssetUrl)
	require.NoError(t, err)
	reader, err := ti.assetStorage.Read(ctx, assetURL)
	require.NoError(t, err)
	stored, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.Equal(t, content, stored)
}

func TestAHPAttachmentOnlyMessagePersists(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx := hookAuthContext(t, ctx)
	ti.service.auth = &ahpTestAuthorizer{authCtx: authCtx}
	eventHandler, err := ti.service.ahpHandler()
	require.NoError(t, err)

	ref := ahpUploadRef(t, ti.service.ahpUploadHandler(), []byte("only an attachment"))
	event := ahpTestInbound()
	sessionID := uuid.NewString()
	event["session"] = map[string]any{"id": sessionID}
	event["message"] = map[string]any{"sender": "user", "channel": "cli", "messages": []any{
		map[string]any{"id": "message", "role": "user", "parts": []any{ahpTestAttachmentPart("notes", "application/xml", ref)}},
	}}
	w := ahpRequest(t, eventHandler, "hooks/intercept", event, []string{"deny"}, ahpAuthHeaders)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.True(t, ahp.ParseInterceptResponse(w.Body.Bytes()).OK, w.Body.String())

	parts, err := chatRepo.New(ti.conn).ListChatContentPartsByChatID(ctx, chatRepo.ListChatContentPartsByChatIDParams{
		ChatID:               sessionIDToUUID(sessionID),
		ProjectID:            *authCtx.ProjectID,
		ParentChatMessageIds: []uuid.UUID{},
	})
	require.NoError(t, err)
	require.Len(t, parts, 1)
	require.Equal(t, message.PromptAttachment, parts[0].Kind)
}

func TestAHPAttachmentReferencesAreProjectScoped(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx := hookAuthContext(t, ctx)
	ti.service.auth = &ahpTestAuthorizer{authCtx: authCtx}
	ref := ahpUploadRef(t, ti.service.ahpUploadHandler(), []byte("project scoped"))

	raw := ahpTestInbound()
	raw["message"] = map[string]any{"sender": "user", "channel": "cli", "messages": []any{
		map[string]any{"id": "message", "role": "user", "parts": []any{ahpTestAttachmentPart("notes", "application/xml", ref)}},
	}}
	b, err := json.Marshal(raw)
	require.NoError(t, err)
	var e ahp.UserMessageInboundEvent
	require.NoError(t, json.Unmarshal(b, &e))

	attachments, err := ti.service.ahpPromptAttachments(contextvalues.SetAuthContext(ctx, authCtx), &e)
	require.NoError(t, err)
	require.Len(t, attachments, 1)
	other := *authCtx
	otherProject := uuid.New()
	other.ProjectID = &otherProject
	attachments, err = ti.service.ahpPromptAttachments(contextvalues.SetAuthContext(ctx, &other), &e)
	require.ErrorIs(t, err, errAHPAttachmentUnavailable)
	require.Empty(t, attachments)
}

func TestAHPUploadStagesEveryReceipt(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx := hookAuthContext(t, ctx)
	ti.service.auth = &ahpTestAuthorizer{authCtx: authCtx}
	for _, content := range [][]byte{[]byte("<notes>text</notes>"), {0, 0xff, 0xfe}, {}} {
		ref := ahpUploadRef(t, ti.service.ahpUploadHandler(), content)
		var staged ahpStagedUpload
		key := ahpUploadCacheKey(*authCtx.ProjectID, strings.TrimPrefix(ref, ahpUploadRefPrefix))
		require.NoError(t, ti.service.cache.Get(ctx, key, &staged))
		require.True(t, bytes.Equal(content, staged.Content))
		ttl, err := ti.redisClient.TTL(ctx, key).Result()
		require.NoError(t, err)
		require.Positive(t, ttl)
		require.LessOrEqual(t, ttl, ahpUploadTTL)
	}
}

func TestAHPUploadQuotaIsSharedAndProjectScoped(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx := hookAuthContext(t, ctx)
	ti.service.auth = &ahpTestAuthorizer{authCtx: authCtx}
	limit, err := ti.service.ahpUploadLimiter.AllowN(ctx, authCtx.ProjectID.String(), ahpUploadBurst)
	require.NoError(t, err)
	require.True(t, limit.Allowed)
	// A separate limiter instance must see the same quota, as another replica does.
	ti.service.ahpUploadLimiter = ratelimit.New(ratelimit.NewRedisStore(ti.redisClient), "ahp-upload",
		ratelimit.Rate{Tokens: ahpUploadBurst, Interval: ahpUploadTTL, Burst: ahpUploadBurst})
	content := []byte("quota test")
	w := ahpUploadRequest(t, ti.service.ahpUploadHandler(), content, ahpDigest(content), ahpAuthHeaders)
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	retryAfter, err := strconv.Atoi(w.Header().Get("Retry-After"))
	require.NoError(t, err)
	require.Positive(t, retryAfter)
	other := *authCtx
	otherProject := uuid.New()
	other.ProjectID = &otherProject
	ti.service.auth = &ahpTestAuthorizer{authCtx: &other}
	ahpUploadRef(t, ti.service.ahpUploadHandler(), content)
}

func TestAHPUploadQuotaFailsClosed(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.auth = &ahpTestAuthorizer{authCtx: hookAuthContext(t, ctx)}
	client := redis.NewClient(ti.redisClient.Options())
	require.NoError(t, client.Close())
	ti.service.ahpUploadLimiter = ratelimit.New(ratelimit.NewRedisStore(client), "ahp-upload", ratelimit.PerMinute(1))
	content := []byte("quota unavailable")
	w := ahpUploadRequest(t, ti.service.ahpUploadHandler(), content, ahpDigest(content), ahpAuthHeaders)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Empty(t, w.Header().Get("Retry-After"))
}

// Fail only reads, leaving the rest of the real cache contract unchanged.
type ahpAttachmentReadFailure struct{ cache.Cache }

func (ahpAttachmentReadFailure) Get(context.Context, string, any) error {
	return errors.New("attachment cache unavailable")
}

func TestAHPInboundAttachmentUnavailable(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"missing", "read error", "cache absent"} {
		for _, prompt := range []string{"", "safe inline prompt"} {
			t.Run(failure+"/"+prompt, func(t *testing.T) {
				t.Parallel()
				ctx, ti := newTestHooksService(t)
				auth := hookAuthContext(t, ctx)
				ti.service.auth = &ahpTestAuthorizer{authCtx: auth}
				ref := ahpUploadRef(t, ti.service.ahpUploadHandler(), []byte("RESTRICTED_TEST_CONTENT"))
				switch failure {
				case "missing":
					require.NoError(t, ti.service.cache.Delete(ctx, ahpUploadCacheKey(*auth.ProjectID, strings.TrimPrefix(ref, ahpUploadRefPrefix))))
				case "read error":
					ti.service.cache = ahpAttachmentReadFailure{Cache: ti.service.cache}
				case "cache absent":
					ti.service.cache = nil
				}
				handler, err := ti.service.ahpHandler()
				require.NoError(t, err)
				input := ahpTestInbound()
				parts := []any{ahpTestAttachmentPart("notes", "application/xml", ref)}
				if prompt != "" {
					parts = append(parts, ahpTestPart(prompt))
				}
				input["message"] = map[string]any{"sender": "user", "channel": "cli", "messages": []any{map[string]any{"id": "message", "role": "user", "parts": parts}}}
				response := ahpRequest(t, handler, "hooks/intercept", input, []string{"deny"}, ahpAuthHeaders)
				require.Equal(t, http.StatusOK, response.Code)
				var envelope struct {
					Result json.RawMessage `json:"result"`
					Error  struct {
						Code int `json:"code"`
					} `json:"error"`
				}
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
				require.Equal(t, -32004, envelope.Error.Code, "missing attachment must not return a successful partial or empty scan")
				require.Empty(t, envelope.Result)
			})
		}
	}
}
