package internalsessionissuers

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
)

// Deployment binds issuer authority and key operations to this deployment.
// Database copies must not confer authority over the source deployment's keys.
type Deployment struct {
	// ServerURL is the configured platform origin, never a request host.
	ServerURL *url.URL

	// AllowLoopbackIssuer admits HTTP loopback for local development only.
	AllowLoopbackIssuer bool

	// KeyRing is the fully qualified KMS ring this deployment owns.
	KeyRing string
}

// CheckIssuer rejects authority copied from another deployment.
func (d Deployment) CheckIssuer(organizationID, issuer string) error {
	expected, err := URL(d.ServerURL, organizationID, d.AllowLoopbackIssuer)
	if err != nil || expected.String() != issuer {
		return fmt.Errorf("%w: issuer does not belong to this deployment", ErrMisconfigured)
	}
	return nil
}

// KMSKeyPrefix starts the id of every issuer KMS key, separating them from
// other keys on the shared identity provider ring.
const KMSKeyPrefix = "emaissuer"

// CheckKey rejects KMS versions outside the configured ring or without the
// issuer key prefix.
func (d Deployment) CheckKey(resource string) error {
	if err := gcpkms.ValidateKeyRingName(d.KeyRing); err != nil {
		return fmt.Errorf("%w: deployment key ring is unavailable", ErrMisconfigured)
	}
	if err := gcpkms.ValidateKeyVersionName(resource); err != nil || !strings.HasPrefix(resource, d.KeyRing+"/cryptoKeys/"+KMSKeyPrefix+"-") {
		return fmt.Errorf("%w: issuer key does not belong to this deployment", ErrMisconfigured)
	}
	return nil
}
