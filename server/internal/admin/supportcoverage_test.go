package admin

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

type stubCoverageReader struct {
	result *telemetry.SupportCoverageResult
	err    error
}

func (s stubCoverageReader) SupportCoverageForOrganization(context.Context, string, int) (*telemetry.SupportCoverageResult, error) {
	return s.result, s.err
}

func TestGetSupportCoverage_PreservesReaderErrorCode(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)
	svc.supportCoverage = stubCoverageReader{
		result: nil,
		err:    oops.E(oops.CodeInvalid, nil, "organization id is required"),
	}

	_, err := svc.GetSupportCoverage(ctx, &gen.GetSupportCoveragePayload{
		AdminSessionToken: nil, OrganizationID: "  ", WindowDays: 30,
	})

	// Malformed input must not read back as a server fault.
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeInvalid, shareable.Code)
}

func TestGetSupportCoverage_OmitsLastSeenWhenThereIsNoEvidence(t *testing.T) {
	t.Parallel()

	seen := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	ctx, svc, _ := newTestAdminService(t)
	svc.supportCoverage = stubCoverageReader{
		err: nil,
		result: &telemetry.SupportCoverageResult{
			Cells: []telemetry.SupportCoverageCell{
				{Capability: "session", Surface: "cursor", Status: "observed", Value: 3, Detail: "", LastSeen: seen},
				{Capability: "session", Surface: "codex", Status: "none", Value: 0, Detail: "", LastSeen: time.Time{}},
			},
			Unmapped:   nil,
			WindowDays: 30,
			From:       seen.AddDate(0, 0, -30),
			To:         seen,
		},
	}

	result, err := svc.GetSupportCoverage(ctx, &gen.GetSupportCoveragePayload{
		AdminSessionToken: nil, OrganizationID: "org-1", WindowDays: 30,
	})
	require.NoError(t, err)
	require.Len(t, result.Cells, 2)

	// last_seen declares a date-time format, so an empty sentinel would fail
	// validation for the whole response.
	require.NotNil(t, result.Cells[0].LastSeen)
	require.Equal(t, "2026-09-24T10:00:00Z", *result.Cells[0].LastSeen)
	require.Nil(t, result.Cells[1].LastSeen)
}
