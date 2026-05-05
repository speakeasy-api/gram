package repo

import (
	"context"

	"github.com/google/uuid"
)

type RiskPolicyTargetInput struct {
	TargetType string
	TargetID   string
}

type ReplaceRiskPolicyTargetsParams struct {
	RiskPolicyID   uuid.UUID
	OrganizationID string
	Targets        []RiskPolicyTargetInput
}

func (q *Queries) ReplaceRiskPolicyTargets(ctx context.Context, arg ReplaceRiskPolicyTargetsParams) error {
	if err := q.DeleteRiskPolicyTargetsByPolicy(ctx, arg.RiskPolicyID); err != nil {
		return err
	}
	if len(arg.Targets) == 0 {
		return nil
	}

	rows := make([]InsertRiskPolicyTargetsParams, 0, len(arg.Targets))
	for _, target := range arg.Targets {
		rows = append(rows, InsertRiskPolicyTargetsParams{
			RiskPolicyID:   arg.RiskPolicyID,
			OrganizationID: arg.OrganizationID,
			TargetType:     target.TargetType,
			TargetID:       target.TargetID,
		})
	}

	_, err := q.InsertRiskPolicyTargets(ctx, rows)
	return err
}

type CreateRiskPolicyWithTargetsParams struct {
	Policy  CreateRiskPolicyParams
	Targets []RiskPolicyTargetInput
}

// CreateRiskPolicyWithTargets is intended to be called on a tx-bound Queries
// when the caller needs policy and target writes to commit together.
func (q *Queries) CreateRiskPolicyWithTargets(ctx context.Context, arg CreateRiskPolicyWithTargetsParams) (RiskPolicy, error) {
	policy, err := q.CreateRiskPolicy(ctx, arg.Policy)
	if err != nil {
		return RiskPolicy{}, err
	}

	if err := q.ReplaceRiskPolicyTargets(ctx, ReplaceRiskPolicyTargetsParams{
		RiskPolicyID:   policy.ID,
		OrganizationID: policy.OrganizationID,
		Targets:        arg.Targets,
	}); err != nil {
		return RiskPolicy{}, err
	}

	return policy, nil
}

type UpdateRiskPolicyWithTargetsParams struct {
	Policy  UpdateRiskPolicyParams
	Targets []RiskPolicyTargetInput
}

// UpdateRiskPolicyWithTargets is intended to be called on a tx-bound Queries
// when the caller needs policy and target writes to commit together.
func (q *Queries) UpdateRiskPolicyWithTargets(ctx context.Context, arg UpdateRiskPolicyWithTargetsParams) (RiskPolicy, error) {
	policy, err := q.UpdateRiskPolicy(ctx, arg.Policy)
	if err != nil {
		return RiskPolicy{}, err
	}

	if err := q.ReplaceRiskPolicyTargets(ctx, ReplaceRiskPolicyTargetsParams{
		RiskPolicyID:   policy.ID,
		OrganizationID: policy.OrganizationID,
		Targets:        arg.Targets,
	}); err != nil {
		return RiskPolicy{}, err
	}

	return policy, nil
}
