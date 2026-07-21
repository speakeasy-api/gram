package activities_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestCustomDomainHealthCheckMissingDomainIsNoop(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "custom_domain_health_missing")
	require.NoError(t, err)
	checker := activities.NewCustomDomainHealth(testenv.NewLogger(t), conn, nil, "custom-domain.example.com")

	err = checker.Check(t.Context(), activities.CheckCustomDomainHealthArgs{
		CustomDomainID: uuid.New(),
		OrganizationID: "test-organization",
		CheckedAt:      time.Now().UTC(),
	})

	require.NoError(t, err)
}
