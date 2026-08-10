package gcpauth

// Source records how an identity was resolved, so callers can tell an in-cluster
// attached identity apart from local Application Default Credentials or an
// impersonated service account.
type Source string

const (
	// SourceADC means the principal came from local Application Default
	// Credentials — the local-dev ambient path.
	SourceADC Source = "application_default_credentials"

	// SourceImpersonation means the principal is a service account Gram
	// successfully impersonated from its own identity.
	SourceImpersonation Source = "impersonation"

	// SourceMetadataServer means the principal came from the GCE/GKE metadata
	// server (the attached service account) — the production ambient path.
	SourceMetadataServer Source = "metadata_server"
)
