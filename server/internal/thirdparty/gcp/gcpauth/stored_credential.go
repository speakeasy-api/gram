package gcpauth

import (
	"context"
	"log/slog"
)

// StoredCredentialProblem classifies why a stored organization-tier credential
// cannot be used to authenticate an outbound GCP call. The values double as the
// machine-readable outcome strings the verify surface reports, so callers that
// expose them do not re-map.
type StoredCredentialProblem string

const (
	// StoredCredentialDeleted means the backing credential row was soft-deleted.
	// Soft deletes never fire the foreign keys that reference credentials (the
	// deleted column is generated), so rows pointing at a tombstone are a normal,
	// reachable state that callers must report as what it is rather than as a
	// missing resource.
	StoredCredentialDeleted StoredCredentialProblem = "credential_deleted" //nolint:gosec // G101: a problem code, not a credential

	// StoredCredentialUnusable means the credential row exists but cannot
	// authenticate anything: it names no impersonation target, still carries
	// Workload Identity Federation configuration, or names a target the
	// impersonation screening refuses.
	StoredCredentialUnusable StoredCredentialProblem = "credential_unusable" //nolint:gosec // G101: a problem code, not a credential
)

// StoredCredential carries the organization-tier credential columns as they
// come back from a database read, before any screening. It exists so every
// surface that authenticates as a stored credential runs the same ladder of
// checks; a caller assembling a Credential by hand would skip them.
type StoredCredential struct {
	// Present reports whether a live (non-soft-deleted) credential row was
	// found. A LEFT JOIN read leaves it false when the credential is gone.
	Present bool

	// ImpersonateServiceAccount is the stored impersonation target, empty when
	// the row names none.
	ImpersonateServiceAccount string

	// HasWifConfig reports whether any Workload Identity Federation column is
	// still set on the row.
	HasWifConfig bool

	// SkipProjectVerification exempts this credential from the refusal of
	// targets in this deployment's own GCP project, which a platform
	// administrator granted when the row was written. The exemption is recorded
	// on the row because this screening also runs where no request actor exists
	// to consult.
	//
	// It must always be read from the stored column. Writing a literal true at a
	// call site grants the exemption to a credential nobody approved, which is
	// the whole of what the screening prevents.
	SkipProjectVerification bool
}

// ScreenStoredCredential settles which identity an outbound GCP call should
// authenticate as, given a credential as stored, or reports why it cannot.
//
// A non-empty problem means the credential must not be used; detail explains it
// in terms the credential's owner can act on. A non-nil error means the
// screening could not be evaluated at all, which callers must never treat as
// acceptance. Only one of the three return paths is ever meaningful at a time.
//
// The ladder is ordered from most to least fundamental. Rows written before the
// organization tier became impersonation-only can name no target, or name one
// alongside Workload Identity Federation columns; neither can be used honestly.
// An empty target would authenticate as Gram's own ambient identity and act on
// a customer-supplied resource with Gram's own reach, and a WIF row's real
// resolution mode is WIF (which the resolver reports as unsupported), so using
// its impersonation hop in isolation would claim the credential works when
// nothing else can use it.
//
// The final step re-screens the stored target via ImpersonationTargetProblem.
// The write-time guard postdates the rows it screens, so a credential created
// earlier can still name a service account in Gram's own project — and an
// endpoint would then authenticate as it against a caller-supplied resource
// name, which is an inventory oracle for Gram's own GCP estate.
//
// A row carrying SkipProjectVerification is forgiven that final refusal, and
// only that one. The screening still runs in full: forgiving a classified
// refusal rather than skipping the comparison costs nothing here, because this
// path resolves the ambient identity on every call regardless, and it keeps the
// exemption from widening to a malformed address.
func (i *Identity) ScreenStoredCredential(ctx context.Context, logger *slog.Logger, stored StoredCredential) (Credential, StoredCredentialProblem, string, error) {
	if !stored.Present {
		return noCredential(), StoredCredentialDeleted, "the backing credential for this key was deleted; point the key at a live credential", nil
	}

	switch {
	case stored.ImpersonateServiceAccount == "":
		return noCredential(), StoredCredentialUnusable, "the backing credential names no service account to impersonate; edit it to set one", nil
	case stored.HasWifConfig:
		return noCredential(), StoredCredentialUnusable, "the backing credential still uses Workload Identity Federation, which cannot be used; save it again to convert it to impersonation", nil
	}

	kind, reason, err := i.ImpersonationTargetProblem(ctx, logger, stored.ImpersonateServiceAccount)
	if err != nil {
		return noCredential(), "", "", err
	}
	exempted := kind == TargetOwnProject && stored.SkipProjectVerification
	if kind != TargetOK && !exempted {
		return noCredential(), StoredCredentialUnusable, reason, nil
	}

	return Credential{
		ImpersonateServiceAccount: stored.ImpersonateServiceAccount,
		WifPoolID:                 "",
		WifProviderID:             "",
		WifProjectNumber:          "",
	}, "", "", nil
}

// noCredential is the credential the screening's negative paths carry: each
// reports a problem instead, so nothing reads it. It exists because exhaustruct
// requires every field at each literal.
func noCredential() Credential {
	return Credential{
		ImpersonateServiceAccount: "",
		WifPoolID:                 "",
		WifProviderID:             "",
		WifProjectNumber:          "",
	}
}
