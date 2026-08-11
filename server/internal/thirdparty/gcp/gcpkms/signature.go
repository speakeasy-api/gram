package gcpkms

import (
	"crypto/ecdsa"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
)

// ecdsaDERToJOSE converts the ASN.1 DER ECDSA signature GCP KMS returns into the
// fixed-width R || S concatenation JOSE requires (RFC 7515 section 3.4).
//
// The left-padding is what makes this correct rather than a plain
// concatenation. DER encodes integers minimally, so a small R or S occupies
// fewer bytes than the curve width; joining those directly yields a short
// signature that every verifier rejects. Only about one signature in 256 has a
// short component, so an implementation that skipped the padding would fail
// intermittently and pass casual testing. FillBytes pads explicitly below, and
// TestECDSADERToJOSE_LeftPadsShortComponents pins the behaviour.
func ecdsaDERToJOSE(der []byte, coordinateBytes int) ([]byte, error) {
	var parsed struct {
		R, S *big.Int
	}

	rest, err := asn1.Unmarshal(der, &parsed)
	if err != nil {
		return nil, fmt.Errorf("parse ecdsa der signature: %w", err)
	}
	if len(rest) > 0 {
		return nil, fmt.Errorf("parse ecdsa der signature: %d trailing bytes", len(rest))
	}

	// A DER ECDSA signature encodes two integers in [1, n-1]. Zero is never
	// valid, and a negative value would make FillBytes panic below.
	if parsed.R.Sign() <= 0 || parsed.S.Sign() <= 0 {
		return nil, errors.New("ecdsa signature component is zero or negative")
	}

	maxBits := coordinateBytes * 8
	if parsed.R.BitLen() > maxBits || parsed.S.BitLen() > maxBits {
		return nil, fmt.Errorf("ecdsa signature component exceeds the %d-byte curve width", coordinateBytes)
	}

	out := make([]byte, 2*coordinateBytes)
	parsed.R.FillBytes(out[:coordinateBytes])
	parsed.S.FillBytes(out[coordinateBytes:])

	return out, nil
}

// coordinateBytes is the byte width of a single ECDSA signature component for a
// public key's curve.
func coordinateBytes(pub *ecdsa.PublicKey) int {
	return (pub.Curve.Params().BitSize + 7) / 8
}
