package gcpkms

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/asn1"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

type derSignature struct {
	R, S *big.Int
}

// A DER integer is encoded minimally, so a small R or S produces a shorter
// signature. JOSE requires both halves left-padded to the curve width; getting
// this wrong yields tokens that fail verification roughly one time in 256.
func TestECDSADERToJOSE_LeftPadsShortComponents(t *testing.T) {
	t.Parallel()

	der, err := asn1.Marshal(derSignature{R: big.NewInt(1), S: big.NewInt(2)})
	require.NoError(t, err)

	raw, err := ecdsaDERToJOSE(der, 32)
	require.NoError(t, err)
	require.Len(t, raw, 64, "both halves must occupy the full curve width")

	require.Equal(t, make([]byte, 31), raw[:31], "R must be left-padded with zeros")
	require.Equal(t, byte(1), raw[31])
	require.Equal(t, make([]byte, 31), raw[32:63], "S must be left-padded with zeros")
	require.Equal(t, byte(2), raw[63])
}

func TestECDSADERToJOSE_FullWidthComponentsRoundTrip(t *testing.T) {
	t.Parallel()

	r := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)) // 32 bytes of 0xff
	der, err := asn1.Marshal(derSignature{R: r, S: big.NewInt(7)})
	require.NoError(t, err)

	raw, err := ecdsaDERToJOSE(der, 32)
	require.NoError(t, err)
	require.Len(t, raw, 64)
	require.Equal(t, r.FillBytes(make([]byte, 32)), raw[:32])
}

// Real signatures from a P-256 key must always convert to exactly 64 bytes. The
// loop is what catches the short-component case, which appears only in a
// minority of signatures.
func TestECDSADERToJOSE_RealSignaturesAreAlwaysFullWidth(t *testing.T) {
	t.Parallel()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	digest := make([]byte, 32)
	for i := range 256 {
		digest[0] = byte(i)
		digest[1] = byte(i >> 8)

		der, err := ecdsa.SignASN1(rand.Reader, key, digest)
		require.NoError(t, err)

		raw, err := ecdsaDERToJOSE(der, 32)
		require.NoError(t, err)
		require.Len(t, raw, 64)
	}
}

func TestECDSADERToJOSE_RejectsTrailingBytes(t *testing.T) {
	t.Parallel()

	der, err := asn1.Marshal(derSignature{R: big.NewInt(1), S: big.NewInt(2)})
	require.NoError(t, err)

	_, err = ecdsaDERToJOSE(append(der, 0x00), 32)
	require.ErrorContains(t, err, "trailing bytes")
}

func TestECDSADERToJOSE_RejectsOversizedComponent(t *testing.T) {
	t.Parallel()

	tooBig := new(big.Int).Lsh(big.NewInt(1), 256) // 33 bytes, wider than P-256
	der, err := asn1.Marshal(derSignature{R: tooBig, S: big.NewInt(1)})
	require.NoError(t, err)

	_, err = ecdsaDERToJOSE(der, 32)
	require.ErrorContains(t, err, "exceeds the 32-byte curve width")
}

// R and S are integers in [1, n-1]; zero is never valid, and a negative value
// would make FillBytes panic.
func TestECDSADERToJOSE_RejectsZeroAndNegativeComponents(t *testing.T) {
	t.Parallel()

	for _, sig := range []derSignature{
		{R: big.NewInt(0), S: big.NewInt(1)},
		{R: big.NewInt(1), S: big.NewInt(0)},
		{R: big.NewInt(-1), S: big.NewInt(1)},
		{R: big.NewInt(1), S: big.NewInt(-1)},
	} {
		der, err := asn1.Marshal(sig)
		require.NoError(t, err)

		_, err = ecdsaDERToJOSE(der, 32)
		require.ErrorContains(t, err, "zero or negative", "R=%s S=%s", sig.R, sig.S)
	}
}

func TestECDSADERToJOSE_RejectsGarbage(t *testing.T) {
	t.Parallel()

	_, err := ecdsaDERToJOSE([]byte("not der"), 32)
	require.ErrorContains(t, err, "parse ecdsa der signature")
}

func TestCoordinateBytes(t *testing.T) {
	t.Parallel()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	require.Equal(t, 32, coordinateBytes(&key.PublicKey))

	key384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	require.NoError(t, err)
	require.Equal(t, 48, coordinateBytes(&key384.PublicKey))
}
