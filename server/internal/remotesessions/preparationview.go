package remotesessions

import (
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func preparationDiagnostic(state string) *PreparationResult {
	r := &PreparationResult{State: state, Stage: PreparationStageSelection, GrantSource: PreparationGrantSourceUnknown, Remediation: "", Retryable: false, BindingID: uuid.Nil, Generation: 0, ClientID: uuid.Nil, ExternalClientID: "", Issuer: "", Resource: "", GrantTypes: nil, Scopes: nil}
	switch state {
	case PreparationStateUnsupportedProfile:
		r.Stage = PreparationStageEligibility
		r.Remediation = "The issuer does not advertise the ID-JAG profile. Configure a supported resource authorization server."
	case PreparationStateIncompleteMetadata:
		r.Stage = PreparationStageEligibility
		r.Remediation = "Refresh discovery or ask the provider to advertise JWT-bearer support."
	case PreparationStateUnknownGrants:
		r.Stage = PreparationStageRegistration
		r.Remediation = "An administrator must confirm effective registration grants."
	case PreparationStateManualSetupRequired:
		r.Stage = PreparationStageRegistration
		r.Remediation = "Configure a separate downstream client with JWT-bearer and a supported authentication method."
	case PreparationStateConfigurationRequired:
		r.Remediation = "Select an explicit client or unlink/rebind using the current binding generation."
	case PreparationStateInProgress:
		r.Stage = PreparationStageRegistration
		r.Retryable = true
		r.Remediation = "Read preparation status; do not submit another registration."
	case PreparationStateIndeterminate:
		r.Stage = PreparationStageRegistration
		r.Remediation = "Reconcile with the provider, then explicitly select the resulting registration or unlink before a new attempt. Do not blindly repeat registration."
	case PreparationStateProviderRejection:
		r.Stage = PreparationStageRegistration
		r.Remediation = "Review the provider registration requirements before explicitly retrying."
	case PreparationStateTransientFailure:
		r.Stage = PreparationStageDiscovery
		r.Retryable = true
		r.Remediation = "Refresh issuer discovery and retry."
	case PreparationStatePublishedAcceptanceUnverified:
		r.Stage = PreparationStagePublication
		r.Remediation = "Metadata is published. Provider acceptance and human access remain unverified."
	case PreparationStateReady:
		r.Stage = PreparationStageRegistration
		r.Remediation = "Registration is recorded. Per-user authorization must be verified by token exchange."
	case PreparationStateUnlinked:
		r.Remediation = "The binding is unlinked and prior generations are invalid."
	}
	return r
}

// Legacy bindings can have NULL preparation fields. Missing state must not
// imply readiness, and missing provenance must not imply verified grants.
func preparationBindingState(state pgtype.Text) string {
	if !state.Valid {
		return PreparationStateConfigurationRequired
	}
	return state.String
}

func preparationBindingGrantSource(source pgtype.Text) string {
	if !source.Valid {
		return PreparationGrantSourceUnknown
	}
	return source.String
}

func preparationResult(b repo.RemoteSessionEmaBinding, issuer repo.RemoteSessionIssuer, client repo.RemoteSessionClient, state string) *PreparationResult {
	if state == PreparationStateReady || state == PreparationStatePublishedAcceptanceUnverified {
		if !preparationGrantSourceRecorded(preparationBindingGrantSource(b.GrantSource)) {
			state = PreparationStateUnknownGrants
		} else if b.RemoteSessionClientID.Valid && client.ID == b.RemoteSessionClientID.UUID && !slices.Contains(client.GrantTypes, oauthwire.GrantTypeJWTBearer) {
			state = preparationMissingGrantsState(client.GrantTypes)
		}
	}
	r := preparationDiagnostic(state)
	r.BindingID = b.ID
	r.Generation = b.Generation
	r.Resource = b.Resource
	r.Issuer = issuer.Issuer
	r.GrantSource = preparationBindingGrantSource(b.GrantSource)
	r.Scopes = b.RequestedScopes
	if b.RemoteSessionClientID.Valid && client.ID == b.RemoteSessionClientID.UUID {
		r.ClientID = b.RemoteSessionClientID.UUID
		r.ExternalClientID = client.ClientID
		r.GrantTypes = client.GrantTypes
	}
	return r
}
