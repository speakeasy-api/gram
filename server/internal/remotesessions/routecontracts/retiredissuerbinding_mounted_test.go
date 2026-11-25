package routecontracts_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

func TestRetiredIssuerBindingRPCsAreNotMounted(t *testing.T) {
	t.Parallel()

	mux := goahttp.NewMuxer()
	remotesessions.Attach(mux, &remotesessions.Service{})
	usersessions.Attach(mux, &usersessions.Service{})

	for _, tc := range []struct {
		method string
		path   string
		status int
	}{
		{http.MethodPost, "/rpc/remoteSessionClients.attachUserSessionIssuer", http.StatusNotFound},
		{http.MethodPost, "/rpc/remoteSessionClients.detachUserSessionIssuer", http.StatusNotFound},
		{http.MethodGet, "/rpc/organizationUserSessionIssuers.listRemoteSessionClientBindings", http.StatusNotFound},
		// Wrong-method controls prove the retained paths are registered without
		// invoking a handler or requiring credentials or a database.
		{http.MethodGet, "/rpc/remoteSessionClients.create", http.StatusMethodNotAllowed},
		{http.MethodGet, "/rpc/remoteSessionClients.createCimd", http.StatusMethodNotAllowed},
		{http.MethodPost, "/rpc/remoteSessionClients.list", http.StatusMethodNotAllowed},
		{http.MethodGet, "/rpc/userSessionIssuers.update", http.StatusMethodNotAllowed},
		{http.MethodGet, "/rpc/remoteSessions.attachServerClientAssociation", http.StatusMethodNotAllowed},
		{http.MethodGet, "/rpc/remoteSessions.detachServerClientAssociation", http.StatusMethodNotAllowed},
		{http.MethodPost, "/rpc/remoteSessions.listServerClientAssociations", http.StatusMethodNotAllowed},
		{http.MethodPost, "/rpc/remoteSessions.listBindings", http.StatusMethodNotAllowed},
		{http.MethodGet, "/rpc/remoteSessions.attachBinding", http.StatusMethodNotAllowed},
		{http.MethodGet, "/rpc/remoteSessions.detachBinding", http.StatusMethodNotAllowed},
		{http.MethodPost, "/rpc/organizationUserSessionIssuers.listMcpServers", http.StatusMethodNotAllowed},
	} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			res := httptest.NewRecorder()
			mux.ServeHTTP(res, httptest.NewRequest(tc.method, tc.path, nil))
			require.Equal(t, tc.status, res.Code)
		})
	}
}
