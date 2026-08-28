package gcpauth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"golang.org/x/oauth2"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// Identity is the resolver every Gram service holds: a Resolver plus the two
// things a service needs before it acts on a customer-supplied service account —
// who Gram itself is, and whether a nominated impersonation target is one Gram
// may assume at all.
//
// It exists as one type rather than a resolver and a separate policy helper so a
// service cannot be wired with the ability to authenticate as a customer's
// identity but without the screening that decides which identities are allowed.
//
// Safe for concurrent use, and meant to be shared by every service that screens
// targets so they agree on one answer and probe for it once.
type Identity struct {
	resolver Resolver

	// gramPrincipalMu guards gramPrincipal and gramPrincipalOK.
	gramPrincipalMu sync.Mutex

	// gramPrincipal memoizes Gram's own resolved GCP identity. It is fixed for
	// the process lifetime, so re-probing would add an outbound round trip to
	// every screen for no new information. Only successful resolutions are
	// cached, so a transient failure (or an environment that has not yet been
	// granted credentials) retries.
	gramPrincipal string

	// gramPrincipalOK reports whether gramPrincipal holds a resolved value. It is
	// separate from an empty-string check because "" is a successful resolve
	// against a source that carries no service-account email.
	gramPrincipalOK bool
}

var _ Resolver = (*Identity)(nil)

// NewIdentity returns an identity backed by the given resolver.
func NewIdentity(resolver Resolver) *Identity {
	return &Identity{
		resolver:        resolver,
		gramPrincipalMu: sync.Mutex{},
		gramPrincipal:   "",
		gramPrincipalOK: false,
	}
}

// ResolvePrincipal reports the effective principal for a credential.
//
// Deliberately not served from the GramPrincipal memo, even for the ambient
// credential the memo holds: this is the call that proves an identity is usable
// right now, so an answer cached from an earlier grant would report success for
// an impersonation right that has since been revoked.
func (i *Identity) ResolvePrincipal(ctx context.Context, cred Credential) (Principal, error) {
	principal, err := i.resolver.ResolvePrincipal(ctx, cred)
	if err != nil {
		// Passed through unwrapped, unlike TokenSource below. Callers echo this
		// error's text into a user-facing verify detail, where the provider's own
		// message is the entire value: it names the grant the customer is missing.
		// A wrapper prefix naming an internal function only pushes that message
		// further from the reader.
		//nolint:wrapcheck // the provider's message is the product here, not context for it
		return Principal{Email: "", Source: ""}, err
	}

	return principal, nil
}

// TokenSource returns a token source that authenticates calls as the
// credential's identity.
func (i *Identity) TokenSource(ctx context.Context, cred Credential) (oauth2.TokenSource, error) {
	source, err := i.resolver.TokenSource(ctx, cred)
	if err != nil {
		return nil, fmt.Errorf("build gcp token source: %w", err)
	}

	return source, nil
}

// GramPrincipal reports Gram's own service account: the identity a customer
// grants impersonation rights to. An empty email with a nil error is a
// successful resolve against a source that carries no service-account email
// (local development backed by a user login), not a failure.
func (i *Identity) GramPrincipal(ctx context.Context) (string, error) {
	i.gramPrincipalMu.Lock()
	cached, ok := i.gramPrincipal, i.gramPrincipalOK
	i.gramPrincipalMu.Unlock()

	if ok {
		return cached, nil
	}

	// Resolve outside the lock. The probe mints a token and can hit the metadata
	// server, so it takes seconds when the environment is unhealthy — and since
	// failures are not cached, holding the lock across it would serialize every
	// screen in the process behind one slow call for the whole duration of an
	// outage. Concurrent callers racing to resolve is harmless: they compute the
	// same value.
	principal, err := i.resolver.ResolvePrincipal(ctx, Credential{
		ImpersonateServiceAccount: "",
		WifPoolID:                 "",
		WifProviderID:             "",
		WifProjectNumber:          "",
	})
	if err != nil {
		return "", fmt.Errorf("resolve gram's own gcp identity: %w", err)
	}

	i.gramPrincipalMu.Lock()
	i.gramPrincipal = principal.Email
	i.gramPrincipalOK = true
	i.gramPrincipalMu.Unlock()

	return principal.Email, nil
}

// TargetProblemKind classifies why an impersonation target must not be used.
//
// It exists so a caller can tell the two refusals apart without matching on
// message text: one of them is exemptible by a platform administrator and the
// other never is, and a screening decision that turned on the wording of a
// sentence would break the moment that sentence was reworded.
type TargetProblemKind string

