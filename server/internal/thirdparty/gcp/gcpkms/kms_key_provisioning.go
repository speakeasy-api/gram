package gcpkms

import (
	"context"
	"errors"
	"fmt"
	"time"

	kmspb "cloud.google.com/go/kms/apiv1/kmspb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// keyGenerationPollInterval is how often a freshly created version is
// re-read while GCP generates its material. Asymmetric versions start in
// PENDING_GENERATION and typically enable within a few seconds.
const keyGenerationPollInterval = time.Second

// keyGenerationTimeout bounds the wait for a version to enable. A version
// still pending after this is a provider problem, reported rather than waited
// out on a request-shaped budget.
const keyGenerationTimeout = 2 * time.Minute

// keyCleanupTimeout bounds reconciliation independently of the failed request.
const keyCleanupTimeout = 10 * time.Second

// CreateSigningKey creates the key and waits for its first version to leave
// PENDING_GENERATION. The version name returned is what Gram records; the key
// name is what IAM bindings attach to.
func (c *kmsSigningClient) CreateSigningKey(ctx context.Context, params CreateSigningKeyParams) (*CreatedSigningKey, error) {
	if err := validateCreateSigningKeyParams(params); err != nil {
		return nil, err
	}

	kmsAlg, err := kmsAlgorithmForJOSE(params.Algorithm)
	if err != nil {
		return nil, err
	}

	key, err := c.kms.CreateCryptoKey(ctx, &kmspb.CreateCryptoKeyRequest{
		Parent:      params.KeyRing,
		CryptoKeyId: params.KeyID,
		CryptoKey: &kmspb.CryptoKey{
			Name:             "",
			Primary:          nil,
			Purpose:          kmspb.CryptoKey_ASYMMETRIC_SIGN,
			CreateTime:       nil,
			NextRotationTime: nil,
			RotationSchedule: nil,
			VersionTemplate: &kmspb.CryptoKeyVersionTemplate{
				ProtectionLevel: kmspb.ProtectionLevel_SOFTWARE,
				Algorithm:       kmsAlg,
			},
			Labels:                        nil,
			ImportOnly:                    false,
			DestroyScheduledDuration:      nil,
			CryptoKeyBackend:              "",
			KeyAccessJustificationsPolicy: nil,
		},
		SkipInitialVersionCreation: false,
	})
	if err != nil {
		if status.Code(err) == codes.AlreadyExists {
			return nil, fmt.Errorf("%w: %s in %s", ErrSigningKeyExists, params.KeyID, params.KeyRing)
		}
		return nil, fmt.Errorf("create gcp kms crypto key: %w", err)
	}

	keyName := key.GetName()
	if keyName == "" {
		return nil, errors.New("create gcp kms crypto key: response omitted the key name")
	}
	versionName := signingKeyVersionName(keyName)

	if err := c.awaitVersionEnabled(ctx, versionName); err != nil {
		// The key already exists even though provisioning failed. Do not use the
		// request's cancellation/deadline for the compensating operation.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), keyCleanupTimeout)
		defer cancel()
		if cleanupErr := c.reconcileFailedKeyGeneration(cleanupCtx, versionName); cleanupErr != nil {
			// Preserve both resource names for manual reconciliation. Wrapping the
			// original error keeps cancellation and gRPC status checks working.
			return nil, fmt.Errorf("created gcp kms key %s, version %s requires reconciliation (%w): %w", keyName, versionName, cleanupErr, err)
		}
		return nil, fmt.Errorf("created gcp kms key %s, version %s is unusable after failed provisioning: %w", keyName, versionName, err)
	}

	return &CreatedSigningKey{KeyName: keyName, KeyVersionName: versionName}, nil
}

// reconcileFailedKeyGeneration makes the new version unusable. CryptoKey
// containers cannot be deleted. GCP forbids disabling/destroying a version in
// PENDING_GENERATION, so wait for generation before attempting a state update.
// The caller must supply a bounded context independent of the failed request.
func (c *kmsSigningClient) reconcileFailedKeyGeneration(ctx context.Context, versionName string) error {
	for {
		version, err := c.kms.GetCryptoKeyVersion(ctx, &kmspb.GetCryptoKeyVersionRequest{Name: versionName})
		switch {
		case ctx.Err() != nil:
			return fmt.Errorf("reconcile gcp kms version: %w", ctx.Err())
		case err == nil:
			switch state := version.GetState(); state {
			case kmspb.CryptoKeyVersion_ENABLED:
				return c.DisableKeyVersion(ctx, versionName)
			case kmspb.CryptoKeyVersion_DISABLED, kmspb.CryptoKeyVersion_DESTROY_SCHEDULED,
				kmspb.CryptoKeyVersion_DESTROYED, kmspb.CryptoKeyVersion_GENERATION_FAILED:
				return nil
			case kmspb.CryptoKeyVersion_PENDING_GENERATION:
			default:
				return fmt.Errorf("cannot reconcile gcp kms version in state %s", state)
			}
		case isTransientKMSError(err):
		default:
			return fmt.Errorf("read gcp kms version for reconciliation: %w", err)
		}

		timer := time.NewTimer(c.keyGenerationPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("reconcile gcp kms version: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

// awaitVersionEnabled polls a version until GCP reports it ENABLED. Any other
// terminal state is an error: a version that was disabled or destroyed out
// from under a create can never sign. Transient read failures are retried
// within the same timeout budget.
func (c *kmsSigningClient) awaitVersionEnabled(ctx context.Context, versionName string) error {
	ctx, cancel := context.WithTimeout(ctx, keyGenerationTimeout)
	defer cancel()

	for {
		version, err := c.kms.GetCryptoKeyVersion(ctx, &kmspb.GetCryptoKeyVersionRequest{Name: versionName})
		switch {
		case err == nil:
			switch state := version.GetState(); state {
			case kmspb.CryptoKeyVersion_ENABLED:
				return nil
			case kmspb.CryptoKeyVersion_PENDING_GENERATION:
			default:
				return fmt.Errorf("gcp kms crypto key version %s is %s, expected ENABLED", versionName, state)
			}
		case ctx.Err() != nil:
			return fmt.Errorf("wait for gcp kms crypto key version %s to enable: %w", versionName, ctx.Err())
		case isTransientKMSError(err):
		default:
			return fmt.Errorf("get gcp kms crypto key version %s: %w", versionName, err)
		}

		timer := time.NewTimer(c.keyGenerationPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("wait for gcp kms crypto key version %s to enable: %w", versionName, ctx.Err())
		case <-timer.C:
		}
	}
}

// isTransientKMSError reports the gRPC codes worth retrying a poll on.
func isTransientKMSError(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return true
	default:
		return false
	}
}

// DisableKeyVersion moves a version to DISABLED. Idempotent on an already
// disabled version.
func (c *kmsSigningClient) DisableKeyVersion(ctx context.Context, versionName string) error {
	if err := ValidateKeyVersionName(versionName); err != nil {
		return err
	}

	_, err := c.kms.UpdateCryptoKeyVersion(ctx, &kmspb.UpdateCryptoKeyVersionRequest{
		CryptoKeyVersion: &kmspb.CryptoKeyVersion{
			Name:                             versionName,
			State:                            kmspb.CryptoKeyVersion_DISABLED,
			ProtectionLevel:                  kmspb.ProtectionLevel_PROTECTION_LEVEL_UNSPECIFIED,
			Algorithm:                        kmspb.CryptoKeyVersion_CRYPTO_KEY_VERSION_ALGORITHM_UNSPECIFIED,
			Attestation:                      nil,
			CreateTime:                       nil,
			GenerateTime:                     nil,
			DestroyTime:                      nil,
			DestroyEventTime:                 nil,
			ImportJob:                        "",
			ImportTime:                       nil,
			ImportFailureReason:              "",
			GenerationFailureReason:          "",
			ExternalDestructionFailureReason: "",
			ExternalProtectionLevelOptions:   nil,
			ReimportEligible:                 false,
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"state"}},
	})
	if err != nil {
		return fmt.Errorf("disable gcp kms crypto key version %s: %w", versionName, err)
	}

	return nil
}

// GrantSignerVerifier adds the service account to the key's IAM policy under
// SignerVerifierRole. Re-granting an existing binding is a no-op, so a retried
// provisioning run does not churn the policy's etag.
func (c *kmsSigningClient) GrantSignerVerifier(ctx context.Context, keyName, serviceAccountEmail string) error {
	if err := ValidateKeyName(keyName); err != nil {
		return err
	}
	if serviceAccountEmail == "" {
		return errors.New("grant gcp kms signer verifier: service account email is required")
	}

	handle := c.kms.ResourceIAM(keyName)
	policy, err := handle.Policy(ctx)
	if err != nil {
		return fmt.Errorf("read gcp kms key iam policy: %w", err)
	}

	member := "serviceAccount:" + serviceAccountEmail
	if policy.HasRole(member, SignerVerifierRole) {
		return nil
	}

	policy.Add(member, SignerVerifierRole)
	if err := handle.SetPolicy(ctx, policy); err != nil {
		return fmt.Errorf("set gcp kms key iam policy: %w", err)
	}

	return nil
}
