package jsonwebkeysets

import (
	"context"
	"crypto"
	"encoding/base64"
	"errors"
	"log/slog"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
)

// Lifecycle states of a published key. The state column carries no database
// CHECK constraint (per house convention), so these constants plus the
// 'revoked' literal the RevokeJsonWebKey query writes are the enum.
const (
	keyStatePending = "pending"
	keyStateActive  = "active"
	keyStateRetired = "retired"
)

const externalKeyProviderGcpKms = "gcp_kms"

// mintRatePerMin and mintRateBurst bound how often one organization can mint a
// key (set creation and key publication). Each mint calls GetPublicKey, a KMS
// management-tier RPC whose quota sits far below the cryptographic operations'
// and is the customer's, so an unbounded endpoint would let a runaway client
// exhaust quota the customer's own workloads need. The bounds match the verify
// probe's for the same reasons its bounds were chosen: an organization can hold
// a key per signing purpose, and an operator working down a list should not be
// refused.
const (
	mintRatePerMin = 10
	mintRateBurst  = 10
)

// mintDetailMaxLen bounds the provider text a failed mint echoes back. The
// untruncated error is always in the log.
const mintDetailMaxLen = 300

// mintedJWK is the outcome of reading a backing key's public half: the
// published document and the identity facts the write transaction re-verifies.
type mintedJWK struct {
	// externalKeyID is the key the JWK was minted from, pinned onto the
	// published row so signing for this kid always resolves this exact key.
	externalKeyID uuid.UUID

	// kid is the RFC 7638 SHA-256 thumbprint of the public key, base64url
	// encoded without padding. A pure function of the key material.
	kid string

	// publicJWK is the marshaled JWK document, carrying the same kid.
	publicJWK []byte
}

// allowMint applies the per-organization mint rate limit. Best effort like the
// verify probe's limiter: an outage degrades to allowing rather than blocking
// every organization's key management on a Redis blip, because the bound exists
// to stop casual quota abuse, not to be a security boundary.
func (s *Service) allowMint(ctx context.Context, logger *slog.Logger, organizationID string) error {
	switch res, limitErr := s.mintLimiter.Allow(ctx, organizationID); {
	case limitErr != nil:
		logger.WarnContext(ctx, "jwks mint rate limiter unavailable, allowing", attr.SlogError(limitErr))
	case !res.Allowed:
		return oops.E(oops.CodeRateLimitExceeded, nil, "key publishing rate limit exceeded, try again shortly")
	}

	return nil
}

