package mcp

import (
	"bytes"
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type agentSessionCredential struct {
	AuthorizerUserID       string
	DelegatedGrants        []byte
	DelegatedGrantsVersion int32
}

func encodeAgentSessionPolicy(target AgentAuthorizationTarget, version authz.DelegatedPolicyVersion) ([]byte, error) {
	if target.Scope != authz.ScopeMCPConnect {
		return nil, fmt.Errorf("agent session target has unsupported scope %q", target.Scope)
	}
	policy, err := authz.NewDelegatedPolicy(version, []authz.Grant{{
		Scope: target.Scope,
		Selector: authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   target.MCPResourceID.String(),
			authz.SelectorKeyProjectID:    target.ProjectID.String(),
		},
	}})
	if err != nil {
		return nil, fmt.Errorf("construct agent session policy: %w", err)
	}
	encoded, err := authz.EncodeDelegatedPolicy(version, policy)
	if err != nil {
		return nil, fmt.Errorf("encode agent session policy: %w", err)
	}
	return encoded, nil
}

func newAgentSessionCredential(target AgentAuthorizationTarget, authorizerUserID string) (agentSessionCredential, error) {
	version := authz.CurrentDelegatedPolicyVersion
	encoded, err := encodeAgentSessionPolicy(target, version)
	if err != nil {
		return agentSessionCredential{}, err
	}
	if authorizerUserID == "" {
		return agentSessionCredential{}, fmt.Errorf("agent session authorizer is missing")
	}
	return agentSessionCredential{
		AuthorizerUserID:       authorizerUserID,
		DelegatedGrants:        encoded,
		DelegatedGrantsVersion: int32(version),
	}, nil
}

func loadAgentSessionCredential(
	endpoint *ResolvedMcpEndpoint,
	subject urn.SessionSubject,
	storedSubject urn.SessionSubject,
	organizationID pgtype.Text,
	authorizerUserID pgtype.Text,
	delegatedGrants []byte,
	delegatedGrantsVersion pgtype.Int4,
) (agentSessionCredential, error) {
	if subject.Kind != urn.SessionSubjectKindAgent || subject.String() != storedSubject.String() ||
		!organizationID.Valid || organizationID.String != endpoint.OrganizationID ||
		!authorizerUserID.Valid || authorizerUserID.String == "" || delegatedGrants == nil || !delegatedGrantsVersion.Valid {
		return agentSessionCredential{}, oops.C(oops.CodeUnauthorized)
	}
	if _, err := uuid.Parse(subject.ID); err != nil {
		return agentSessionCredential{}, oops.C(oops.CodeUnauthorized)
	}

	version := authz.DelegatedPolicyVersion(delegatedGrantsVersion.Int32)
	decoded, err := authz.DecodeDelegatedPolicy(version, delegatedGrants)
	if err != nil {
		return agentSessionCredential{}, oops.C(oops.CodeUnauthorized)
	}
	normalized, err := authz.EncodeDelegatedPolicy(version, decoded)
	if err != nil {
		return agentSessionCredential{}, oops.C(oops.CodeUnauthorized)
	}
	target, ok := agentAuthorizationTarget(endpoint)
	if !ok {
		return agentSessionCredential{}, oops.C(oops.CodeUnauthorized)
	}
	expected, err := encodeAgentSessionPolicy(*target, version)
	if err != nil || !bytes.Equal(normalized, expected) {
		return agentSessionCredential{}, oops.C(oops.CodeUnauthorized)
	}

	return agentSessionCredential{
		AuthorizerUserID:       authorizerUserID.String,
		DelegatedGrants:        append([]byte(nil), delegatedGrants...),
		DelegatedGrantsVersion: delegatedGrantsVersion.Int32,
	}, nil
}

func (s *Service) admitAgentSession(ctx context.Context, endpoint *ResolvedMcpEndpoint, subject urn.SessionSubject, credential agentSessionCredential) (context.Context, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || subject.Kind != urn.SessionSubjectKindAgent {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	if enabled, _ := s.agentAuthorizationRollout(ctx, s.logger, endpoint); !enabled {
		return ctx, oops.C(oops.CodeNotFound)
	}
	actor := urn.NewPrincipal(urn.PrincipalTypeAgent, subject.ID)
	ctx = contextvalues.WithPrincipalCredentialAuthorization(ctx, authCtx, actor, contextvalues.PrincipalCredential{
		AuthorizerUserID:       credential.AuthorizerUserID,
		DelegatedGrants:        credential.DelegatedGrants,
		DelegatedGrantsVersion: credential.DelegatedGrantsVersion,
	})
	ctx, err := s.authz.PrepareContext(ctx)
	if err != nil {
		return ctx, err
	}
	target, ok := agentAuthorizationTarget(endpoint)
	if !ok {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, target.connectCheck()); err != nil {
		return ctx, err
	}
	return ctx, nil
}
