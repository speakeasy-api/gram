package remotesessions

import (
	"github.com/google/uuid"
)

// Preparation wire values are shared by policy, persistence, and API diagnostics.
// Keep these string constants aligned with the Goa preparation enums.
const (
	PreparationStateUnsupportedProfile            string = "unsupported_profile"
	PreparationStateIncompleteMetadata            string = "incomplete_metadata"
	PreparationStateUnknownGrants                 string = "unknown_grants"
	PreparationStateManualSetupRequired           string = "manual_setup_required"
	PreparationStateConfigurationRequired         string = "configuration_required"
	PreparationStateInProgress                    string = "in_progress"
	PreparationStateIndeterminate                 string = "indeterminate"
	PreparationStateProviderRejection             string = "provider_rejection"
	PreparationStateTransientFailure              string = "transient_failure"
	PreparationStatePublishedAcceptanceUnverified string = "published_acceptance_unverified"
	PreparationStateReady                         string = "ready"
	PreparationStateUnlinked                      string = "unlinked"
	PreparationStageSelection                     string = "selection"
	PreparationStageEligibility                   string = "eligibility"
	PreparationStageRegistration                  string = "registration"
	PreparationStageDiscovery                     string = "discovery"
	PreparationStagePublication                   string = "publication"
	PreparationMechanismManual                    string = "manual"
	PreparationMechanismCIMD                      string = "cimd"
	PreparationMechanismDCR                       string = "dcr"
	PreparationGrantSourceUnknown                 string = "unknown"
	PreparationGrantSourceProviderReturned        string = "provider_returned"
	PreparationGrantSourceAdministratorDeclared   string = "administrator_declared"
	PreparationGrantSourceCIMDPublished           string = "cimd_published"
	preparationEligible                           string = "eligible"
)

// Identity-chaining preparation records downstream client registration readiness.
// It is distinct from issuer management and interactive OAuth authorization.

// PreparationResourceMetadata contains optional caller-declared consistency hints
// for project-write-authorized configuration, not provider-verified RFC 9728 evidence.
// Preparation does not exchange tokens or establish trust, consent, or user access.
type PreparationResourceMetadata struct {
	Resource             string
	AuthorizationServers []string
}

// PreparationInput configures identity-chaining readiness for one resource binding.
// It does not configure an interactive OAuth authorization request.
type PreparationInput struct {
	UserSessionIssuerID     uuid.UUID
	RemoteSessionIssuerID   uuid.UUID
	Resource                string
	ClientID                uuid.UUID
	Scopes                  []string
	Mechanism               string
	ConfirmGrants           []string
	ExpectedGeneration      int64
	TokenEndpointAuthMethod string
	ResourceMetadata        *PreparationResourceMetadata
}

// PreparationResult reports identity-chaining readiness and never carries credentials.
// Ready means registration recorded,
// not successful token exchange or authorization for any human.
type PreparationResult struct {
	State            string
	Stage            string
	Remediation      string
	Retryable        bool
	BindingID        uuid.UUID
	Generation       int64
	ClientID         uuid.UUID
	ExternalClientID string
	Issuer           string
	Resource         string
	GrantTypes       []string
	Scopes           []string
	GrantSource      string
}
