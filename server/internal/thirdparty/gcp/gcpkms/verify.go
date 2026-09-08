package gcpkms

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"errors"
	"fmt"

	jose "github.com/go-jose/go-jose/v4"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ProbePayload is the payload signed during verification.
//
// It is deliberately self-describing. A verification probe performs a real
// signing operation, which lands in the key owner's Cloud Audit Log, so an
// operator investigating an unexplained signing event needs a way to recognise
// it as ours.
const ProbePayload = "gram-key-verification-probe"

// VerifyReason is the machine-readable outcome of a verification probe. It
// exists so callers can tell the three cases apart that Detail alone conflates:
// something the key's owner must fix, something that will pass on retry, and
// something that is Gram's fault.
type VerifyReason string

const (
	// ReasonVerified means the key signed and the signature validated.
	ReasonVerified VerifyReason = "verified"

	// ReasonInvalidResourceName means the stored resource name is not a
	// fully-qualified key version. Gram's own record is wrong; no call was made.
	ReasonInvalidResourceName VerifyReason = "invalid_resource_name"

	// ReasonKeyNotFound means the key version does not exist.
	ReasonKeyNotFound VerifyReason = "key_not_found"

	// ReasonPermissionDenied means the credential's identity cannot use the key.
	// Usually a missing roles/cloudkms.signerVerifier grant.
	ReasonPermissionDenied VerifyReason = "permission_denied"

	// ReasonKeyUnusable means the key version exists but cannot sign right now:
	// DISABLED, DESTROYED, or still PENDING_GENERATION.
	ReasonKeyUnusable VerifyReason = "key_unusable"

	// ReasonUnsupportedAlgorithm means the key signs with an algorithm Gram does
	// not publish, such as RSA-PSS.
	ReasonUnsupportedAlgorithm VerifyReason = "unsupported_algorithm"

	// ReasonAlgorithmMismatch means the key is healthy but signs with a different
	// algorithm than the one configured against it.
	ReasonAlgorithmMismatch VerifyReason = "algorithm_mismatch"

	// ReasonSignatureInvalid means the key produced a signature that does not
	// validate against its own public half. This should not happen; treat it as
	// corruption or a provider fault rather than a configuration problem.
	ReasonSignatureInvalid VerifyReason = "signature_invalid"

	// ReasonUnavailable means the probe could not complete for a transient
	// reason: a timeout, a rate limit, or KMS being briefly unreachable. It is
	// the one reason worth retrying; every other failure is stable.
	ReasonUnavailable VerifyReason = "unavailable"

	// ReasonUnexpected means the probe failed in a way this package does not
	// recognise. Worth investigating rather than showing to a customer as
	// something they can fix.
	ReasonUnexpected VerifyReason = "unexpected"
)

// VerifyResult reports whether a key is usable for signing. It is a value rather
// than an error because most negative outcomes here are normal, reportable
// states a human can act on, and callers surface them rather than treating them
// as faults. Reason distinguishes those from the ones that are not.
type VerifyResult struct {
	// Verified is true only when the key produced a signature that validated
	// against its own public half.
	Verified bool

	// Reason is the machine-readable outcome. Callers should branch on this
	// rather than on Detail.
	Reason VerifyReason

	// Algorithm is the JOSE algorithm the key actually signs with. It is set
	// whenever the public key was readable, including on an algorithm mismatch,
	// so callers can report what the key really is.
	Algorithm jose.SignatureAlgorithm

	// Detail explains a negative result in terms a human can act on. It may carry
	// provider error text, so treat it as diagnostic output rather than copy to
	// show a customer verbatim. Empty when Verified is true.
	Detail string
}

