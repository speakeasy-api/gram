package externalkeys

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/external_keys"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalkeys/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
)

// verifyRatePerMin and verifyRateBurst bound how often one organization can run
// the verify probe. Verify authenticates outbound as a caller-named identity and
// then performs a real AsymmetricSign, so an unbounded endpoint would be both an
// oracle for what Gram can reach and a way to run up signing charges on someone
// else's key.
//
// The burst is wider than the credential bucket's because the unit being probed
// is different: an organization holds a handful of credentials but can hold a key
// per signing purpose, and the keys page offers verify per row, so a burst sized
// for credentials would refuse an operator working down a list they can see.
const (
	verifyRatePerMin = 10
	verifyRateBurst  = 10
)

// probeOutcome values the handler produces itself, for the states that are
// settled before any call leaves the process. Everything downstream of that is a
// gcpkms.VerifyReason rendered verbatim, so the two sets share one namespace and
// the design's enum lists both.
const (
	outcomeCredentialDeleted  = "credential_deleted"  //nolint:gosec // G101: a probe outcome, not a credential
	outcomeCredentialUnusable = "credential_unusable" //nolint:gosec // G101: a probe outcome, not a credential
	outcomeUnsupportedAlg     = string(gcpkms.ReasonUnsupportedAlgorithm)
)

// verifyDetailMaxLen bounds the provider text a failed probe echoes back. The
// untruncated error is always in the log.
const verifyDetailMaxLen = 300

// VerifyGcpKmsKey probes end to end that Gram can reach a GCP KMS key through
// its backing credential and use it to sign. The probe is ephemeral — nothing is
// persisted — and almost every negative outcome is a reportable state a human can
// act on rather than a request error: a missing roles/cloudkms.signerVerifier
// grant, a key version that is DISABLED or still PENDING_GENERATION, or a key
// whose real algorithm is not the one recorded against it.
//
// It performs a real signing operation, billed to the key's owner and recorded
// in their Cloud Audit Log under gcpkms.ProbePayload.
func (s *Service) VerifyGcpKmsKey(ctx context.Context, payload *gen.VerifyGcpKmsKeyPayload) (*gen.VerifyKmsKeyResult, error) {
	authCtx, logger, err := s.requireOrgAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid key id").LogError(ctx, logger)
	}

	// A limiter outage is not a throttle: verify is a read-only probe, so degrade
	// to allowing rather than blocking the organization on a Redis blip.
	switch res, limitErr := s.verifyLimiter.Allow(ctx, authCtx.ActiveOrganizationID); {
	case limitErr != nil:
		logger.WarnContext(ctx, "external key verify rate limiter unavailable, allowing", attr.SlogError(limitErr))
	case !res.Allowed:
		return nil, oops.E(oops.CodeRateLimitExceeded, nil, "verify rate limit exceeded, try again shortly")
	}

	row, err := repo.New(s.db).GetGcpKmsKeyForVerify(ctx, repo.GetGcpKmsKeyForVerifyParams{
		ID:             id,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "gcp kms key not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading gcp kms key").LogError(ctx, logger)
	}

	credential, outcome, detail, err := s.resolveVerifyIdentity(ctx, logger, row)
	if err != nil {
		return nil, err
	}
	if outcome != "" {
		return unverified(outcome, detail), nil
	}

	// The recorded algorithm is what Gram advertises and signs with, so it is the
	// expectation the key is measured against. It comes back out of a text column,
	// where a bare conversion to jose.SignatureAlgorithm would accept anything the
	// column happens to hold.
	want, err := gcpkms.ParseSignatureAlgorithm(row.ExternalKey.Algorithm)
	if err != nil {
		logger.ErrorContext(ctx, "external key records an algorithm gram cannot sign with", attr.SlogError(err))
		return unverified(outcomeUnsupportedAlg, "this key records an algorithm Gram cannot sign with; delete it and create it again with RS256 or ES256"), nil
	}

	// TokenSource alone proves nothing: it is lazy by design, so a credential Gram
	// cannot actually impersonate would build a source successfully and then fail
	// on the first KMS call, where the error arrives as Unauthenticated and reads
	// as a missing roles/cloudkms.signerVerifier grant on the key. That names the
	// wrong grant entirely — the missing one is
	// roles/iam.serviceAccountTokenCreator on the service account. Resolving the
	// principal first mints a token, which is what separates the two.
	if _, err := s.gcpIdentity.ResolvePrincipal(ctx, credential); err != nil {
		logger.InfoContext(ctx, "gcp kms key verify could not assume the credential's identity", attr.SlogError(err))
		return unverified(outcomeCredentialUnusable, conv.TruncateDetail(err.Error(), verifyDetailMaxLen)), nil
	}

	// The identity resolved a moment ago, so a token source that cannot be built
	// for it now is a fault on Gram's side rather than the customer's.
	tokenSource, err := s.gcpIdentity.TokenSource(ctx, credential)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "cannot verify this key right now, try again shortly").LogError(ctx, logger)
	}

	client, err := s.kmsClients(ctx, tokenSource)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "cannot verify this key right now, try again shortly").LogError(ctx, logger)
	}
	// Each client owns a gRPC connection, so skipping this leaks one per verify.
	defer o11y.LogDefer(ctx, logger, func() error { return client.Close() })

	result := gcpkms.VerifySigningKey(ctx, client, row.GcpKmsKey.ResourceName, want)
	if !result.Verified {
		logger.InfoContext(ctx, "gcp kms key verify probe did not succeed",
			attr.SlogError(errors.New(result.Detail)),
			attr.SlogOutcome(string(result.Reason)))
		return unverified(string(result.Reason), conv.TruncateDetail(result.Detail, verifyDetailMaxLen)), nil
	}

	return &gen.VerifyKmsKeyResult{
		Verified:     true,
		ProbeOutcome: string(gcpkms.ReasonVerified),
		Detail:       nil,
	}, nil
}

