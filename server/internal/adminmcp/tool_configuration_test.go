package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

type recordingConfigurationReader struct {
	*recordingOrganizationReader
	featuresInput *gen.GetOrganizationFeaturesPayload
	settingsInput *gen.GetOrganizationChatAnalysisSettingsPayload
	features      *gen.ProductFeatures
	settings      *gen.AdminChatAnalysisSettings
	err           error
}

func (r *recordingConfigurationReader) GetOrganizationFeaturesStrict(_ context.Context, organizationID string) (*gen.ProductFeatures, error) {
	r.featuresInput = &gen.GetOrganizationFeaturesPayload{OrganizationID: organizationID}
	return r.features, r.err
}

func (r *recordingConfigurationReader) GetOrganizationChatAnalysisSettings(_ context.Context, input *gen.GetOrganizationChatAnalysisSettingsPayload) (*gen.AdminChatAnalysisSettings, error) {
	r.settingsInput = input
	return r.settings, r.err
}

func testConfigurationReads() *recordingConfigurationReader {
	return &recordingConfigurationReader{
		recordingOrganizationReader: &recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-a"}},
		features:                    &gen.ProductFeatures{},
		settings:                    &gen.AdminChatAnalysisSettings{OrganizationID: "org-a"},
	}
}

func TestOrganizationConfigurationReadsExactTargetAndSafeProjection(t *testing.T) {
	t.Parallel()
	reads := testConfigurationReads()
	reads.features = &gen.ProductFeatures{SsoEnabled: true, PlatformMcpEnabled: true, DeviceAgent: true, GatewayDiscoveryModesEnabled: true, GatewayFrozenToolsetsEnabled: true}
	reads.settings = &gen.AdminChatAnalysisSettings{OrganizationID: "org-a", WorkUnitsEnabled: true, WorkUnitsDailyCap: 42, BusinessMemoryDailyCap: 5, IsDefault: false}

	status, body, data := callStaffReadTool(t, reads, "get_organization_features", `{"organization_id":"org-a"}`)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "org-a", reads.getInput.IDOrSlug)
	require.Equal(t, "org-a", reads.featuresInput.OrganizationID)
	var features OrganizationFeatures
	require.NoError(t, json.Unmarshal(data, &features))
	require.Equal(t, "org-a", features.OrganizationID)
	require.True(t, features.SSOEnabled)
	require.True(t, features.PlatformMCPEnabled)
	require.True(t, features.GatewayDiscoveryModesEnabled)
	require.True(t, features.GatewayFrozenToolsetsEnabled)
	require.NotContains(t, body, `"device_agent"`)
	require.NotContains(t, body, "admin_session_token")

	status, body, data = callStaffReadTool(t, reads, "get_organization_chat_analysis_settings", `{"organization_id":"org-a"}`)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "org-a", reads.settingsInput.OrganizationID)
	var settings OrganizationChatAnalysisSettings
	require.NoError(t, json.Unmarshal(data, &settings))
	require.Equal(t, OrganizationChatAnalysisSettings{OrganizationID: "org-a", WorkUnitsEnabled: true, WorkUnitsDailyCap: 42, BusinessMemoryDailyCap: 5}, settings)
	require.NotContains(t, body, "admin_session_token")
}

func TestOrganizationConfigurationReadsFailClosed(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"get_organization_features", "get_organization_chat_analysis_settings"} {
		reads := testConfigurationReads()
		reads.org.ID = "org-b"
		_, body, _ := callStaffReadTool(t, reads, name, `{"organization_id":"org-a"}`)
		require.Contains(t, body, `"isError":true`)
		require.Nil(t, reads.featuresInput)
		require.Nil(t, reads.settingsInput)

		reads = testConfigurationReads()
		reads.err = errors.New("private database failure")
		_, body, _ = callStaffReadTool(t, reads, name, `{"organization_id":"org-a"}`)
		require.Contains(t, body, `"isError":true`)
		require.NotContains(t, body, "private database failure")

		if name == "get_organization_features" {
			reads = testConfigurationReads()
			reads.features = nil
			_, body, _ = callStaffReadTool(t, reads, name, `{"organization_id":"org-a"}`)
			require.Contains(t, body, `"isError":true`)
		}

		_, body, _ = callStaffReadTool(t, &recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-a"}}, name, `{"organization_id":"org-a"}`)
		require.Contains(t, body, `"isError":true`)
	}

	reads := testConfigurationReads()
	reads.settings.OrganizationID = "org-b"
	_, body, _ := callStaffReadTool(t, reads, "get_organization_chat_analysis_settings", `{"organization_id":"org-a"}`)
	require.Contains(t, body, `"isError":true`)
	require.NotContains(t, body, "org-b")

	auth := &testAuthenticator{principal: staffPrincipal()}
	request := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_admin_context","arguments":{}}}`))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response := httptest.NewRecorder()
	NewRuntime(auth, "", testConfigurationReads()).Handler().ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "inspect organization feature flags and chat analysis settings")
}
