package contextvalues

import "context"

// ActorTelemetryAttributes returns the reserved identifier-only authorization
// attributes. Empty values mean the attribute must be removed, not retained from
// caller input. Human identity is never inferred from an owner or authorizer.
// Use only for telemetry describing this request, not bulk ingested events.
func ActorTelemetryAttributes(ctx context.Context) map[string]string {
	attrs := map[string]string{
		"gram.authorization.organization_id":    "",
		"gram.authorization.actor.type":         "",
		"gram.authorization.actor.id":           "",
		"gram.authorization.api_key_id":         "",
		"gram.authorization.authorizer_user_id": "",
		"gram.authorization.owner_user_id":      "",
	}
	if authCtx, ok := GetAuthContext(ctx); ok && authCtx != nil {
		attrs["gram.authorization.organization_id"] = authCtx.ActiveOrganizationID
		attrs["gram.authorization.api_key_id"] = authCtx.APIKeyID
	}
	if actor, ok := AuthenticatedActor(ctx); ok {
		attrs["gram.authorization.actor.type"] = string(actor.Type)
		attrs["gram.authorization.actor.id"] = actor.ID
	}
	if authorizer, owner, ok := PrincipalCredentialProvenance(ctx); ok {
		attrs["gram.authorization.authorizer_user_id"] = authorizer
		attrs["gram.authorization.owner_user_id"] = owner
	}
	return attrs
}
