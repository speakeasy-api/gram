package mv

import (
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

// workloadDeviceJoinTokenStatus is a join token's state as an administrator
// sees it. Only an active token can enroll a device.
type workloadDeviceJoinTokenStatus string

const (
	workloadDeviceJoinTokenActive   workloadDeviceJoinTokenStatus = "active"
	workloadDeviceJoinTokenRedeemed workloadDeviceJoinTokenStatus = "redeemed"
	workloadDeviceJoinTokenExpired  workloadDeviceJoinTokenStatus = "expired"
	workloadDeviceJoinTokenRevoked  workloadDeviceJoinTokenStatus = "revoked"
)

// deriveWorkloadDeviceJoinTokenStatus checks revoked, then redeemed, then
// expired. Revocation only applies to an unredeemed token, so revoked and
// redeemed never both hold; each outranks expired because it is the deliberate
// act that ended the token, while expiry is only the clock.
func deriveWorkloadDeviceJoinTokenStatus(row repo.GetWorkloadDeviceJoinTokenRow, now time.Time) workloadDeviceJoinTokenStatus {
	switch {
	case row.DeletedAt.Valid:
		return workloadDeviceJoinTokenRevoked
	case row.ConsumedAt.Valid:
		return workloadDeviceJoinTokenRedeemed
	case !row.ExpiresAt.Time.After(now):
		return workloadDeviceJoinTokenExpired
	default:
		return workloadDeviceJoinTokenActive
	}
}

// BuildWorkloadDeviceJoinTokenView renders one join token as of now. The row
// type carries no token hash, so neither the token nor its hash can reach the
// response.
func BuildWorkloadDeviceJoinTokenView(row repo.GetWorkloadDeviceJoinTokenRow, now time.Time) *gen.WorkloadDeviceJoinTokenSummary {
	return &gen.WorkloadDeviceJoinTokenSummary{
		ID:                row.ID.String(),
		AgentID:           row.AgentID.String(),
		CreatedByUserID:   row.CreatedByUserID,
		CreatedAt:         conv.FromPGTimestamptz(row.CreatedAt),
		ExpiresAt:         conv.FromPGTimestamptz(row.ExpiresAt),
		ConsumedAt:        conv.PtrFromPGTimestamptz(row.ConsumedAt),
		ConsumedBySubject: conv.FromPGText[string](row.ConsumedBySubject),
		RevokedAt:         conv.PtrFromPGTimestamptz(row.DeletedAt),
		Status:            string(deriveWorkloadDeviceJoinTokenStatus(row, now)),
	}
}

// BuildWorkloadDeviceJoinTokenListView renders join tokens in list order.
func BuildWorkloadDeviceJoinTokenListView(rows []repo.ListWorkloadDeviceJoinTokensRow, now time.Time) []*gen.WorkloadDeviceJoinTokenSummary {
	views := make([]*gen.WorkloadDeviceJoinTokenSummary, 0, len(rows))
	for _, row := range rows {
		views = append(views, BuildWorkloadDeviceJoinTokenView(repo.GetWorkloadDeviceJoinTokenRow(row), now))
	}
	return views
}
