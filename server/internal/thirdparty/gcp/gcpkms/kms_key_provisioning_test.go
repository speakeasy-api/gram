package gcpkms

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/iam/apiv1/iampb"
	kms "cloud.google.com/go/kms/apiv1"
	kmspb "cloud.google.com/go/kms/apiv1/kmspb"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const testKeyRing = "projects/gram/locations/global/keyRings/okta"

// fakeKeyRing is an in-process KeyManagementService plus IAMPolicy service
// that remembers the keys created in it, so the create path — request shape,
// the pending-generation wait, and the per-key IAM grant — is exercised
// without a GCP network path.
type fakeKeyRing struct {
	kmspb.UnimplementedKeyManagementServiceServer
	iampb.UnimplementedIAMPolicyServer

	mu sync.Mutex

	// keys maps crypto key names to their create requests.
	keys map[string]*kmspb.CreateCryptoKeyRequest

	// pendingReads is how many GetCryptoKeyVersion calls answer
	// PENDING_GENERATION before the version reports ENABLED.
	pendingReads int

	versionReads int

	// policies maps resource names to their IAM policies.
	policies map[string]*iampb.Policy

	setPolicyCalls int

	// transientReads is how many GetCryptoKeyVersion calls fail Unavailable
	// before answering.
	transientReads int

	// disabled records the versions DisableKeyVersion was called for.
	disabled []string

	// createUnavailable persists the key but answers CreateCryptoKey Unavailable.
	createUnavailable bool
}

func newFakeKeyRing() *fakeKeyRing {
	return &fakeKeyRing{
		keys:     map[string]*kmspb.CreateCryptoKeyRequest{},
		policies: map[string]*iampb.Policy{},
	}
}

func (f *fakeKeyRing) CreateCryptoKey(_ context.Context, req *kmspb.CreateCryptoKeyRequest) (*kmspb.CryptoKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	name := req.GetParent() + "/cryptoKeys/" + req.GetCryptoKeyId()
	if f.createUnavailable {
		// The key lands but every response is lost, including gapic retries.
		f.keys[name] = req
		return nil, status.Error(codes.Unavailable, "response lost") //nolint:wrapcheck // the fake speaks gRPC status codes, as GCP does
	}
	if _, exists := f.keys[name]; exists {
		return nil, status.Error(codes.AlreadyExists, "crypto key exists") //nolint:wrapcheck // the fake speaks gRPC status codes, as GCP does
	}
	f.keys[name] = req

	return &kmspb.CryptoKey{Name: name, Purpose: req.GetCryptoKey().GetPurpose(), VersionTemplate: req.GetCryptoKey().GetVersionTemplate()}, nil
}

func (f *fakeKeyRing) GetCryptoKeyVersion(_ context.Context, req *kmspb.GetCryptoKeyVersionRequest) (*kmspb.CryptoKeyVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.versionReads++
	if f.transientReads > 0 {
		f.transientReads--
		return nil, status.Error(codes.Unavailable, "try again") //nolint:wrapcheck // the fake speaks gRPC status codes, as GCP does
	}
	state := kmspb.CryptoKeyVersion_ENABLED
	if f.versionReads <= f.pendingReads {
		state = kmspb.CryptoKeyVersion_PENDING_GENERATION
	}

	return &kmspb.CryptoKeyVersion{Name: req.GetName(), State: state}, nil
}

func (f *fakeKeyRing) UpdateCryptoKeyVersion(_ context.Context, req *kmspb.UpdateCryptoKeyVersionRequest) (*kmspb.CryptoKeyVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if req.GetCryptoKeyVersion().GetState() != kmspb.CryptoKeyVersion_DISABLED || len(req.GetUpdateMask().GetPaths()) != 1 || req.GetUpdateMask().GetPaths()[0] != "state" {
		return nil, status.Error(codes.InvalidArgument, "only state=DISABLED updates are faked") //nolint:wrapcheck // the fake speaks gRPC status codes, as GCP does
	}
	if f.pendingReads > 0 && f.versionReads <= f.pendingReads {
		return nil, status.Error(codes.FailedPrecondition, "cannot disable PENDING_GENERATION") //nolint:wrapcheck // fake gRPC response
	}
	f.disabled = append(f.disabled, req.GetCryptoKeyVersion().GetName())

	return req.GetCryptoKeyVersion(), nil
}

