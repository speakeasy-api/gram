package background

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestScheduledCustomDomainHealthCheckWorkflowID(t *testing.T) {
	t.Parallel()

	domainID := uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e27")
	require.Equal(
		t,
		"v1:custom-domain-health:2026-07-21:019c1a55-c04e-7b95-a840-b5054b457e27",
		scheduledCustomDomainHealthCheckWorkflowID("2026-07-21", domainID),
	)
}

func TestManualCustomDomainHealthCheckWorkflowIDIsUnique(t *testing.T) {
	t.Parallel()

	domainID := uuid.MustParse("019c1a55-c04e-7b95-a840-b5054b457e27")
	first := manualCustomDomainHealthCheckWorkflowID(domainID)
	second := manualCustomDomainHealthCheckWorkflowID(domainID)

	require.NotEqual(t, first, second)
	require.Contains(t, first, "v1:custom-domain-health:manual:"+domainID.String()+":")
}
