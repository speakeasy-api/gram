package authz

import (
	"context"
	"fmt"
	"slices"
)

const (
	// MaxFilterCandidates bounds this spike's in-memory batch, not repository scans.
	MaxFilterCandidates = 100
)

// Filter returns allowed resource references in candidate order. The domain must
// load trusted, tenant-scoped candidates and expose only the returned resources.
// This filters concrete resource reads; it is not policy-driven lookup or discovery.
func (c *Client) Filter(ctx context.Context, subject Subject, permission Permission, targets []Target) ([]ResourceRef, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateSubject(subject); err != nil {
		return nil, err
	}
	definition, exists := c.definitions[permission]
	if !exists {
		return nil, fmt.Errorf("unknown permission %q", permission)
	}
	if !slices.Contains(definition.Forms, ResourceOperation) {
		return nil, fmt.Errorf("permission %q does not support resource filtering", permission)
	}
	if len(targets) > MaxFilterCandidates {
		return nil, fmt.Errorf("resource filtering exceeds %d candidates", MaxFilterCandidates)
	}
	questions := make([]Evaluation, 0, len(targets))
	seen := make(map[ResourceRef]bool, len(targets))
	for _, target := range targets {
		question, err := c.prepareEvaluation(subject, Request{Permission: permission, Target: target})
		if err != nil {
			return nil, err
		}
		if seen[question.Target.Resource] {
			return nil, fmt.Errorf("duplicate filtering candidate")
		}
		seen[question.Target.Resource] = true
		questions = append(questions, question)
	}
	return c.backend.Filter(ctx, subject, questions)
}