func (f *fakeKeyRing) disabledVersions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.disabled...)
}

func (f *fakeKeyRing) GetIamPolicy(_ context.Context, req *iampb.GetIamPolicyRequest) (*iampb.Policy, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if policy, ok := f.policies[req.GetResource()]; ok {
		return policy, nil
	}

	return &iampb.Policy{}, nil
}

func (f *fakeKeyRing) SetIamPolicy(_ context.Context, req *iampb.SetIamPolicyRequest) (*iampb.Policy, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.setPolicyCalls++
	f.policies[req.GetResource()] = req.GetPolicy()

	return req.GetPolicy(), nil
}

func (f *fakeKeyRing) createdKey(t *testing.T, name string) *kmspb.CreateCryptoKeyRequest {
	t.Helper()

	f.mu.Lock()
	defer f.mu.Unlock()

	req, ok := f.keys[name]
	require.True(t, ok, "key %s was not created", name)

	return req
}

func (f *fakeKeyRing) members(resource string, role string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, binding := range f.policies[resource].GetBindings() {
		if binding.GetRole() == role {
			return binding.GetMembers()
		}
	}

	return nil
}

// newFakeProvisioningClient wires the real client to the given servers over bufconn.
func newFakeProvisioningClient(t *testing.T, kmsServer kmspb.KeyManagementServiceServer, iamServer iampb.IAMPolicyServer) *kmsSigningClient {
	t.Helper()

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	kmspb.RegisterKeyManagementServiceServer(server, kmsServer)
	iampb.RegisterIAMPolicyServer(server, iamServer)

	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	raw, err := kms.NewKeyManagementClient(t.Context(), option.WithGRPCConn(conn))
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })

	// Zero poll interval: the fake flips to ENABLED after a fixed number of
	// reads, so the wait is exercised without the test sleeping through it.
	return &kmsSigningClient{kms: raw, keyGenerationPoll: 0}
}

func TestNewSigningKeyID_IsRandomAndValid(t *testing.T) {
	t.Parallel()

	a, err := NewSigningKeyID("okta")
	require.NoError(t, err)
	b, err := NewSigningKeyID("okta")
	require.NoError(t, err)

	require.NotEqual(t, a, b)
	require.Regexp(t, `^okta-[0-9a-f]{32}$`, a)
	require.NoError(t, validateCreateSigningKeyParams(CreateSigningKeyParams{KeyRing: testKeyRing, KeyID: a, Algorithm: jose.RS256}))
}

func TestValidateKeyRingName(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateKeyRingName(testKeyRing))
	require.ErrorIs(t, ValidateKeyRingName(testKeyRing+"/cryptoKeys/k"), ErrInvalidKeyRingName)
	require.ErrorIs(t, ValidateKeyRingName("projects/gram/locations/global"), ErrInvalidKeyRingName)
	require.ErrorIs(t, ValidateKeyRingName(""), ErrInvalidKeyRingName)
}

func TestValidateKeyName(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateKeyName(testKeyRing+"/cryptoKeys/k"))
	require.ErrorIs(t, ValidateKeyName(testKeyRing), ErrInvalidResourceName)
	require.ErrorIs(t, ValidateKeyName(testKeyRing+"/cryptoKeys/k/cryptoKeyVersions/1"), ErrInvalidResourceName)
}

// The key is created for signing, with the KMS algorithm the JOSE algorithm
// maps to, and the returned version name is the one Gram records: a
// cryptoKeyVersions path GetPublicKey and AsymmetricSign accept.
func TestCreateSigningKey_CreatesAsymmetricSignKeyAndWaitsForEnabled(t *testing.T) {
	t.Parallel()

	fake := newFakeKeyRing()
	fake.pendingReads = 2
	client := newFakeProvisioningClient(t, fake, fake)

	created, err := client.CreateSigningKey(t.Context(), CreateSigningKeyParams{
		KeyRing:   testKeyRing,
		KeyID:     "okta-abc",
		Algorithm: jose.RS256,
	})
	require.NoError(t, err)

	require.Equal(t, testKeyRing+"/cryptoKeys/okta-abc", created.KeyName)
	require.Equal(t, created.KeyName+"/cryptoKeyVersions/1", created.KeyVersionName)
	require.NoError(t, ValidateKeyVersionName(created.KeyVersionName))

	req := fake.createdKey(t, created.KeyName)
	require.Equal(t, kmspb.CryptoKey_ASYMMETRIC_SIGN, req.GetCryptoKey().GetPurpose())
	require.Equal(t, kmspb.CryptoKeyVersion_RSA_SIGN_PKCS1_2048_SHA256, req.GetCryptoKey().GetVersionTemplate().GetAlgorithm())
	require.False(t, req.GetSkipInitialVersionCreation())
	require.Equal(t, 3, fake.versionReads, "two pending reads then the enabled one")
}

