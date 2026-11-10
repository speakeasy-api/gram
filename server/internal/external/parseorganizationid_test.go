package external

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseOrganizationIDReadsNestedOrganizationDomain(t *testing.T) {
	t.Parallel()

	var event webhookEvent
	require.NoError(t, json.Unmarshal([]byte(`{"event":"organization_domain.verified","data":{"organization_domain":{"object":"organization_domain","id":"org_domain_1","organization_id":"org_01NESTED","domain":"example.com","state":"verified"}}}`), &event))
	require.Equal(t, "org_01NESTED", parseOrganizationID(event))
}

func TestParseOrganizationIDReadsFlatOrganizationDomain(t *testing.T) {
	t.Parallel()

	var event webhookEvent
	require.NoError(t, json.Unmarshal([]byte(`{"event":"organization_domain.deleted","data":{"object":"organization_domain","id":"org_domain_1","organization_id":"org_01FLAT","domain":"example.com","state":"verified"}}`), &event))
	require.Equal(t, "org_01FLAT", parseOrganizationID(event))
}
