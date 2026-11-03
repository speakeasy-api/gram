package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

type issuerLookupFailure struct {
	usersessionsrepo.DBTX
	err error
}

func (db issuerLookupFailure) QueryRow(context.Context, string, ...any) pgx.Row {
	return issuerFailureRow{err: db.err}
}

type issuerFailureRow struct{ err error }

func (r issuerFailureRow) Scan(...any) error { return r.err }

func TestOAuthSessionIssuerFailureMappings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"retired", pgx.ErrNoRows, http.StatusBadRequest, oautherr.CodeInvalidGrant},
		{"database outage", errors.New("private database failure"), http.StatusInternalServerError, oautherr.CodeServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			service, _, _ := newTokenDispatchTestService(t)
			queries := usersessionsrepo.New(issuerLookupFailure{err: tc.err})
			response := httptest.NewRecorder()
			oops.OAuthErrHandle(service.logger, func(w http.ResponseWriter, r *http.Request) error {
				_, err := service.issueSession(r.Context(), nil, queries, sessionIssuance{}, service.logger)
				return err
			}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/token", nil))
			require.Equal(t, tc.status, response.Code)
			var body oautherr.RFC6749Error
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.Equal(t, tc.code, body.Code)
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		})
	}
}

func TestOAuthMintSessionRejectsInvalidAuthorization(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		subject    urn.SessionSubject
		authorizer pgtype.Text
	}{
		{name: "agent without authorization", subject: urn.NewAgentSubject(uuid.New())},
		{name: "user with delegated authorization", subject: urn.NewUserSubject("user-test"), authorizer: pgtype.Text{String: "user-test", Valid: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			service, endpoint, _ := newTokenDispatchTestService(t)
			response := httptest.NewRecorder()
			oops.OAuthErrHandle(service.logger, func(w http.ResponseWriter, r *http.Request) error {
				_, err := service.mintSession(r.Context(), endpoint, &usersessionsrepo.UserSessionClient{}, nil, mintSessionParams{Policy: sessionIssuancePolicyIssuerScoped, Subject: tc.subject, AuthorizerUserID: tc.authorizer}, service.logger)
				return err
			}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/token", nil))
			require.Equal(t, http.StatusBadRequest, response.Code)
			require.Contains(t, response.Body.String(), `"error":"invalid_grant"`)
		})
	}
}

func TestOAuthMissingGrantTypeBeforeClientLookup(t *testing.T) {
	t.Parallel()
	for _, device := range []bool{false, true} {
		name := "mcp"
		if device {
			name = "device-agent"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			service, endpoint, _ := newTokenDispatchTestService(t)
			request := newTokenDispatchRequest(t, url.Values{})
			response := httptest.NewRecorder()
			var err error
			if device {
				err = service.HandleDeviceAgentToken(response, request)
			} else {
				err = service.ServeToken(response, request, endpoint)
			}
			require.NoError(t, err)
			require.Equal(t, http.StatusBadRequest, response.Code)
			require.Contains(t, response.Body.String(), `"error":"invalid_request"`)
		})
	}
}