func TestCreateSigningKey_ES256UsesP256(t *testing.T) {
	t.Parallel()

	fake := newFakeKeyRing()
	client := newFakeProvisioningClient(t, fake, fake)

	created, err := client.CreateSigningKey(t.Context(), CreateSigningKeyParams{
		KeyRing:   testKeyRing,
		KeyID:     "okta-ec",
		Algorithm: jose.ES256,
	})
	require.NoError(t, err)
	require.Equal(t, kmspb.CryptoKeyVersion_EC_SIGN_P256_SHA256, fake.createdKey(t, created.KeyName).GetCryptoKey().GetVersionTemplate().GetAlgorithm())
}

func TestCreateSigningKey_RejectsBadInputsBeforeCalling(t *testing.T) {
	t.Parallel()

	fake := newFakeKeyRing()
	client := newFakeProvisioningClient(t, fake, fake)

	_, err := client.CreateSigningKey(t.Context(), CreateSigningKeyParams{KeyRing: "not-a-ring", KeyID: "okta-abc", Algorithm: jose.RS256})
	require.ErrorIs(t, err, ErrInvalidKeyRingName)

	_, err = client.CreateSigningKey(t.Context(), CreateSigningKeyParams{KeyRing: testKeyRing, KeyID: "has/slash", Algorithm: jose.RS256})
	require.ErrorIs(t, err, ErrInvalidKeyID)

	_, err = client.CreateSigningKey(t.Context(), CreateSigningKeyParams{KeyRing: testKeyRing, KeyID: "okta-abc", Algorithm: jose.PS256})
	require.ErrorIs(t, err, ErrUnsupportedAlgorithm)

	require.Empty(t, fake.keys)
}

func TestCreateSigningKey_ReportsExistingKey(t *testing.T) {
	t.Parallel()

	fake := newFakeKeyRing()
	client := newFakeProvisioningClient(t, fake, fake)
	params := CreateSigningKeyParams{KeyRing: testKeyRing, KeyID: "okta-dup", Algorithm: jose.RS256}

	_, err := client.CreateSigningKey(t.Context(), params)
	require.NoError(t, err)

	_, err = client.CreateSigningKey(t.Context(), params)
	require.ErrorIs(t, err, ErrSigningKeyExists)
}

// A version that ends in any state but ENABLED can never sign, so the create
// reports it rather than handing back a key that fails at first use.
func TestCreateSigningKey_RefusesNonEnabledVersion(t *testing.T) {
	t.Parallel()

	fake := &disabledVersionRing{fakeKeyRing: newFakeKeyRing()}
	client := newFakeProvisioningClient(t, fake, fake.fakeKeyRing)

	_, err := client.CreateSigningKey(t.Context(), CreateSigningKeyParams{KeyRing: testKeyRing, KeyID: "okta-dis", Algorithm: jose.RS256})
	require.ErrorContains(t, err, "is DISABLED, expected ENABLED")
}

// disabledVersionRing answers every version read with DISABLED.
type disabledVersionRing struct {
	*fakeKeyRing
}

func (f *disabledVersionRing) GetCryptoKeyVersion(_ context.Context, req *kmspb.GetCryptoKeyVersionRequest) (*kmspb.CryptoKeyVersion, error) {
	return &kmspb.CryptoKeyVersion{Name: req.GetName(), State: kmspb.CryptoKeyVersion_DISABLED}, nil
}

