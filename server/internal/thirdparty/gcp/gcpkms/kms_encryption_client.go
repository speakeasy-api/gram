package gcpkms

import (
	"context"
	"errors"
	"fmt"

	kms "cloud.google.com/go/kms/apiv1"
	kmspb "cloud.google.com/go/kms/apiv1/kmspb"
	"golang.org/x/oauth2"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

var _ EncryptionProvisioningClient = (*kmsEncryptionClient)(nil)

// kmsEncryptionClient is the EncryptionClient backed by real GCP Cloud KMS.
type kmsEncryptionClient struct {
	kms *kms.KeyManagementClient
}

// NewEncryptionClient opens an authenticated GCP KMS client for symmetric
// encryption. The caller owns its lifetime and MUST Close it: each client holds
// a gRPC connection, so a missed Close leaks one silently.
func NewEncryptionClient(ctx context.Context, tokenSource oauth2.TokenSource) (EncryptionClient, error) {
	return newKMSEncryptionClient(ctx, tokenSource)
}

// NewEncryptionProvisioningClient opens an authenticated GCP KMS client that
// can also provision symmetric keys. The caller MUST Close it.
func NewEncryptionProvisioningClient(ctx context.Context, tokenSource oauth2.TokenSource) (EncryptionProvisioningClient, error) {
	return newKMSEncryptionClient(ctx, tokenSource)
}

func newKMSEncryptionClient(ctx context.Context, tokenSource oauth2.TokenSource) (*kmsEncryptionClient, error) {
	c, err := kms.NewKeyManagementClient(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, fmt.Errorf("build gcp kms client: %w", err)
	}

	return &kmsEncryptionClient{kms: c}, nil
}

// Close releases the underlying gRPC connection.
func (c *kmsEncryptionClient) Close() error {
	if err := c.kms.Close(); err != nil {
		return fmt.Errorf("close gcp kms client: %w", err)
	}

	return nil
}

// Encrypt encrypts under the key's primary version. Both the request and the
// response carry CRC-32C checksums so corruption in transit is detected, and
// the response must name a version of the requested key.
func (c *kmsEncryptionClient) Encrypt(ctx context.Context, keyName string, plaintext, aad []byte) (*EncryptResult, error) {
	if err := ValidateKeyName(keyName); err != nil {
		return nil, err
	}

	resp, err := c.kms.Encrypt(ctx, &kmspb.EncryptRequest{
		Name:                              keyName,
		Plaintext:                         plaintext,
		AdditionalAuthenticatedData:       aad,
		PlaintextCrc32C:                   wrapperspb.Int64(crc32c(plaintext)),
		AdditionalAuthenticatedDataCrc32C: aadChecksum(aad),
	})
	if err != nil {
		return nil, fmt.Errorf("gcp kms encrypt: %w", err)
	}

	switch {
	case !resp.GetVerifiedPlaintextCrc32C():
		return nil, fmt.Errorf("%w: kms did not verify the plaintext checksum", ErrEncryptionResponseInvalid)
	case len(aad) > 0 && !resp.GetVerifiedAdditionalAuthenticatedDataCrc32C():
		return nil, fmt.Errorf("%w: kms did not verify the associated data checksum", ErrEncryptionResponseInvalid)
	case resp.GetCiphertextCrc32C() == nil:
		return nil, fmt.Errorf("%w: response omitted the ciphertext checksum", ErrEncryptionResponseInvalid)
	case resp.GetCiphertextCrc32C().GetValue() != crc32c(resp.GetCiphertext()):
		return nil, fmt.Errorf("%w: ciphertext checksum mismatch", ErrEncryptionResponseInvalid)
	case !keyVersionOf(keyName, resp.GetName()):
		return nil, fmt.Errorf("%w: response names %q, requested a version of %q", ErrEncryptionResponseInvalid, resp.GetName(), keyName)
	}

	return &EncryptResult{Ciphertext: resp.GetCiphertext(), KeyVersionName: resp.GetName()}, nil
}

// Decrypt decrypts a ciphertext produced under keyName, checking CRC-32C
// checksums in both directions.
func (c *kmsEncryptionClient) Decrypt(ctx context.Context, keyName string, ciphertext, aad []byte) ([]byte, error) {
	if err := ValidateKeyName(keyName); err != nil {
		return nil, err
	}

	resp, err := c.kms.Decrypt(ctx, &kmspb.DecryptRequest{
		Name:                              keyName,
		Ciphertext:                        ciphertext,
		AdditionalAuthenticatedData:       aad,
		CiphertextCrc32C:                  wrapperspb.Int64(crc32c(ciphertext)),
		AdditionalAuthenticatedDataCrc32C: aadChecksum(aad),
	})
	if err != nil {
		return nil, fmt.Errorf("gcp kms decrypt: %w", err)
	}

	switch {
	case resp.GetPlaintextCrc32C() == nil:
		return nil, fmt.Errorf("%w: response omitted the plaintext checksum", ErrEncryptionResponseInvalid)
	case resp.GetPlaintextCrc32C().GetValue() != crc32c(resp.GetPlaintext()):
		return nil, fmt.Errorf("%w: plaintext checksum mismatch", ErrEncryptionResponseInvalid)
	}

	return resp.GetPlaintext(), nil
}

// aadChecksum is the associated data checksum to send. An empty value sends no
// checksum, because KMS only reports one as verified when data was supplied.
func aadChecksum(aad []byte) *wrapperspb.Int64Value {
	if len(aad) == 0 {
		return nil
	}

	return wrapperspb.Int64(crc32c(aad))
}

// CreateEncryptionKey creates a software-protected GOOGLE_SYMMETRIC_ENCRYPTION
// key. Symmetric material is generated synchronously, so the first version
// must already be the enabled primary in the response. The key has no
// rotation schedule: rotation is driven by Speakeasy so every wrap can be
// re-wrapped before an old version is disabled.
func (c *kmsEncryptionClient) CreateEncryptionKey(ctx context.Context, params CreateEncryptionKeyParams) (*CreatedEncryptionKey, error) {
	if err := validateCreateEncryptionKeyParams(params); err != nil {
		return nil, err
	}

	keyName := params.KeyRing + "/cryptoKeys/" + params.KeyID
	key, err := c.kms.CreateCryptoKey(ctx, &kmspb.CreateCryptoKeyRequest{
		Parent:      params.KeyRing,
		CryptoKeyId: params.KeyID,
		CryptoKey: &kmspb.CryptoKey{
			Name:             "",
			Primary:          nil,
			Purpose:          kmspb.CryptoKey_ENCRYPT_DECRYPT,
			CreateTime:       nil,
			NextRotationTime: nil,
			RotationSchedule: nil,
			VersionTemplate: &kmspb.CryptoKeyVersionTemplate{
				ProtectionLevel: kmspb.ProtectionLevel_SOFTWARE,
				Algorithm:       kmspb.CryptoKeyVersion_GOOGLE_SYMMETRIC_ENCRYPTION,
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
		// Key ids are random, so AlreadyExists means an earlier attempt of this
		// create landed and its response was lost before a retry: the key is
		// ours and must not be left enabled.
		alreadyExists := status.Code(err) == codes.AlreadyExists
		if alreadyExists || isTransientKMSError(err) || ctx.Err() != nil {
			// GCP may have persisted the key even though the response was lost;
			// the name is deterministic, so disable the key we would own.
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), keyCleanupTimeout)
			defer cancel()
			if cleanupErr := c.reconcileUncertainEncryptionKeyCreate(cleanupCtx, keyName); cleanupErr != nil {
				return nil, fmt.Errorf("gcp kms key %s may exist and requires reconciliation (%w): %w", keyName, cleanupErr, err)
			}
			if alreadyExists {
				return nil, fmt.Errorf("%w: %s in %s, its versions were disabled: %w", ErrKeyExists, params.KeyID, params.KeyRing, err)
			}
			return nil, fmt.Errorf("gcp kms key %s may exist, its versions were disabled if present: %w", keyName, err)
		}
		return nil, fmt.Errorf("create gcp kms crypto key: %w", err)
	}

	if key.GetName() != keyName {
		return nil, fmt.Errorf("create gcp kms crypto key: response names %q, requested %q", key.GetName(), keyName)
	}
	if state := key.GetPrimary().GetState(); state != kmspb.CryptoKeyVersion_ENABLED {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), keyCleanupTimeout)
		defer cancel()
		cleanupErr := c.DisableEncryptionKey(cleanupCtx, keyName)
		return nil, errors.Join(fmt.Errorf("created gcp kms key %s, but its primary version is %s, expected ENABLED", keyName, state), cleanupErr)
	}

	return &CreatedEncryptionKey{KeyName: keyName}, nil
}

