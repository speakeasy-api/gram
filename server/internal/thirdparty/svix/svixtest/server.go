package svixtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/must"
	"github.com/stretchr/testify/mock"
	"github.com/svix/svix-webhooks/go/models"
)

// HTTPStatusError is a sentinel error that overrides the default 500 status
// code returned by mock handlers. Return it from a mock method to simulate
// a specific HTTP error code (e.g. 400, 403, 429).
type HTTPStatusError struct {
	Code int
	Msg  string
}

func (e *HTTPStatusError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	return http.StatusText(e.Code)
}

type MockServer struct {
	mock.Mock
	logger *slog.Logger
	mux    *http.ServeMux
	srv    *httptest.Server
}

func NewMockServer(logger *slog.Logger) *MockServer {
	var m MockServer

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/app", m.handleGetOrCreateApp)
	mux.HandleFunc("POST /api/v1/app/{appID}/msg", m.handleMessageCreate)
	mux.HandleFunc("POST /api/v1/auth/app-portal-access/{appID}", m.handleAppPortalAccessCreate)

	m.logger = logger
	m.mux = mux
	m.srv = httptest.NewServer(m.mux)

	return &m
}

func (m *MockServer) GetOrCreateApp(ctx context.Context, inp *models.ApplicationIn) (_ *models.ApplicationOut, created bool, err error) {
	args := m.Called(ctx, inp)

	app, _ := args.Get(0).(*models.ApplicationOut)
	return app, args.Bool(1), args.Error(2)
}

func (m *MockServer) handleGetOrCreateApp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var inp models.ApplicationIn
	if err := json.NewDecoder(r.Body).Decode(&inp); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	out, created, err := m.GetOrCreateApp(ctx, &inp)
	if err != nil {
		code := http.StatusInternalServerError
		var httpErr *HTTPStatusError
		if errors.As(err, &httpErr) {
			code = httpErr.Code
		}
		http.Error(w, err.Error(), code)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(conv.Ternary(created, http.StatusCreated, http.StatusOK))
	if err := json.NewEncoder(w).Encode(out); err != nil {
		m.logger.ErrorContext(ctx, "failed to write mock svix response", attr.SlogError(err))
		return
	}
}

func (m *MockServer) CreateMessage(ctx context.Context, inp *models.MessageIn) (*models.MessageOut, error) {
	args := m.Called(ctx, inp)

	msg, _ := args.Get(0).(*models.MessageOut)
	return msg, args.Error(1)
}

func (m *MockServer) handleMessageCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var inp models.MessageIn
	if err := json.NewDecoder(r.Body).Decode(&inp); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	out, err := m.CreateMessage(ctx, &inp)
	if err != nil {
		code := http.StatusInternalServerError
		var httpErr *HTTPStatusError
		if errors.As(err, &httpErr) {
			code = httpErr.Code
		}
		http.Error(w, err.Error(), code)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(out); err != nil {
		m.logger.ErrorContext(ctx, "failed to write mock svix response", attr.SlogError(err))
		return
	}
}

func (m *MockServer) CreateAppPortalSession(ctx context.Context, appID string) (*models.AppPortalAccessOut, error) {
	args := m.Called(ctx, appID)

	session, _ := args.Get(0).(*models.AppPortalAccessOut)
	return session, args.Error(1)
}

func (m *MockServer) handleAppPortalAccessCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	appID := r.PathValue("appID")

	out, err := m.CreateAppPortalSession(ctx, appID)
	if err != nil {
		code := http.StatusInternalServerError
		var httpErr *HTTPStatusError
		if errors.As(err, &httpErr) {
			code = httpErr.Code
		}
		http.Error(w, err.Error(), code)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(out); err != nil {
		m.logger.ErrorContext(ctx, "failed to write mock svix response", attr.SlogError(err))
		return
	}
}

func (m *MockServer) URL() *url.URL {
	return must.Value(url.Parse(m.srv.URL))
}

func (m *MockServer) Close() {
	m.srv.Close()
}
