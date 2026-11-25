package mcp_test

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// deviceIssuerURL is a Speakeasy device issuer in the test organization.
const deviceIssuerURL = "https://gram.example.com/workload/devices/test-org"

// enrolledDevice is a device key registered under the device issuer.
type enrolledDevice struct {
	subject    string
	key        *ecdsa.PrivateKey
	thumbprint string
}

// seedDeviceIssuer writes the organization's device issuer, whose assertions
// verify against each subject's registered key.
func seedDeviceIssuer(t *testing.T, f liveWorkloadFixture) uuid.UUID {
	t.Helper()
	id, err := testrepo.New(f.conn).CreateWorkloadDeviceIssuerFixture(t.Context(), testrepo.CreateWorkloadDeviceIssuerFixtureParams{
		OrganizationID: f.organizationID,
		Name:           "Speakeasy devices",
		Issuer:         deviceIssuerURL,
		JwksUri:        deviceIssuerURL + "/jwks.json",
	})
	require.NoError(t, err)
	return id
}

// enrollDevice registers a fresh key for a fresh device subject and admits it.
func enrollDevice(t *testing.T, f liveWorkloadFixture, issuerID uuid.UUID) enrolledDevice {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	public := jose.JSONWebKey{Key: key.Public(), KeyID: "", Algorithm: "", Use: ""}
	sum, err := public.Thumbprint(crypto.SHA256)
	require.NoError(t, err)
	thumbprint := base64.RawURLEncoding.EncodeToString(sum)
	public.KeyID = thumbprint
	public.Algorithm = string(jose.ES256)
	public.Use = "sig"
	document, err := public.MarshalJSON()
	require.NoError(t, err)

	device := enrolledDevice{subject: "device:" + uuid.NewString(), key: key, thumbprint: thumbprint}
	q := testrepo.New(f.conn)
	require.NoError(t, q.CreateWorkloadDeviceKeyFixture(t.Context(), testrepo.CreateWorkloadDeviceKeyFixtureParams{
		OrganizationID:   f.organizationID,
		WorkloadIssuerID: issuerID,
		Subject:          device.subject,
		PublicJwk:        document,
		Thumbprint:       thumbprint,
	}))
	require.NoError(t, q.CreateWorkloadIdentityAdmissionFixture(t.Context(), testrepo.CreateWorkloadIdentityAdmissionFixtureParams{
		OrganizationID:   f.organizationID,
		ProjectID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		WorkloadIssuerID: issuerID,
		Subject:          device.subject,
	}))
	return device
}

// signDeviceAssertion signs an assertion for subject with key, naming kid.
func signDeviceAssertion(t *testing.T, key *ecdsa.PrivateKey, kid, subject string) string {
	t.Helper()
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), kid),
	)
	require.NoError(t, err)
	now := time.Now()
	raw, err := jwt.Signed(signer).Claims(jwt.Claims{
		Issuer:   deviceIssuerURL,
		Subject:  subject,
		Audience: jwt.Audience{liveWorkloadAudience + "/token"},
		ID:       uuid.NewString(),
		IssuedAt: jwt.NewNumericDate(now),
		Expiry:   jwt.NewNumericDate(now.Add(5 * time.Minute)),
	}).Serialize()
	require.NoError(t, err)
	return raw
}

func (f liveWorkloadFixture) presentRaw(t *testing.T, raw string) error {
	t.Helper()
	if err := mcp.AdmitWorkloadAssertion(t.Context(), f.conn, f.verifier, f.endpoint(f.organizationID, f.projectID), []string{
		liveWorkloadAudience,
		liveWorkloadAudience + "/token",
	}, raw); err != nil {
		return fmt.Errorf("admit device assertion: %w", err)
	}
	return nil
}

// A registered device's assertion is admitted, verified against its own key
// with no key set fetched.
func TestDeviceAssertion_RegisteredDeviceIsAdmittedWithoutAFetch(t *testing.T) {
	t.Parallel()
	f := newLiveWorkloadFixture(t)
	device := enrollDevice(t, f, seedDeviceIssuer(t, f))

	require.NoError(t, f.presentRaw(t, signDeviceAssertion(t, device.key, device.thumbprint, device.subject)))
	require.Equal(t, f.connectionsAtSetup, f.issuer.Connections(), "a device assertion must not fetch any key set")
}