// reconcileUncertainEncryptionKeyCreate disables a key whose create response
// was lost. NotFound means the create never landed and nothing needs doing.
func (c *kmsEncryptionClient) reconcileUncertainEncryptionKeyCreate(ctx context.Context, keyName string) error {
	_, err := c.kms.GetCryptoKey(ctx, &kmspb.GetCryptoKeyRequest{Name: keyName})
	switch {
	case status.Code(err) == codes.NotFound:
		return nil
	case err != nil && !isTransientKMSError(err):
		return fmt.Errorf("read gcp kms key after uncertain create: %w", err)
	}

	return c.DisableEncryptionKey(ctx, keyName)
}

// DisableEncryptionKey disables every enabled version of the key. Versions
// already disabled or destroyed are left alone.
func (c *kmsEncryptionClient) DisableEncryptionKey(ctx context.Context, keyName string) error {
	if err := ValidateKeyName(keyName); err != nil {
		return err
	}

	it := c.kms.ListCryptoKeyVersions(ctx, &kmspb.ListCryptoKeyVersionsRequest{
		Parent:    keyName,
		PageSize:  0,
		PageToken: "",
		View:      kmspb.CryptoKeyVersion_CRYPTO_KEY_VERSION_VIEW_UNSPECIFIED,
		Filter:    "state=ENABLED",
		OrderBy:   "",
	})
	for {
		version, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("list gcp kms key versions: %w", err)
		}

		if err := disableKeyVersion(ctx, c.kms, version.GetName()); err != nil {
			return err
		}
	}
}

// GrantEncrypterDecrypter binds EncrypterDecrypterRole for the service account
// on one crypto key, never the ring.
func (c *kmsEncryptionClient) GrantEncrypterDecrypter(ctx context.Context, keyName, serviceAccountEmail string) error {
	return grantKeyRole(ctx, c.kms, keyName, serviceAccountEmail, EncrypterDecrypterRole)
}

// RevokeEncrypterDecrypter removes the EncrypterDecrypterRole binding for the
// service account on one crypto key.
func (c *kmsEncryptionClient) RevokeEncrypterDecrypter(ctx context.Context, keyName, serviceAccountEmail string) error {
	return revokeKeyRole(ctx, c.kms, keyName, serviceAccountEmail, EncrypterDecrypterRole)
}

// validateCreateEncryptionKeyParams checks everything that can be checked
// before a request leaves the process.
func validateCreateEncryptionKeyParams(params CreateEncryptionKeyParams) error {
	if err := ValidateKeyRingName(params.KeyRing); err != nil {
		return err
	}
	if !keyIDPattern.MatchString(params.KeyID) {
		return fmt.Errorf("%w: %q", ErrInvalidKeyID, params.KeyID)
	}

	return nil
}
