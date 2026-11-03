package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

const testCustomDomainID = "9a4d0f1e-7a3c-4f43-9e3f-0b6c1a2d3e4f"

type recordingCustomDomainReader struct {
	*recordingOrganizationReader
	listInput *gen.ListCustomDomainsPayload
	list      *gen.AdminListCustomDomainsResult
	listErr   error
}

func (r *recordingCustomDomainReader) ListCustomDomains(_ context.Context, input *gen.ListCustomDomainsPayload) (*gen.AdminListCustomDomainsResult, error) {
	r.listInput = input
	return r.list, r.listErr
}

func testCustomDomainReads() *recordingCustomDomainReader {
	return &recordingCustomDomainReader{
		recordingOrganizationReader: &recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-a"}},
		list:                        &gen.AdminListCustomDomainsResult{CustomDomains: []*gen.AdminCustomDomain{}},
	}
}

func testAdminCustomDomain(orgID string) *gen.AdminCustomDomain {
	return &gen.AdminCustomDomain{
		ID: testCustomDomainID, OrganizationID: orgID, OrganizationName: "Example", OrganizationSlug: "example",
		Domain: "mcp.example.com", Verified: true, Activated: true,
		DNSTargetToken: new("abcdefgh23"), DNSTarget: new("abcdefgh23.domains.example.net"),
		DNSRoutingMethod: new("shared_cname"), HealthStatus: new("healthy"),
		DNSConfigurationStatus: new("matched"), DNSCheckedAt: new("2026-11-01T00:00:00Z"),
		CreatedAt: "2026-01-01T00:00:00Z",
	}
}

func TestListCustomDomainsProjectsTargetAndRoutingMethod(t *testing.T) {
	t.Parallel()
	reads := testCustomDomainReads()
	reads.list.CustomDomains = []*gen.AdminCustomDomain{testAdminCustomDomain("org-a")}

	status, _, data := callStaffReadTool(t, reads, "list_custom_domains", `{"dns_routing_method":"shared_cname"}`)
	require.Equal(t, http.StatusOK, status)
	require.Nil(t, reads.listInput.OrganizationID)
	require.Equal(t, new("shared_cname"), reads.listInput.DNSRoutingMethod)
	require.Nil(t, reads.getInput)

	var output ListCustomDomainsOutput
	require.NoError(t, json.Unmarshal(data, &output))
	require.False(t, output.Truncated)
	require.Equal(t, []CustomDomainListing{{
		ID: testCustomDomainID, OrganizationID: "org-a", OrganizationName: "Example", OrganizationSlug: "example",
		Domain: "mcp.example.com", Verified: true, Activated: true,
		DNSTargetToken: new("abcdefgh23"), DNSTarget: new("abcdefgh23.domains.example.net"),
		DNSRoutingMethod: new("shared_cname"), HealthStatus: new("healthy"),
		DNSConfigurationStatus: new("matched"), DNSCheckedAt: new("2026-11-01T00:00:00Z"),
		CreatedAt: "2026-01-01T00:00:00Z",
	}}, output.CustomDomains)
}

func TestListCustomDomainsRequiresExactOrganization(t *testing.T) {
	t.Parallel()
	reads := testCustomDomainReads()
	reads.list.CustomDomains = []*gen.AdminCustomDomain{testAdminCustomDomain("org-a")}

	status, _, data := callStaffReadTool(t, reads, "list_custom_domains", `{"organization_id":"org-a"}`)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "org-a", reads.getInput.IDOrSlug)
	require.Equal(t, new("org-a"), reads.listInput.OrganizationID)
	var output ListCustomDomainsOutput
	require.NoError(t, json.Unmarshal(data, &output))
	require.Len(t, output.CustomDomains, 1)

	// A slug or a mismatched organization never reaches the list.
	other := testCustomDomainReads()
	other.org.ID = "org-b"
	_, body, _ := callStaffReadTool(t, other, "list_custom_domains", `{"organization_id":"org-a"}`)
	require.Contains(t, body, `"isError":true`)
	require.Nil(t, other.listInput)

	// A row from another organization fails closed instead of leaking.
	leaky := testCustomDomainReads()
	leaky.list.CustomDomains = []*gen.AdminCustomDomain{testAdminCustomDomain("org-b")}
	_, body, _ = callStaffReadTool(t, leaky, "list_custom_domains", `{"organization_id":"org-a"}`)
	require.Contains(t, body, `"isError":true`)
	require.NotContains(t, body, "mcp.example.com")
}

func TestListCustomDomainsRejectsUnknownRoutingMethod(t *testing.T) {
	t.Parallel()
	reads := testCustomDomainReads()

	_, body, _ := callStaffReadTool(t, reads, "list_custom_domains", `{"dns_routing_method":"cname"}`)
	require.Contains(t, body, `"isError":true`)
	require.Nil(t, reads.listInput)
}

func TestListCustomDomainsTruncatesAtCap(t *testing.T) {
	t.Parallel()
	reads := testCustomDomainReads()
	reads.list.CustomDomains = make([]*gen.AdminCustomDomain, maxCustomDomains+1)
	for i := range reads.list.CustomDomains {
		reads.list.CustomDomains[i] = testAdminCustomDomain("org-a")
	}

	_, _, data := callStaffReadTool(t, reads, "list_custom_domains", `{}`)
	var output ListCustomDomainsOutput
	require.NoError(t, json.Unmarshal(data, &output))
	require.Len(t, output.CustomDomains, maxCustomDomains)
	require.True(t, output.Truncated)
}

func TestListCustomDomainsHidesReaderErrors(t *testing.T) {
	t.Parallel()
	reads := testCustomDomainReads()
	reads.listErr = errors.New("private database detail")

	_, body, _ := callStaffReadTool(t, reads, "list_custom_domains", `{}`)
	require.Contains(t, body, `"isError":true`)
	require.NotContains(t, body, "private database detail")
}