// VerifySigningKey proves end to end that a credential can reach a key AND use
// it to sign: it reads the public half, confirms the key's algorithm matches the
// one expected, signs a probe digest, and verifies that signature locally
// against the fetched public key.
//
// Nothing is persisted. Two KMS calls are made, one of which is a real signing
// operation.
func VerifySigningKey(ctx context.Context, client SigningClient, resourceName string, want jose.SignatureAlgorithm) VerifyResult {
	public, err := client.GetPublicKey(ctx, resourceName)
	if err != nil {
		return failedVerify(ReasonForError(err), "", err)
	}

	// The stored algorithm drives how Gram advertises and signs with this key, so
	// a mismatch is fatal even though the key itself is perfectly healthy: signing
	// RS256 with an RSA-PSS key mints tokens no verifier accepts.
	if public.Algorithm != want {
		return VerifyResult{
			Verified:  false,
			Reason:    ReasonAlgorithmMismatch,
			Algorithm: public.Algorithm,
			Detail: fmt.Sprintf(
				"key signs with %s but is configured as %s; update the configured algorithm or point at a %s key",
				public.Algorithm, want, want,
			),
		}
	}

	digest, err := digestPayload(public.Algorithm, []byte(ProbePayload))
	if err != nil {
		return failedVerify(ReasonForError(err), public.Algorithm, err)
	}

	signature, err := client.AsymmetricSign(ctx, resourceName, public.Algorithm, digest)
	if err != nil {
		return failedVerify(ReasonForError(err), public.Algorithm, err)
	}

	if err := verifySignature(public.Key, digest, signature); err != nil {
		return failedVerify(ReasonSignatureInvalid, public.Algorithm, err)
	}

	return VerifyResult{Verified: true, Reason: ReasonVerified, Algorithm: public.Algorithm, Detail: ""}
}

// failedVerify builds a negative result, keeping the error's text as the detail
// so a negative outcome always explains itself.
func failedVerify(reason VerifyReason, alg jose.SignatureAlgorithm, err error) VerifyResult {
	return VerifyResult{Verified: false, Reason: reason, Algorithm: alg, Detail: err.Error()}
}

// ReasonForError maps a KMS call failure onto the reason a caller acts on.
// Google API errors arrive as gRPC statuses even through the REST transport, so
// the status code carries the distinction between "the customer must fix
// something" and "try again". Exported because callers beyond the verify probe
// (such as the JWKS mint path reading a key's public half) need the same
// classification to pick an error code.
func ReasonForError(err error) VerifyReason {
	switch {
	case errors.Is(err, ErrInvalidResourceName):
		return ReasonInvalidResourceName
	case errors.Is(err, ErrUnsupportedAlgorithm):
		return ReasonUnsupportedAlgorithm
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		// A bare context error carries no gRPC status, so without this it would
		// fall through to ReasonUnexpected and callers would not retry something
		// that is purely transient. gRPC returns a proper DeadlineExceeded status
		// for in-call expiry, but the retry layer can surface the raw error.
		return ReasonUnavailable
	}

	sts, ok := status.FromError(err)
	if !ok {
		return ReasonUnexpected
	}

	switch sts.Code() {
	case codes.PermissionDenied, codes.Unauthenticated:
		return ReasonPermissionDenied
	case codes.NotFound:
		return ReasonKeyNotFound
	case codes.FailedPrecondition:
		return ReasonKeyUnusable
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Aborted:
		return ReasonUnavailable
	default:
		return ReasonUnexpected
	}
}

// verifySignature checks a provider-encoded signature against the public key
// that should have produced it: PKCS#1 v1.5 for RSA, ASN.1 DER for ECDSA.
func verifySignature(public crypto.PublicKey, digest, signature []byte) error {
	switch pub := public.(type) {
	case *rsa.PublicKey:
		if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest, signature); err != nil {
			return fmt.Errorf("signature did not verify against the key's own public half: %w", err)
		}
		return nil
	case *ecdsa.PublicKey:
		if !ecdsa.VerifyASN1(pub, digest, signature) {
			return errors.New("signature did not verify against the key's own public half")
		}
		return nil
	default:
		return fmt.Errorf("unsupported public key type %T", public)
	}
}
