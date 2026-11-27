package workos_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/partition"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestPartitionFromMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metadata map[string]string
		want     partition.Partition
	}{
		{name: "nil metadata", metadata: nil, want: partition.US},
		{name: "empty metadata", metadata: map[string]string{}, want: partition.US},
		{name: "other keys only", metadata: map[string]string{"plan": "enterprise"}, want: partition.US},
		{name: "us marker", metadata: map[string]string{workos.PartitionMetadataKey: "us"}, want: partition.US},
		{name: "eu marker", metadata: map[string]string{workos.PartitionMetadataKey: "eu", "plan": "enterprise"}, want: partition.EU},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := workos.PartitionFromMetadata(tc.metadata)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestPartitionFromMetadataRejectsInvalidMarkers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{name: "empty", raw: ""},
		{name: "uppercase", raw: "EU"},
		{name: "unknown", raw: "apac"},
		{name: "padded", raw: " us"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := workos.PartitionFromMetadata(map[string]string{workos.PartitionMetadataKey: tc.raw})
			require.ErrorIs(t, err, workos.ErrInvalidPartitionMarker)
		})
	}
}

func TestOrganizationPartitionErrorNamesOrganizationAndValue(t *testing.T) {
	t.Parallel()

	org := workos.Organization{
		ID:         "org_invalid",
		Name:       "Invalid",
		ExternalID: "",
		CreatedAt:  "",
		UpdatedAt:  "",
		Metadata:   map[string]string{workos.PartitionMetadataKey: "EU"},
	}

	_, err := org.Partition()
	require.ErrorIs(t, err, workos.ErrInvalidPartitionMarker)
	require.ErrorContains(t, err, "org_invalid")
	require.ErrorContains(t, err, `"EU"`)
}

func TestGetOrganizationReadsPartitionMarker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metadata string
		want     partition.Partition
	}{
		{name: "missing marker", metadata: `{}`, want: partition.US},
		{name: "matching marker", metadata: `{"partition":"us"}`, want: partition.US},
		{name: "other partition marker", metadata: `{"partition":"eu","plan":"enterprise"}`, want: partition.EU},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := organizationHTTPClient(t, partition.US, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"org_example","name":"example","metadata":`+tc.metadata+`}`)
			})

			org, err := client.GetOrganization(t.Context(), "org_example")
			require.NoError(t, err)

			got, err := org.Partition()
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestListOrganizationsReadsMetadata(t *testing.T) {
	t.Parallel()

	client := organizationHTTPClient(t, partition.US, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"org_us","name":"us"},{"id":"org_eu","name":"eu","metadata":{"partition":"eu"}}],"list_metadata":{"after":""}}`)
	})

	orgs, err := client.ListOrganizations(t.Context())
	require.NoError(t, err)
	require.Len(t, orgs, 2)

	us, err := orgs[0].Partition()
	require.NoError(t, err)
	require.Equal(t, partition.US, us)

	eu, err := orgs[1].Partition()
	require.NoError(t, err)
	require.Equal(t, partition.EU, eu)
}

// captureCreateMetadata returns a client in partition p that records the
// metadata of every organization create request.
func captureCreateMetadata(t *testing.T, p partition.Partition) (*workos.Client, <-chan json.RawMessage) {
	t.Helper()

	metadata := make(chan json.RawMessage, 1)
	client := organizationHTTPClient(t, p, func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		metadata <- payload["metadata"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, verifiedOrganizationResponse)
	})

	return client, metadata
}

func TestCreateOrganizationMarksClientPartition(t *testing.T) {
	t.Parallel()

	client, metadata := captureCreateMetadata(t, partition.EU)

	_, err := client.CreateOrganization(t.Context(), "example", "")
	require.NoError(t, err)
	require.JSONEq(t, `{"partition":"eu"}`, string(<-metadata))
}

func TestCreateOrganizationWithVerifiedDomainMarksClientPartition(t *testing.T) {
	t.Parallel()

	client, metadata := captureCreateMetadata(t, partition.EU)

	_, err := client.CreateOrganizationWithVerifiedDomain(t.Context(), "example", "www.example.com")
	require.NoError(t, err)
	require.JSONEq(t, `{"partition":"eu"}`, string(<-metadata))
}

