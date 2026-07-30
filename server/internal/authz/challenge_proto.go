package authz

import (
	"fmt"
	"time"

	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
)

func challengeRowToProto(row authzrepo.ChallengeRow) *authzv1.ChallengeRow {
	ts := row.Timestamp.UTC().Format(time.RFC3339Nano)
	op := string(row.Operation)
	outcome := string(row.Outcome)
	reason := string(row.Reason)
	ptype := string(row.PrincipalType)
	evalCount := row.EvaluatedGrantCount
	filterCandidate := row.FilterCandidateCount
	filterAllowed := row.FilterAllowedCount

	reqs := make([]*authzv1.ChallengeRow_RequestedCheck, 0, len(row.RequestedChecks))
	for _, c := range row.RequestedChecks {
		scope := c.Scope
		kind := c.ResourceKind
		rid := c.ResourceID
		sel := c.Selector
		reqs = append(reqs, authzv1.ChallengeRow_RequestedCheck_builder{
			Scope:        &scope,
			ResourceKind: &kind,
			ResourceId:   &rid,
			Selector:     &sel,
		}.Build())
	}

	mgs := make([]*authzv1.ChallengeRow_MatchedGrant, 0, len(row.MatchedGrants))
	for _, g := range row.MatchedGrants {
		urn := g.PrincipalURN
		scope := g.Scope
		sel := g.Selector
		via := g.MatchedViaCheckScope
		mgs = append(mgs, authzv1.ChallengeRow_MatchedGrant_builder{
			PrincipalUrn:         &urn,
			Scope:                &scope,
			Selector:             &sel,
			MatchedViaCheckScope: &via,
		}.Build())
	}

	return authzv1.ChallengeRow_builder{
		Id:                   &row.ID,
		Timestamp:            &ts,
		OrganizationId:       &row.OrganizationID,
		ProjectId:            &row.ProjectID,
		TraceId:              &row.TraceID,
		SpanId:               &row.SpanID,
		RequestId:            row.RequestID,
		PrincipalUrn:         &row.PrincipalURN,
		PrincipalType:        &ptype,
		UserId:               row.UserID,
		UserExternalId:       row.UserExternalID,
		UserEmail:            row.UserEmail,
		ApiKeyId:             row.APIKeyID,
		SessionId:            row.SessionID,
		RoleSlugs:            row.RoleSlugs,
		Operation:            &op,
		Outcome:              &outcome,
		Reason:               &reason,
		Scope:                &row.Scope,
		ResourceKind:         &row.ResourceKind,
		ResourceId:           &row.ResourceID,
		Selector:             &row.Selector,
		ExpandedScopes:       row.ExpandedScopes,
		RequestedChecks:      reqs,
		MatchedGrants:        mgs,
		EvaluatedGrantCount:  &evalCount,
		FilterCandidateCount: &filterCandidate,
		FilterAllowedCount:   &filterAllowed,
	}.Build()
}

func challengeRowFromProto(msg *authzv1.ChallengeRow) (authzrepo.ChallengeRow, error) {
	ts, err := time.Parse(time.RFC3339Nano, msg.GetTimestamp())
	if err != nil {
		// Accept plain RFC3339 as well (no fractional seconds).
		ts, err = time.Parse(time.RFC3339, msg.GetTimestamp())
		if err != nil {
			return authzrepo.ChallengeRow{}, fmt.Errorf("parse challenge timestamp: %w", err)
		}
	}

	reqs := make([]authzrepo.RequestedCheck, 0, len(msg.GetRequestedChecks()))
	for _, c := range msg.GetRequestedChecks() {
		reqs = append(reqs, authzrepo.RequestedCheck{
			Scope:        c.GetScope(),
			ResourceKind: c.GetResourceKind(),
			ResourceID:   c.GetResourceId(),
			Selector:     c.GetSelector(),
		})
	}

	mgs := make([]authzrepo.MatchedGrant, 0, len(msg.GetMatchedGrants()))
	for _, g := range msg.GetMatchedGrants() {
		mgs = append(mgs, authzrepo.MatchedGrant{
			PrincipalURN:         g.GetPrincipalUrn(),
			Scope:                g.GetScope(),
			Selector:             g.GetSelector(),
			MatchedViaCheckScope: g.GetMatchedViaCheckScope(),
		})
	}

	return authzrepo.ChallengeRow{
		ID:                   msg.GetId(),
		Timestamp:            ts.UTC(),
		OrganizationID:       msg.GetOrganizationId(),
		ProjectID:            msg.GetProjectId(),
		TraceID:              msg.GetTraceId(),
		SpanID:               msg.GetSpanId(),
		RequestID:            optionalProtoString(msg.HasRequestId(), msg.GetRequestId()),
		PrincipalURN:         msg.GetPrincipalUrn(),
		PrincipalType:        authzrepo.PrincipalType(msg.GetPrincipalType()),
		UserID:               optionalProtoString(msg.HasUserId(), msg.GetUserId()),
		UserExternalID:       optionalProtoString(msg.HasUserExternalId(), msg.GetUserExternalId()),
		UserEmail:            optionalProtoString(msg.HasUserEmail(), msg.GetUserEmail()),
		APIKeyID:             optionalProtoString(msg.HasApiKeyId(), msg.GetApiKeyId()),
		SessionID:            optionalProtoString(msg.HasSessionId(), msg.GetSessionId()),
		RoleSlugs:            msg.GetRoleSlugs(),
		Operation:            authzrepo.Operation(msg.GetOperation()),
		Outcome:              authzrepo.Outcome(msg.GetOutcome()),
		Reason:               authzrepo.Reason(msg.GetReason()),
		Scope:                msg.GetScope(),
		ResourceKind:         msg.GetResourceKind(),
		ResourceID:           msg.GetResourceId(),
		Selector:             msg.GetSelector(),
		ExpandedScopes:       msg.GetExpandedScopes(),
		RequestedChecks:      reqs,
		MatchedGrants:        mgs,
		EvaluatedGrantCount:  msg.GetEvaluatedGrantCount(),
		FilterCandidateCount: msg.GetFilterCandidateCount(),
		FilterAllowedCount:   msg.GetFilterAllowedCount(),
	}, nil
}

func optionalProtoString(has bool, v string) *string {
	if !has {
		return nil
	}
	return &v
}
