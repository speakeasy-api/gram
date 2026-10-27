package oops_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/oauthui"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestBrowserErrHandlePreservesPublicError(t *testing.T) {
	t.Parallel()
	public := oops.E(oops.CodeForbidden, errors.New("private database detail"), "public refusal")
	annotated := oops.WithBrowserReason(public, oauthui.ReasonExpired)
	handler := func(http.ResponseWriter, *http.Request) error { return fmt.Errorf("validate request: %w", annotated) }
	logger := testenv.NewLogger(t)
	request := httptest.NewRequest(http.MethodGet, "/connect", nil)
	request.Header.Set("Accept", "application/json")
	browser := httptest.NewRecorder()
	oops.BrowserErrHandle(logger, handler).ServeHTTP(browser, request)
	body := oauthtest.RequireAuthorizationErrorPage(t, browser, http.StatusForbidden)
	require.Equal(t, string(oops.CodeForbidden), body.Code)
	require.Equal(t, "public refusal", body.Description)
	require.Contains(t, browser.Body.String(), public.AsGoa(t.Context()).ID)
	require.Contains(t, browser.Body.String(), "link expired or was already used")
	require.NotContains(t, browser.Body.String(), "private database detail")

	machine := httptest.NewRecorder()
	original := httptest.NewRecorder()
	request.Header.Set("Accept", "text/html")
	oops.ErrHandle(logger, handler).ServeHTTP(machine, request)
	oops.ErrHandle(logger, func(http.ResponseWriter, *http.Request) error { return public }).ServeHTTP(original, request)
	require.Equal(t, original.Code, machine.Code)
	require.Equal(t, original.Header(), machine.Header())
	require.JSONEq(t, original.Body.String(), machine.Body.String())
}

func TestBrowserErrHandleMasksUnexpectedError(t *testing.T) {
	t.Parallel()
	response := httptest.NewRecorder()
	oops.BrowserErrHandle(testenv.NewLogger(t), func(http.ResponseWriter, *http.Request) error {
		return errors.New("private connection string")
	}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/authorize", nil))
	body := oauthtest.RequireAuthorizationErrorPage(t, response, http.StatusInternalServerError)
	require.Equal(t, string(oops.CodeUnexpected), body.Code)
	require.Equal(t, oops.CodeUnexpected.UserMessage(), body.Description)
	require.NotContains(t, response.Body.String(), "private connection string")
}

func TestBrowserErrHandleDoesNotAppendAfterResponse(t *testing.T) {
	t.Parallel()
	response := httptest.NewRecorder()
	oops.BrowserErrHandle(testenv.NewLogger(t), func(w http.ResponseWriter, r *http.Request) error {
		http.Redirect(w, r, "/safe", http.StatusSeeOther)
		return errors.New("late failure")
	}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/connect", nil))
	require.Equal(t, http.StatusSeeOther, response.Code)
	require.Equal(t, "/safe", response.Header().Get("Location"))
	require.NotContains(t, response.Body.String(), "Technical details")
}