func TestCreateOrganizationWithoutPartitionSendsNothing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		p    partition.Partition
	}{
		{name: "empty", p: ""},
		{name: "unknown", p: "apac"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := organizationHTTPClient(t, tc.p, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected WorkOS request: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusInternalServerError)
			})

			_, err := client.CreateOrganization(t.Context(), "example", "")
			require.ErrorIs(t, err, workos.ErrClientPartitionNotConfigured)

			_, err = client.CreateOrganizationWithVerifiedDomain(t.Context(), "example", "www.example.com")
			require.ErrorIs(t, err, workos.ErrClientPartitionNotConfigured)
		})
	}
}

func TestSetOrganizationPartitionSendsOnlyTheMarker(t *testing.T) {
	t.Parallel()

	type request struct {
		method string
		path   string
		body   []byte
	}
	requests := make(chan request, 1)
	client := organizationHTTPClient(t, partition.US, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		requests <- request{method: r.Method, path: r.URL.Path, body: body}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"org_example","name":"example","metadata":{"partition":"eu"}}`)
	})

	require.NoError(t, client.SetOrganizationPartition(t.Context(), "org_example", partition.EU))

	got := <-requests
	require.Equal(t, http.MethodPut, got.method)
	require.Equal(t, "/organizations/org_example", got.path)
	require.JSONEq(t, `{"metadata":{"partition":"eu"}}`, string(got.body))
}

func TestSetOrganizationPartitionRejectsInvalidPartitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		p    partition.Partition
	}{
		{name: "empty", p: ""},
		{name: "uppercase", p: "EU"},
		{name: "unknown", p: "apac"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := organizationHTTPClient(t, partition.US, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected WorkOS request: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusInternalServerError)
			})

			require.Error(t, client.SetOrganizationPartition(t.Context(), "org_example", tc.p))
		})
	}
}

func TestStubClientMarksCreatedOrganizations(t *testing.T) {
	t.Parallel()

	stub := workos.NewStubClient()

	usOrgID, err := stub.CreateOrganization(t.Context(), "US Co", "")
	require.NoError(t, err)

	stub.SetPartition(partition.EU)
	euOrgID, err := stub.CreateOrganization(t.Context(), "EU Co", "")
	require.NoError(t, err)

	usOrg, err := stub.GetOrganization(t.Context(), usOrgID)
	require.NoError(t, err)
	got, err := usOrg.Partition()
	require.NoError(t, err)
	require.Equal(t, partition.US, got)

	euOrg, err := stub.GetOrganization(t.Context(), euOrgID)
	require.NoError(t, err)
	got, err = euOrg.Partition()
	require.NoError(t, err)
	require.Equal(t, partition.EU, got)
}

func TestStubClientSetOrganizationPartitionKeepsOtherMetadata(t *testing.T) {
	t.Parallel()

	stub := workos.NewStubClient()
	stub.UpsertOrganization(workos.Organization{
		ID:         "org_example",
		Name:       "Example",
		ExternalID: "",
		CreatedAt:  "",
		UpdatedAt:  "",
		Metadata:   map[string]string{"plan": "enterprise"},
	})

	require.NoError(t, stub.SetOrganizationPartition(t.Context(), "org_example", partition.EU))

	org, err := stub.GetOrganization(t.Context(), "org_example")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"plan": "enterprise", workos.PartitionMetadataKey: "eu"}, org.Metadata)
}

func TestStubClientSetOrganizationPartitionRejectsInvalidPartition(t *testing.T) {
	t.Parallel()

	stub := workos.NewStubClient()
	orgID, err := stub.CreateOrganization(t.Context(), "Example", "")
	require.NoError(t, err)

	require.Error(t, stub.SetOrganizationPartition(t.Context(), orgID, "EU"))

	org, err := stub.GetOrganization(t.Context(), orgID)
	require.NoError(t, err)
	require.Equal(t, map[string]string{workos.PartitionMetadataKey: "us"}, org.Metadata)
}