// A transient Unavailable while the version generates is retried, not surfaced.
func TestCreateSigningKey_RetriesTransientVersionReads(t *testing.T) {
	t.Parallel()

	fake := newFakeKeyRing()
	fake.transientReads = 2
	fake.pendingReads = 3
	client := newFakeProvisioningClient(t, fake, fake)

	created, err := client.CreateSigningKey(t.Context(), CreateSigningKeyParams{KeyRing: testKeyRing, KeyID: "okta-retry", Algorithm: jose.RS256})
	require.NoError(t, err)
	require.Equal(t, testKeyRing+"/cryptoKeys/okta-retry/cryptoKeyVersions/1", created.KeyVersionName)
	require.Equal(t, 4, fake.versionReads, "two unavailable, one pending, one enabled")
}

// A non-transient read error is surfaced at once.
func TestCreateSigningKey_SurfacesPermanentVersionReadErrors(t *testing.T) {
	t.Parallel()

	fake := &permissionDeniedRing{fakeKeyRing: newFakeKeyRing()}
	client := newFakeProvisioningClient(t, fake, fake.fakeKeyRing)

	_, err := client.CreateSigningKey(t.Context(), CreateSigningKeyParams{KeyRing: testKeyRing, KeyID: "okta-denied", Algorithm: jose.RS256})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	require.ErrorContains(t, err, "requires reconciliation")
	require.ErrorContains(t, err, testKeyRing+"/cryptoKeys/okta-denied/cryptoKeyVersions/1")
}

// permissionDeniedRing fails every version read with PermissionDenied.
type permissionDeniedRing struct {
	*fakeKeyRing
}

func (f *permissionDeniedRing) GetCryptoKeyVersion(_ context.Context, _ *kmspb.GetCryptoKeyVersionRequest) (*kmspb.CryptoKeyVersion, error) {
	return nil, status.Error(codes.PermissionDenied, "no") //nolint:wrapcheck // the fake speaks gRPC status codes, as GCP does
}

// Cancelling the context ends the wait even while reads keep failing transiently.
func TestCreateSigningKey_StopsWaitingOnContextCancel(t *testing.T) {
	t.Parallel()

	fake := newFakeKeyRing()
	fake.transientReads = 1 << 30
	client := newFakeProvisioningClient(t, fake, fake)
	client.keyGenerationPoll = time.Hour

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := client.CreateSigningKey(ctx, CreateSigningKeyParams{KeyRing: testKeyRing, KeyID: "okta-cancel", Algorithm: jose.RS256})
		done <- err
	}()

	require.Eventually(t, func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return fake.versionReads >= 1
	}, 5*time.Second, 5*time.Millisecond)
	fake.mu.Lock()
	fake.transientReads = 0
	fake.mu.Unlock()
	cancel()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, []string{testKeyRing + "/cryptoKeys/okta-cancel/cryptoKeyVersions/1"}, fake.disabledVersions())
	case <-time.After(5 * time.Second):
		t.Fatal("create did not return after cancel")
	}
}

func TestDisableKeyVersion_DisablesOnlyState(t *testing.T) {
	t.Parallel()

	fake := newFakeKeyRing()
	client := newFakeProvisioningClient(t, fake, fake)
	versionName := testKeyRing + "/cryptoKeys/okta-dis/cryptoKeyVersions/1"

	require.NoError(t, client.DisableKeyVersion(t.Context(), versionName))
	require.Equal(t, []string{versionName}, fake.disabledVersions())

	require.ErrorIs(t, client.DisableKeyVersion(t.Context(), testKeyRing+"/cryptoKeys/okta-dis"), ErrInvalidResourceName)
}

// The grant lands on the key resource, not the ring, and re-granting is a
// no-op rather than a second SetIamPolicy.
func TestGrantSignerVerifier_BindsRoleOnTheKeyOnly(t *testing.T) {
	t.Parallel()

	fake := newFakeKeyRing()
	client := newFakeProvisioningClient(t, fake, fake)
	keyName := testKeyRing + "/cryptoKeys/okta-grant"

	require.NoError(t, client.GrantSignerVerifier(t.Context(), keyName, "signer@speakeasy-signing.iam.gserviceaccount.com"))
	require.Equal(t, []string{"serviceAccount:signer@speakeasy-signing.iam.gserviceaccount.com"}, fake.members(keyName, string(SignerVerifierRole)))
	require.Nil(t, fake.members(testKeyRing, string(SignerVerifierRole)), "the ring must carry no binding")
	require.Equal(t, 1, fake.setPolicyCalls)

	require.NoError(t, client.GrantSignerVerifier(t.Context(), keyName, "signer@speakeasy-signing.iam.gserviceaccount.com"))
	require.Equal(t, 1, fake.setPolicyCalls, "an existing binding is not rewritten")
}

