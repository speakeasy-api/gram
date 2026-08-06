package gcpauth

// Credential is the subset of a GCP IAM credential that determines how its
// identity resolves. All fields empty means the ambient attached identity.
type Credential struct {
	// ImpersonateServiceAccount is the service account Gram impersonates: the
	// target for impersonation mode, or the impersonation hop for WIF mode.
	ImpersonateServiceAccount string

	// WifPoolID is the Workload Identity Federation pool ID.
	WifPoolID string

	// WifProviderID is the Workload Identity Federation provider ID.
	WifProviderID string

	// WifProjectNumber is the GCP project number backing the WIF pool.
	WifProjectNumber string
}

// credentialMode is the authentication approach a credential's columns describe.
// It mirrors the server-side inference: any Workload Identity Federation field
// means WIF (an impersonation target is then only the federation hop), an
// impersonation target alone means impersonation, and no fields at all means the
// ambient attached identity. It is an unexported, string-valued enum so switch
// and test output reads clearly; the values never cross a boundary.
type credentialMode string

const (
	// modeAmbient uses Gram's own attached identity; no impersonation or WIF
	// fields are set.
	modeAmbient credentialMode = "ambient"

	// modeImpersonation impersonates a target service account from Gram's own
	// identity; an impersonation target is set and no WIF fields are.
	modeImpersonation credentialMode = "impersonation"

	// modeWIF federates in via Workload Identity Federation; at least one WIF
	// field is set (an impersonation target, if present, is only the hop).
	modeWIF credentialMode = "wif"
)

// mode derives the credential's authentication approach from which columns are
// set.
func (c Credential) mode() credentialMode {
	switch {
	case c.WifPoolID != "" || c.WifProviderID != "" || c.WifProjectNumber != "":
		return modeWIF
	case c.ImpersonateServiceAccount != "":
		return modeImpersonation
	default:
		return modeAmbient
	}
}
