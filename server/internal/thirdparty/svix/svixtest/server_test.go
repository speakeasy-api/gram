package svixtest_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	svix "github.com/svix/svix-webhooks/go"
	"github.com/svix/svix-webhooks/go/models"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/svix/svixtest"
)

func newSDKClient(t *testing.T, serverURL *url.URL) *svix.Svix {
	t.Helper()

	noRetries := []time.Duration{}
	client, err := svix.New("test-token", &svix.SvixOptions{
		ServerUrl:     serverURL,
		RetrySchedule: &noRetries,
	})
	require.NoError(t, err, "create svix sdk client")
	return client
}

func TestMockServer_GetOrCreateApp_Created(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	srv := svixtest.NewMockServer(logger)
	t.Cleanup(srv.Close)

	uid := "app-uid-1"
	wantOut := &models.ApplicationOut{
		Id:        "app_123",
		Uid:       &uid,
		Name:      "test app",
		Metadata:  map[string]string{},
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	}

	srv.On("GetOrCreateApp", mock.Anything, mock.MatchedBy(func(in *models.ApplicationIn) bool {
		return in != nil && in.Name == "test app" && in.Uid != nil && *in.Uid == uid
	})).Return(wantOut, true, nil).Once()

	client := newSDKClient(t, srv.URL())

	got, err := client.Application.GetOrCreate(context.Background(), models.ApplicationIn{
		Name: "test app",
		Uid:  &uid,
	}, nil)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, wantOut.Id, got.Id)
	require.Equal(t, wantOut.Name, got.Name)
	require.NotNil(t, got.Uid)
	require.Equal(t, uid, *got.Uid)

	srv.AssertExpectations(t)
}

func TestMockServer_GetOrCreateApp_Existing(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	srv := svixtest.NewMockServer(logger)
	t.Cleanup(srv.Close)

	wantOut := &models.ApplicationOut{
		Id:        "app_existing",
		Name:      "existing",
		Metadata:  map[string]string{},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	srv.On("GetOrCreateApp", mock.Anything, mock.AnythingOfType("*models.ApplicationIn")).
		Return(wantOut, false, nil).Once()

	client := newSDKClient(t, srv.URL())

	got, err := client.Application.GetOrCreate(context.Background(), models.ApplicationIn{
		Name: "existing",
	}, nil)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "app_existing", got.Id)

	srv.AssertExpectations(t)
}

func TestMockServer_GetOrCreateApp_Error(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	srv := svixtest.NewMockServer(logger)
	t.Cleanup(srv.Close)

	srv.On("GetOrCreateApp", mock.Anything, mock.AnythingOfType("*models.ApplicationIn")).
		Return((*models.ApplicationOut)(nil), false, errors.New("boom")).Once()

	client := newSDKClient(t, srv.URL())

	_, err := client.Application.GetOrCreate(context.Background(), models.ApplicationIn{
		Name: "bad",
	}, nil)
	require.Error(t, err)

	var sErr *svix.Error
	require.ErrorAs(t, err, &sErr)
	require.Equal(t, http.StatusInternalServerError, sErr.Status())

	srv.AssertExpectations(t)
}

func TestMockServer_CreateMessage_Success(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	srv := svixtest.NewMockServer(logger)
	t.Cleanup(srv.Close)

	eventID := "evt_1"
	wantOut := &models.MessageOut{
		Id:        "msg_123",
		EventId:   &eventID,
		EventType: "user.created",
		Payload:   map[string]any{"hello": "world"},
		Timestamp: time.Now().UTC().Truncate(time.Second),
	}

	srv.On("CreateMessage", mock.Anything, mock.MatchedBy(func(in *models.MessageIn) bool {
		return in != nil && in.EventType == "user.created" &&
			in.EventId != nil && *in.EventId == eventID &&
			in.Payload["hello"] == "world"
	})).Return(wantOut, nil).Once()

	client := newSDKClient(t, srv.URL())

	got, err := client.Message.Create(context.Background(), "app_123", models.MessageIn{
		EventId:   &eventID,
		EventType: "user.created",
		Payload:   map[string]any{"hello": "world"},
	}, nil)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "msg_123", got.Id)
	require.Equal(t, "user.created", got.EventType)

	srv.AssertExpectations(t)
}

func TestMockServer_CreateMessage_Error(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	srv := svixtest.NewMockServer(logger)
	t.Cleanup(srv.Close)

	srv.On("CreateMessage", mock.Anything, mock.AnythingOfType("*models.MessageIn")).
		Return((*models.MessageOut)(nil), errors.New("publish failed")).Once()

	client := newSDKClient(t, srv.URL())

	_, err := client.Message.Create(context.Background(), "app_123", models.MessageIn{
		EventType: "user.deleted",
		Payload:   map[string]any{},
	}, nil)
	require.Error(t, err)

	var sErr *svix.Error
	require.ErrorAs(t, err, &sErr)
	require.Equal(t, http.StatusInternalServerError, sErr.Status())

	srv.AssertExpectations(t)
}

func TestMockServer_URL(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	srv := svixtest.NewMockServer(logger)
	t.Cleanup(srv.Close)

	require.NotEmpty(t, srv.URL())
	require.NotEmpty(t, srv.URL().String())
}

func TestMockServer_Close_StopsServer(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	srv := svixtest.NewMockServer(logger)

	u := srv.URL()
	srv.Close()

	// After close, requests to the URL should fail.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, u.JoinPath("/api/v1/app").String(), http.NoBody)
	require.NoError(t, err)

	httpClient := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := httpClient.Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err, "expected request after Close to fail")
}