func TestRevokeSignerVerifier_RemovesBindingAndIgnoresAbsent(t *testing.T) {
	t.Parallel()

	fake := newFakeKeyRing()
	client := newFakeProvisioningClient(t, fake, fake)
	keyName := testKeyRing + "/cryptoKeys/okta-revoke"
	sa := "signer@speakeasy-signing.iam.gserviceaccount.com"

	require.NoError(t, client.RevokeSignerVerifier(t.Context(), keyName, sa))
	require.Equal(t, 0, fake.setPolicyCalls, "revoking an absent binding writes nothing")

	require.NoError(t, client.GrantSignerVerifier(t.Context(), keyName, sa))
	require.NoError(t, client.RevokeSignerVerifier(t.Context(), keyName, sa))
	require.Nil(t, fake.members(keyName, string(SignerVerifierRole)))
	require.Equal(t, 2, fake.setPolicyCalls)

	require.ErrorIs(t, client.RevokeSignerVerifier(t.Context(), testKeyRing, sa), ErrInvalidResourceName)
	require.Error(t, client.RevokeSignerVerifier(t.Context(), keyName, ""))
}

func TestCreateSigningKey_ReconcilesUncertainCreate(t *testing.T) {
	t.Parallel()

	fake := newFakeKeyRing()
	fake.createUnavailable = true
	client := newFakeProvisioningClient(t, fake, fake)

	// gapic retries Unavailable; a short deadline exhausts them the way a real outage does.
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	_, err := client.CreateSigningKey(ctx, CreateSigningKeyParams{KeyRing: testKeyRing, KeyID: "okta-lost", Algorithm: jose.RS256})
	require.Error(t, err)
	require.ErrorContains(t, err, "may exist", "the lost create is reported, not hidden")
	require.ErrorContains(t, err, testKeyRing+"/cryptoKeys/okta-lost/cryptoKeyVersions/1")
	require.Equal(t, []string{testKeyRing + "/cryptoKeys/okta-lost/cryptoKeyVersions/1"}, fake.disabledVersions())
}

func TestGrantSignerVerifier_RejectsRingAndVersionNames(t *testing.T) {
	t.Parallel()

	fake := newFakeKeyRing()
	client := newFakeProvisioningClient(t, fake, fake)

	require.ErrorIs(t, client.GrantSignerVerifier(t.Context(), testKeyRing, "sa@p.iam.gserviceaccount.com"), ErrInvalidResourceName)
	require.ErrorIs(t, client.GrantSignerVerifier(t.Context(), testKeyRing+"/cryptoKeys/k/cryptoKeyVersions/1", "sa@p.iam.gserviceaccount.com"), ErrInvalidResourceName)
	require.Error(t, client.GrantSignerVerifier(t.Context(), testKeyRing+"/cryptoKeys/k", ""))
}

// The local stand-in names a key in the ring that its one in-process key then
// answers for, so provisioning can run end to end without GCP.
func TestLocalSigningClient_CreateSigningKeyNamesItsKey(t *testing.T) {
	t.Parallel()

	client, err := NewLocalSigningClient(jose.RS256)
	require.NoError(t, err)

	created, err := client.CreateSigningKey(t.Context(), CreateSigningKeyParams{KeyRing: testKeyRing, KeyID: "okta-local", Algorithm: jose.RS256})
	require.NoError(t, err)
	require.Equal(t, testKeyRing+"/cryptoKeys/okta-local/cryptoKeyVersions/1", created.KeyVersionName)

	public, err := client.GetPublicKey(t.Context(), created.KeyVersionName)
	require.NoError(t, err)
	require.Equal(t, jose.RS256, public.Algorithm)

	require.NoError(t, client.GrantSignerVerifier(t.Context(), created.KeyName, "sa@p.iam.gserviceaccount.com"))
	require.NoError(t, client.DisableKeyVersion(t.Context(), created.KeyVersionName))
	require.ErrorIs(t, client.DisableKeyVersion(t.Context(), created.KeyName), ErrInvalidResourceName)

	_, err = client.CreateSigningKey(t.Context(), CreateSigningKeyParams{KeyRing: testKeyRing, KeyID: "okta-ec", Algorithm: jose.ES256})
	require.ErrorIs(t, err, ErrUnsupportedAlgorithm, "the local client holds one algorithm")

	_, err = client.CreateSigningKey(t.Context(), CreateSigningKeyParams{KeyRing: "ring", KeyID: "okta-local", Algorithm: jose.RS256})
	require.ErrorIs(t, err, ErrInvalidKeyRingName)
}

