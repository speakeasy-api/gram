package mv_test

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestCustomDomainViewPreservesObservationFreshnessWithinOneSecond(t *testing.T) {
	t.Parallel()
	checkedAt := time.Date(2026, time.November, 1, 0, 0, 0, 100, time.UTC)
	domain := customdomainsrepo.CustomDomain{
		DnsCheckedAt:    pgtype.Timestamptz{Time: checkedAt, Valid: true},
		HealthCheckedAt: pgtype.Timestamptz{Time: checkedAt, Valid: true},
		HealthStatus:    pgtype.Text{String: "healthy", Valid: true},
	}
	current := mv.BuildCustomDomainView(domain, false, uuid.Nil)
	require.Equal(t, new("healthy"), current.HealthStatus)
	domain.HealthCheckedAt.Time = checkedAt.Add(time.Millisecond)
	mixed := mv.BuildCustomDomainView(domain, false, uuid.Nil)
	// Both strings are rounded to seconds by the established API formatter.
	require.Equal(t, mixed.DNSCheckedAt, mixed.HealthCheckedAt)
	require.Equal(t, new("unknown"), mixed.HealthStatus)
}
