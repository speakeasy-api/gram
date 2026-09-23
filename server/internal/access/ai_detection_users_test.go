package access

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	accessserver "github.com/speakeasy-api/gram/server/gen/http/access/server"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestService_ListAIDetectionUsers_ExpandsTargetIntoUsers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx, orgID, _ := withUniqueDetectionOrg(t, ctx, ti)

	now := time.Now().UTC().Truncate(time.Second)
	// Alex: one device with a serial, one without, no version.
	seedAIDetection(t, ctx, ti, orgID, "cursor", "serial-1", "alex@example.com", "installed", "harness", "", now.Add(-2*time.Hour))
	seedAIDetection(t, ctx, ti, orgID, "cursor", "", "alex@example.com", "running", "harness", "", now.Add(-90*time.Minute))
	// Sam: two devices, both signals, two versions, the most recent sighting.
	seedAIDetection(t, ctx, ti, orgID, "cursor", "serial-2", "sam@example.com", "installed", "harness", "1.7.49", now.Add(-24*time.Hour))
	seedAIDetection(t, ctx, ti, orgID, "cursor", "serial-3", "sam@example.com", "installed", "harness", "1.7.52", now.Add(-time.Hour))
	seedAIDetection(t, ctx, ti, orgID, "cursor", "serial-3", "sam@example.com", "running", "harness", "", now)
	// Another target in this org, and this target in another org, stay out.
	seedAIDetection(t, ctx, ti, orgID, "ollama", "serial-1", "alex@example.com", "running", "local_model", "0.6.2", now)
	seedAIDetection(t, ctx, ti, "detections-test-org-"+uuid.NewString(), "cursor", "serial-9", "eve@example.com", "running", "harness", "", now)

	result, err := ti.service.ListAIDetectionUsers(ctx, &gen.ListAIDetectionUsersPayload{TargetID: "cursor", SessionToken: nil})
	require.NoError(t, err)

	require.Equal(t, "cursor", result.Detection.TargetID)
	require.Equal(t, "Cursor", result.Detection.DisplayName, "the target row is decorated like the inventory's")
	require.EqualValues(t, 2, result.Detection.UserCount)
	require.EqualValues(t, 3, result.Detection.DeviceCount)

	require.Len(t, result.Users, 2)
	sam := result.Users[0]
	require.Equal(t, "sam@example.com", sam.UserEmail, "most recently seen first")
	require.EqualValues(t, 2, sam.DeviceCount)
	require.Equal(t, []string{"installed", "running"}, sam.Signals)
	require.Equal(t, []string{"1.7.49", "1.7.52"}, sam.Versions)
	require.Equal(t, now.Add(-24*time.Hour).Format(time.RFC3339), sam.FirstSeen)
	require.Equal(t, now.Format(time.RFC3339), sam.LastSeen)

	alex := result.Users[1]
	require.Equal(t, "alex@example.com", alex.UserEmail)
	require.EqualValues(t, 1, alex.DeviceCount, "a device that reports no serial is attributed but not counted")
	require.Equal(t, []string{"installed", "running"}, alex.Signals)
	require.Empty(t, alex.Versions)
	require.Equal(t, now.Add(-2*time.Hour).Format(time.RFC3339), alex.FirstSeen)
	require.Equal(t, now.Add(-90*time.Minute).Format(time.RFC3339), alex.LastSeen)
}

func TestService_ListAIDetectionUsers_UnknownTargetIsNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx, orgID, _ := withUniqueDetectionOrg(t, ctx, ti)
	seedAIDetection(t, ctx, ti, orgID, "cursor", "serial-1", "alex@example.com", "installed", "harness", "", time.Now().UTC())

	_, err := ti.service.ListAIDetectionUsers(ctx, &gen.ListAIDetectionUsersPayload{TargetID: "ollama", SessionToken: nil})
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeNotFound, shareableErr.Code)
}

func TestService_ListAIDetectionUsers_RejectsInsufficientScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeProjectRead,
		Selector: authz.NewSelector(authz.ScopeProjectRead, "unrelated-project"),
	})

	_, err := ti.service.ListAIDetectionUsers(ctx, &gen.ListAIDetectionUsersPayload{TargetID: "cursor", SessionToken: nil})
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeForbidden, shareableErr.Code)
}

func TestService_ListAIDetectionUsers_RejectsUnauthenticatedCaller(t *testing.T) {
	t.Parallel()

	_, ti := newTestAccessService(t)

	_, err := ti.service.ListAIDetectionUsers(t.Context(), &gen.ListAIDetectionUsersPayload{TargetID: "cursor", SessionToken: nil})
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeUnauthorized, shareableErr.Code)
}

// The target id is validated at the HTTP boundary against the same pattern
// the scan-report ingest enforces, so a malformed id never reaches ClickHouse.
func TestListAIDetectionUsers_HTTPRejectsMalformedTargetID(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "/rpc/access.listAIDetectionUsers?target_id=Not%20A%20Target", nil)
	require.NoError(t, err)
	req.Header.Set("Gram-Session", "test-session")

	called := false
	handler := accessserver.NewListAIDetectionUsersHandler(
		func(_ context.Context, _ any) (any, error) {
			called = true
			return nil, nil
		},
		nil,
		goahttp.RequestDecoder,
		goahttp.ResponseEncoder,
		nil,
		nil,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	require.False(t, called, "a malformed target id must not reach the service")
}

// One person's linked alias emails fold into one row, as the inventory's user
// count folds them into one user. Without the fold the same person would be
// listed once per email.
func TestService_ListAIDetectionUsers_FoldsAliasEmailsIntoOnePerson(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx, orgID, _ := withUniqueDetectionOrg(t, ctx, ti)

	emailSuffix := uuid.NewString()
	workEmail := "employee-" + emailSuffix + "@example.com"
	aliasEmail := "employee-personal-" + emailSuffix + "@example.com"
	seedAIDetectionIdentity(t, ctx, ti, orgID, workEmail, "user-employee", workEmail)
	seedAIDetectionIdentity(t, ctx, ti, orgID, aliasEmail, "user-employee", workEmail)

	now := time.Now().UTC().Truncate(time.Second)
	seedAIDetection(t, ctx, ti, orgID, "cursor", "serial-1", workEmail, "installed", "harness", "1.7.49", now.Add(-48*time.Hour))
	seedAIDetection(t, ctx, ti, orgID, "cursor", "serial-2", aliasEmail, "running", "harness", "1.7.52", now)
	seedAIDetection(t, ctx, ti, orgID, "cursor", "serial-3", "outsider@example.com", "installed", "harness", "", now.Add(-time.Hour))

	result, err := ti.service.ListAIDetectionUsers(ctx, &gen.ListAIDetectionUsersPayload{TargetID: "cursor", SessionToken: nil})
	require.NoError(t, err)
	require.EqualValues(t, 2, result.Detection.UserCount)
	require.Len(t, result.Users, 2)

	employee := result.Users[0]
	require.Equal(t, workEmail, employee.UserEmail, "the alias reads under the canonical email")
	require.EqualValues(t, 2, employee.DeviceCount)
	require.Equal(t, []string{"installed", "running"}, employee.Signals)
	require.Equal(t, []string{"1.7.49", "1.7.52"}, employee.Versions)
	require.Equal(t, now.Add(-48*time.Hour).Format(time.RFC3339), employee.FirstSeen)
	require.Equal(t, now.Format(time.RFC3339), employee.LastSeen)
	require.Equal(t, "outsider@example.com", result.Users[1].UserEmail)
}