// resolveVerifyIdentity settles which identity the probe should authenticate as,
// or reports why it cannot run at all.
//
// A non-empty outcome means the caller returns that outcome unverified; a
// non-nil error means the probe could not be evaluated and the request fails.
// Only one of the three return paths is ever meaningful at a time.
func (s *Service) resolveVerifyIdentity(ctx context.Context, logger *slog.Logger, row repo.GetGcpKmsKeyForVerifyRow) (credential gcpauth.Credential, outcome, detail string, err error) {
	// external_credentials.deleted is a generated column, so soft-deleting a
	// credential never fires the external_keys foreign key and leaves keys behind
	// it pointing at a tombstone. Those keys still exist and still list, so this
	// says what actually happened rather than reporting the key as missing.
	if !row.CredentialID.Valid {
		return gcpauth.Credential{
			ImpersonateServiceAccount: "",
			WifPoolID:                 "",
			WifProviderID:             "",
			WifProjectNumber:          "",
		}, outcomeCredentialDeleted, "the backing credential for this key was deleted; point the key at a live credential", nil
	}

	// Rows written before this tier became impersonation-only can name no target,
	// or name one alongside Workload Identity Federation columns. Neither can be
	// probed honestly: an empty target would authenticate as Gram's own ambient
	// identity and report on a key Gram reaches by itself, and a WIF row's real
	// resolution mode is WIF (which gcpauth reports as unsupported), so probing
	// its impersonation hop in isolation would claim the credential works when
	// nothing else can use it.
	target := row.ImpersonateServiceAccount.String
	switch {
	case target == "":
		return gcpauth.Credential{
			ImpersonateServiceAccount: "",
			WifPoolID:                 "",
			WifProviderID:             "",
			WifProjectNumber:          "",
		}, outcomeCredentialUnusable, "the backing credential names no service account to impersonate; edit it to set one", nil
	case row.WifPoolID.Valid || row.WifProviderID.Valid || row.WifProjectNumber.Valid:
		return gcpauth.Credential{
			ImpersonateServiceAccount: "",
			WifPoolID:                 "",
			WifProviderID:             "",
			WifProjectNumber:          "",
		}, outcomeCredentialUnusable, "the backing credential still uses Workload Identity Federation, which cannot be verified; save it again to convert it to impersonation", nil
	}

	// Re-screen the stored target. The write-time guard postdates the rows it
	// screens, so a credential created earlier can still name a service account in
	// Gram's own project — and this endpoint would then authenticate as it and
	// probe a caller-supplied resource name, which is an inventory oracle for
	// Gram's own KMS. A screening the server cannot evaluate is an error rather
	// than an unverified result: reporting "not verified" would blame the
	// customer's configuration for a fault on Gram's side.
	reason, err := s.gcpIdentity.ImpersonationTargetProblem(ctx, logger, target)
	if err != nil {
		return gcpauth.Credential{
			ImpersonateServiceAccount: "",
			WifPoolID:                 "",
			WifProviderID:             "",
			WifProjectNumber:          "",
		}, "", "", oops.E(oops.CodeUnexpected, err, "cannot verify this key right now, try again shortly").LogError(ctx, logger)
	}
	if reason != "" {
		return gcpauth.Credential{
			ImpersonateServiceAccount: "",
			WifPoolID:                 "",
			WifProviderID:             "",
			WifProjectNumber:          "",
		}, outcomeCredentialUnusable, reason, nil
	}

	return gcpauth.Credential{
		ImpersonateServiceAccount: target,
		WifPoolID:                 "",
		WifProviderID:             "",
		WifProjectNumber:          "",
	}, "", "", nil
}

func unverified(outcome, detail string) *gen.VerifyKmsKeyResult {
	return &gen.VerifyKmsKeyResult{
		Verified:     false,
		ProbeOutcome: outcome,
		Detail:       conv.PtrEmpty(detail),
	}
}