const (
	// TargetUnknown is the zero value, returned only alongside an error. It is
	// deliberately not the acceptable case: a caller who reads the kind without
	// checking the error gets a value that no acceptance test matches, so a
	// screening that could not be evaluated fails closed rather than reading as
	// permission.
	TargetUnknown TargetProblemKind = ""

	// TargetOK means the target may be impersonated.
	TargetOK TargetProblemKind = "ok"

	// TargetMalformed means the target is not a user-managed service account
	// address, so it cannot be placed in a project at all. No exemption applies
	// to it: a target that cannot be placed cannot be screened.
	TargetMalformed TargetProblemKind = "malformed"

	// TargetOwnProject means the target names a service account in this
	// deployment's own GCP project, which ordinary callers must never reach.
	TargetOwnProject TargetProblemKind = "own_project"
)

// ImpersonationTargetProblem classifies why a target must not be impersonated,
// returning TargetOK when it is acceptable. The accompanying reason explains the
// refusal in terms the target's owner can act on and is empty when the kind is
// TargetOK.
//
// A non-nil error means the policy could not be evaluated at all, which callers
// must never treat as acceptance: the screening exists because this deployment
// publishes its own service account by design, so without it an organization
// member could name an internal service account and use a verify probe to
// discover which ones it holds impersonation rights on. Only one of the three
// return paths is ever meaningful at a time.
//
// Both sides of the comparison have to be a user-managed address for it to mean
// anything. Google's default compute and App Engine service accounts identify
// their project by number, or by an id that cannot be compared against one, so
// they are refused rather than compared unreliably.
//
// The logger is taken per call rather than held on the identity so the refusal
// lands with the caller's request-scoped attributes.
func (i *Identity) ImpersonationTargetProblem(ctx context.Context, logger *slog.Logger, target string) (TargetProblemKind, string, error) {
	if serviceAccountProject(target) == "" {
		return TargetMalformed, "impersonate_service_account must be a user-managed service account (name@PROJECT_ID.iam.gserviceaccount.com)", nil
	}

	gramSA, err := i.GramPrincipal(ctx)
	if err != nil {
		return TargetUnknown, "", fmt.Errorf("resolve gram's own gcp identity to screen impersonation target: %w", err)
	}

	gramProject := serviceAccountProject(gramSA)
	if gramProject == "" {
		// Gram is running as something this cannot place in a project — most
		// likely a default compute service account. Refusing is deliberate: the
		// alternative is comparing against nothing and silently accepting every
		// target, and a loud failure here is a deployment problem to fix (give
		// Gram a dedicated service account) rather than a hole to leave open.
		logger.ErrorContext(ctx, "gram's own gcp identity is not a user-managed service account, cannot screen impersonation targets",
			attr.SlogError(errors.New("unrecognized service account form")))
		return TargetUnknown, "", fmt.Errorf("gram's own gcp identity %q is not a user-managed service account", gramSA)
	}

	if serviceAccountProject(target) == gramProject {
		return TargetOwnProject, "impersonate_service_account must be a service account in your own GCP project", nil
	}

	return TargetOK, "", nil
}

// serviceAccountProject extracts the project id from a user-managed GCP service
// account email of the form name@PROJECT_ID.iam.gserviceaccount.com.
//
// Returns "" for every other address, including user accounts, Google's default
// compute (PROJECT_NUMBER-compute@developer.gserviceaccount.com) and App Engine
// (PROJECT_ID@appspot.gserviceaccount.com) service accounts, and the
// Google-managed service agents described below. Callers treat "" as "cannot be
// placed in a project" and refuse it, so a project number is never compared
// against a project id.
func serviceAccountProject(email string) string {
	_, domain, found := strings.Cut(strings.ToLower(strings.TrimSpace(email)), "@")
	if !found {
		return ""
	}

	project, ok := strings.CutSuffix(domain, ".iam.gserviceaccount.com")
	if !ok {
		return ""
	}

	if !isProjectNamespace(project) {
		return ""
	}

	return project
}

// managedAgentNamespaces are the domain components Google's own service agents
// live under, which are namespaces rather than project ids.
var managedAgentNamespaces = []string{"cloudservices", "container-engine-robot", "containerregistry"}

// isProjectNamespace reports whether the domain component of a
// *.iam.gserviceaccount.com address names a customer project rather than one of
// Google's managed service-agent namespaces.
//
// This matters because the caller compares the extracted value against Gram's
// own project to refuse targets inside it. A managed agent such as
// service-NUMBER@gcp-sa-cloudkms.iam.gserviceaccount.com would otherwise yield
// "gcp-sa-cloudkms", which never equals any project id, so an agent belonging to
// Gram's own project would slip through the very comparison meant to catch it.
func isProjectNamespace(project string) bool {
	if strings.HasPrefix(project, "gcp-sa-") || strings.HasSuffix(project, "-system") {
		return false
	}

	return !slices.Contains(managedAgentNamespaces, project)
}
