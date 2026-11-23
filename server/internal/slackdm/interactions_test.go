package slackdm_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/slackdm"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const testSecret = "synthetic-signing-secret"

// recordingHandler records the bodies it is handed.
type recordingHandler struct {
	bodies []string
}

func (h *recordingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	h.bodies = append(h.bodies, string(body))
	w.WriteHeader(http.StatusAccepted)
}

func signedRequest(t *testing.T, body, secret string) *http.Request {
	t.Helper()
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("v0:" + timestamp + ":" + body))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, slackdm.InteractionsPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)
	req.Header.Set("X-Slack-Signature", "v0="+hex.EncodeToString(mac.Sum(nil)))
	return req
}

func clickBody(actionIDs ...string) string {
	actions := make([]string, 0, len(actionIDs))
	for _, id := range actionIDs {
		actions = append(actions, `{"action_id":"`+id+`","value":"x"}`)
	}
	return url.Values{"payload": {`{"type":"block_actions","actions":[` + strings.Join(actions, ",") + `]}`}}.Encode()
}

func newTestRouter(t *testing.T, secret string) (*slackdm.Router, *recordingHandler, *recordingHandler) {
	t.Helper()
	first, second := &recordingHandler{}, &recordingHandler{}
	router := slackdm.NewRouter(testenv.NewLogger(t), secret).
		Handle("first_", first).
		Handle("second_", second)
	return router, first, second
}

func TestRouterSendsClicksByActionIDPrefix(t *testing.T) {
	t.Parallel()
	router, first, second := newTestRouter(t, testSecret)

	firstBody, secondBody := clickBody("first_approve"), clickBody("second_deny")
	for _, body := range []string{firstBody, secondBody} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, signedRequest(t, body, testSecret))
		require.Equal(t, http.StatusAccepted, rec.Code)
	}
	require.Equal(t, []string{firstBody}, first.bodies, "the handler gets the body intact")
	require.Equal(t, []string{secondBody}, second.bodies)
}

func TestRouterAcknowledgesUnclaimedInteractions(t *testing.T) {
	t.Parallel()
	router, first, second := newTestRouter(t, testSecret)

	for _, body := range []string{
		clickBody("other_approve"),
		clickBody("first_approve", "second_deny"),
		url.Values{"payload": {`{"type":"view_submission"}`}}.Encode(),
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, signedRequest(t, body, testSecret))
		require.Equal(t, http.StatusOK, rec.Code, body)
	}
	require.Empty(t, first.bodies)
	require.Empty(t, second.bodies)
}

func TestRouterRejectsUnsignedUnclaimedInteractions(t *testing.T) {
	t.Parallel()
	router, _, _ := newTestRouter(t, testSecret)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, signedRequest(t, clickBody("other_approve"), "another-secret"))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRouterWithoutSecretRefusesUnclaimedInteractions(t *testing.T) {
	t.Parallel()
	router, _, _ := newTestRouter(t, "")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, signedRequest(t, clickBody("other_approve"), testSecret))
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRouterRejectsMalformedUnclaimedPayload(t *testing.T) {
	t.Parallel()
	router, _, _ := newTestRouter(t, testSecret)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, signedRequest(t, url.Values{"payload": {"not json"}}.Encode(), testSecret))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRouterRejectsOversizedBody(t *testing.T) {
	t.Parallel()
	router, first, _ := newTestRouter(t, testSecret)

	body := clickBody("first_approve") + "&pad=" + strings.Repeat("a", slackdm.MaxInteractionBytes)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, signedRequest(t, body, testSecret))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Empty(t, first.bodies)
}

func TestUnclaimedServesWhatAHandlerPassesOn(t *testing.T) {
	t.Parallel()
	router, _, _ := newTestRouter(t, testSecret)

	rec := httptest.NewRecorder()
	router.Unclaimed().ServeHTTP(rec, signedRequest(t, clickBody("first_approve"), testSecret))
	require.Equal(t, http.StatusOK, rec.Code)
}