// mintFromExternalKey reads the backing key's public half out of the customer's
// KMS and shapes it into the JWK document to publish. It runs BEFORE the write
// transaction opens: the KMS read is seconds of external RPC against a
// low-quota management-tier endpoint, and holding a pool connection across it
// would be the transaction-shaped copy of that outage. The caller's transaction
// re-locks the backing key row and re-verifies nothing moved.
//
// Negative outcomes name what the key's owner can fix. The credential
// screening (deleted, targetless, WIF-era, or a screened-out impersonation
// target) is gcpauth.Identity.ScreenStoredCredential, shared with the
// externalkeys verify probe; unlike the probe, which reports those states as
// unverified outcomes, minting refuses them outright because a publish that
// cannot read the key has nothing to publish.
func (s *Service) mintFromExternalKey(ctx context.Context, logger *slog.Logger, organizationID string, externalKeyID uuid.UUID) (*mintedJWK, error) {
	row, err := repo.New(s.db).GetExternalKeyForMint(ctx, repo.GetExternalKeyForMintParams{
		ID:             externalKeyID,
		OrganizationID: conv.ToPGText(organizationID),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeBadRequest, nil, "external key not found").LogError(ctx, logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading external key").LogError(ctx, logger)
	}

	if row.ExternalKey.Provider != externalKeyProviderGcpKms {
		return nil, oops.E(oops.CodeBadRequest, nil, "AWS KMS keys cannot back a JSON Web Key Set yet; choose a GCP KMS key").LogError(ctx, logger)
	}
	if !row.ResourceName.Valid {
		return nil, oops.E(oops.CodeUnexpected, nil, "gcp kms key is missing its resource name").LogError(ctx, logger)
	}

	// The recorded algorithm is what Gram advertises and signs with, so it is
	// the expectation the key is measured against. It comes back out of a text
	// column, where a bare conversion would accept anything the column holds.
	want, err := gcpkms.ParseSignatureAlgorithm(row.ExternalKey.Algorithm)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "this key records an algorithm Speakeasy cannot sign with; delete it and create it again with RS256 or ES256").LogError(ctx, logger)
	}

	credential, problem, detail, err := s.gcpIdentity.ScreenStoredCredential(ctx, logger, gcpauth.StoredCredential{
		Present:                   row.CredentialID.Valid,
		ImpersonateServiceAccount: row.ImpersonateServiceAccount.String,
		HasWifConfig:              row.WifPoolID.Valid || row.WifProviderID.Valid || row.WifProjectNumber.Valid,
		SkipProjectVerification:   row.SkipProjectVerification.Bool,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "cannot publish a key right now, try again shortly").LogError(ctx, logger)
	}
	if problem != "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "%s", detail).LogError(ctx, logger)
	}

	// TokenSource alone proves nothing: it is lazy by design, so resolving the
	// principal first is what separates "Gram cannot assume this identity" (a
	// grant the customer must add) from a key-level permission problem.
	if _, err := s.gcpIdentity.ResolvePrincipal(ctx, credential); err != nil {
		logger.InfoContext(ctx, "jwks mint could not assume the credential's identity", attr.SlogError(err))
		return nil, oops.E(oops.CodeBadRequest, nil, "cannot authenticate as the backing credential: %s", conv.TruncateDetail(err.Error(), mintDetailMaxLen)).LogError(ctx, logger)
	}

	tokenSource, err := s.gcpIdentity.TokenSource(ctx, credential)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "cannot publish a key right now, try again shortly").LogError(ctx, logger)
	}

	// The signing-client lifecycle lives here and only here: each client owns a
	// gRPC connection that leaks silently without Close. AIS-243 (serving the
	// JWKS document per issuer) is expected to lift this fetch-and-close shape
	// into a shared seam once it exists to show what the seam should be.
	client, err := s.kmsClients(ctx, tokenSource)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "cannot publish a key right now, try again shortly").LogError(ctx, logger)
	}
	defer o11y.LogDefer(ctx, logger, "failed to close gcp kms client", func() error { return client.Close() })

	public, err := client.GetPublicKey(ctx, row.ResourceName.String)
	if err != nil {
		return nil, s.mintPublicKeyError(ctx, logger, err)
	}

	// The KMS-reported algorithm is authoritative for the published document; a
	// disagreement with the recorded algorithm means the row points at the
	// wrong key, and publishing anyway would advertise signatures no verifier
	// accepts. This is the check gcpkms.PublicKey.Algorithm exists to enable.
	if public.Algorithm != want {
		return nil, oops.E(oops.CodeBadRequest, nil, "key signs with %s but is configured as %s; update the key's configuration or point at a %s key", public.Algorithm, want, want).LogError(ctx, logger)
	}

	jwk := jose.JSONWebKey{
		Key:                         public.Key,
		KeyID:                       "",
		Algorithm:                   string(public.Algorithm),
		Use:                         "sig",
		Certificates:                nil,
		CertificatesURL:             nil,
		CertificateThumbprintSHA1:   nil,
		CertificateThumbprintSHA256: nil,
	}

	thumbprint, err := jwk.Thumbprint(crypto.SHA256)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error deriving key thumbprint").LogError(ctx, logger)
	}

	// KeyID is set before marshaling so the stored document carries the same
	// kid as the row it lands in. Thumbprints are unpadded base64url per RFC
	// 7638's kid convention.
	jwk.KeyID = base64.RawURLEncoding.EncodeToString(thumbprint)

	doc, err := jwk.MarshalJSON()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error encoding public jwk").LogError(ctx, logger)
	}

	return &mintedJWK{
		externalKeyID: row.ExternalKey.ID,
		kid:           jwk.KeyID,
		publicJWK:     doc,
	}, nil
}

// mintPublicKeyError maps a GetPublicKey failure onto an error code by the same
// classification the verify probe uses: transient provider trouble is a
// gateway error worth retrying, unrecognised failures are Gram's to
// investigate, and everything else names something the key's owner can fix.
func (s *Service) mintPublicKeyError(ctx context.Context, logger *slog.Logger, err error) error {
	detail := conv.TruncateDetail(err.Error(), mintDetailMaxLen)

	switch gcpkms.ReasonForError(err) {
	case gcpkms.ReasonUnavailable:
		return oops.E(oops.CodeGatewayError, err, "GCP KMS is unreachable right now, try again shortly").LogError(ctx, logger)
	case gcpkms.ReasonUnexpected:
		return oops.E(oops.CodeUnexpected, err, "error reading the key's public half").LogError(ctx, logger)
	default:
		return oops.E(oops.CodeBadRequest, err, "cannot read the key's public half from GCP KMS: %s", detail).LogError(ctx, logger)
	}
}