// One device's key cannot sign as another device, however the assertion
// names its key.
func TestDeviceAssertion_OneDeviceCannotSignAsAnother(t *testing.T) {
	t.Parallel()
	f := newLiveWorkloadFixture(t)
	issuerID := seedDeviceIssuer(t, f)
	a := enrollDevice(t, f, issuerID)
	b := enrollDevice(t, f, issuerID)

	// A's key and kid, claiming B.
	require.ErrorContains(t, f.presentRaw(t, signDeviceAssertion(t, a.key, a.thumbprint, b.subject)), "device_key_id_mismatch")
	// A's key claiming B, naming B's kid.
	require.ErrorContains(t, f.presentRaw(t, signDeviceAssertion(t, a.key, b.thumbprint, b.subject)), "assertion_signature_invalid")
	// B is still fine.
	require.NoError(t, f.presentRaw(t, signDeviceAssertion(t, b.key, b.thumbprint, b.subject)))
}

// An assertion must name the registered key; a missing kid is refused.
func TestDeviceAssertion_KidMustNameTheRegisteredKey(t *testing.T) {
	t.Parallel()
	f := newLiveWorkloadFixture(t)
	device := enrollDevice(t, f, seedDeviceIssuer(t, f))

	require.ErrorContains(t, f.presentRaw(t, signDeviceAssertion(t, device.key, "", device.subject)), "device_key_id_mismatch")
	require.ErrorContains(t, f.presentRaw(t, signDeviceAssertion(t, device.key, "some-other-kid", device.subject)), "device_key_id_mismatch")
}

// A withdrawn device's key is gone, so it is refused even though it still
// holds a valid private key.
func TestDeviceAssertion_RetiredKeyIsRefused(t *testing.T) {
	t.Parallel()
	f := newLiveWorkloadFixture(t)
	issuerID := seedDeviceIssuer(t, f)
	device := enrollDevice(t, f, issuerID)

	withdrawn, err := testrepo.New(f.conn).SoftDeleteWorkloadDeviceKeyFixture(t.Context(), testrepo.SoftDeleteWorkloadDeviceKeyFixtureParams{OrganizationID: f.organizationID, WorkloadIssuerID: issuerID, Subject: device.subject})
	require.NoError(t, err)
	require.EqualValues(t, 1, withdrawn)

	require.ErrorContains(t, f.presentRaw(t, signDeviceAssertion(t, device.key, device.thumbprint, device.subject)), "device_key_not_registered")
}

// An issuer whose key_source is not a known value fails closed instead of
// falling back to its remote key set.
func TestDeviceAssertion_UnknownKeySourceFailsClosed(t *testing.T) {
	t.Parallel()
	f := newLiveWorkloadFixture(t)
	issuerID := seedDeviceIssuer(t, f)
	device := enrollDevice(t, f, issuerID)

	updated, err := testrepo.New(f.conn).SetWorkloadIssuerKeySourceFixture(t.Context(), testrepo.SetWorkloadIssuerKeySourceFixtureParams{ID: issuerID, OrganizationID: f.organizationID, KeySource: conv.ToPGText("device_keyz")})
	require.NoError(t, err)
	require.EqualValues(t, 1, updated)

	require.ErrorContains(t, f.presentRaw(t, signDeviceAssertion(t, device.key, device.thumbprint, device.subject)), "issuer_key_source_unknown")
}

// A registered key whose subject is not admitted is refused at admission.
func TestDeviceAssertion_UnadmittedDeviceIsRefused(t *testing.T) {
	t.Parallel()
	f := newLiveWorkloadFixture(t)
	issuerID := seedDeviceIssuer(t, f)
	device := enrollDevice(t, f, issuerID)

	withdrawn, err := testrepo.New(f.conn).SoftDeleteWorkloadAdmissionFixture(t.Context(), testrepo.SoftDeleteWorkloadAdmissionFixtureParams{OrganizationID: f.organizationID, WorkloadIssuerID: issuerID, Subject: device.subject})
	require.NoError(t, err)
	require.EqualValues(t, 1, withdrawn)

	require.ErrorContains(t, f.presentRaw(t, signDeviceAssertion(t, device.key, device.thumbprint, device.subject)), "subject_not_admitted")
}