// The first read fails provisioning; subsequent reads model generation continuing
// independently in GCP. Cleanup must wait rather than issue an illegal disable.
type failedGenerationReadRing struct {
	*fakeKeyRing
	readOnce  sync.Once
	cancel    context.CancelFunc
	updateErr error
}

func (f *failedGenerationReadRing) GetCryptoKeyVersion(ctx context.Context, req *kmspb.GetCryptoKeyVersionRequest) (*kmspb.CryptoKeyVersion, error) {
	first := false
	f.readOnce.Do(func() { first = true })
	if first {
		if f.cancel != nil {
			f.cancel()
			return nil, status.Error(codes.Canceled, "request cancelled") //nolint:wrapcheck // fake gRPC response
		}
		return nil, status.Error(codes.PermissionDenied, "generation read failed") //nolint:wrapcheck // fake gRPC response
	}
	return f.fakeKeyRing.GetCryptoKeyVersion(ctx, req)
}

func (f *failedGenerationReadRing) UpdateCryptoKeyVersion(ctx context.Context, req *kmspb.UpdateCryptoKeyVersionRequest) (*kmspb.CryptoKeyVersion, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return f.fakeKeyRing.UpdateCryptoKeyVersion(ctx, req)
}

func TestCreateSigningKey_ReconcilesFailedGeneration(t *testing.T) {
	t.Parallel()
	for _, cancelRequest := range []bool{false, true} {
		name := "read failure"
		if cancelRequest {
			name = "request cancellation"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			fake := &failedGenerationReadRing{fakeKeyRing: newFakeKeyRing()}
			if cancelRequest {
				fake.cancel = cancel
			}
			fake.pendingReads = 2
			client := newFakeProvisioningClient(t, fake, fake.fakeKeyRing)
			created, err := client.CreateSigningKey(ctx, CreateSigningKeyParams{KeyRing: testKeyRing, KeyID: "cleanup", Algorithm: jose.RS256})
			require.Nil(t, created)
			if cancelRequest {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.Equal(t, codes.PermissionDenied, status.Code(err))
			}
			require.NotContains(t, err.Error(), "requires reconciliation")
			require.Equal(t, []string{testKeyRing + "/cryptoKeys/cleanup/cryptoKeyVersions/1"}, fake.disabledVersions())
			require.Equal(t, 3, fake.versionReads)
		})
	}
}

func TestCreateSigningKey_PreservesResourceOnCleanupFailure(t *testing.T) {
	t.Parallel()
	fake := &failedGenerationReadRing{
		fakeKeyRing: newFakeKeyRing(),
		updateErr:   status.Error(codes.PermissionDenied, "disable denied"),
	}
	client := newFakeProvisioningClient(t, fake, fake.fakeKeyRing)
	_, err := client.CreateSigningKey(t.Context(), CreateSigningKeyParams{KeyRing: testKeyRing, KeyID: "cleanup", Algorithm: jose.RS256})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	require.ErrorContains(t, err, "generation read failed")
	require.ErrorContains(t, err, "disable denied")
	require.ErrorContains(t, err, "requires reconciliation")
	require.ErrorContains(t, err, testKeyRing+"/cryptoKeys/cleanup/cryptoKeyVersions/1")
}

func TestReconcileFailedKeyGeneration_BoundsPendingWait(t *testing.T) {
	t.Parallel()
	fake := newFakeKeyRing()
	fake.pendingReads = 1 << 30
	client := newFakeProvisioningClient(t, fake, fake)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	err := client.reconcileFailedKeyGeneration(ctx, testKeyRing+"/cryptoKeys/cleanup/cryptoKeyVersions/1")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Empty(t, fake.disabledVersions(), "pending versions must not be disabled")
}
