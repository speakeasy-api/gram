package contextvalues

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ActorTelemetryAttributes returns the reserved identifier-only authorization
// attributes. Empty values mean the attribute must be removed, not retained from
// caller input. Human identity is never inferred from an owner or authorizer.
// Use only for telemetry describing this request, not bulk ingested events.
//
// A workload records the agent it acts for as its actor, so its activity is
// counted as that agent's, and keeps its own identity in the workload issuer
// and subject, as hook events do. A workload without an assigned agent remains
// the actor. Only telemetry reads the agent this way; authorization, sessions
// and audit keep the workload.
func ActorTelemetryAttributes(ctx context.Context) map[string]string {
	attrs := map[string]string{
		"gram.authorization.organization_id":    "",
		"gram.authorization.actor.type":         "",
		"gram.authorization.actor.id":           "",
		"gram.authorization.api_key_id":         "",
		"gram.authorization.authorizer_user_id": "",
		"gram.authorization.owner_user_id":      "",
		"gram.workload_issuer.id":               "",
		"gram.workload.subject":                 "",
	}
	if authCtx, ok := GetAuthContext(ctx); ok && authCtx != nil {
		attrs["gram.authorization.organization_id"] = authCtx.ActiveOrganizationID
		attrs["gram.authorization.api_key_id"] = authCtx.APIKeyID
	}
	if actor, ok := AuthenticatedActor(ctx); ok {
		recorded := actor
		if actor.Type == urn.PrincipalTypeWorkload {
			if issuerID, subject, err := actor.Workload(); err == nil {
				attrs["gram.workload_issuer.id"] = issuerID.String()
				attrs["gram.workload.subject"] = subject
			}
			if agent, found := EffectiveAgent(ctx); found {
				recorded = agent
			}
		}
		attrs["gram.authorization.actor.type"] = string(recorded.Type)
		attrs["gram.authorization.actor.id"] = recorded.ID
	}
	if authorizer, owner, ok := PrincipalCredentialProvenance(ctx); ok {
		attrs["gram.authorization.authorizer_user_id"] = authorizer
		attrs["gram.authorization.owner_user_id"] = owner
	}
	return attrs
}
