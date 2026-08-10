package gcpkms

import (
	"errors"
	"fmt"
	"regexp"
)

// ErrInvalidResourceName is returned when a stored resource name is not a
// fully-qualified GCP KMS crypto key version.
var ErrInvalidResourceName = errors.New("invalid gcp kms resource name")

// resourceNamePattern matches a crypto key VERSION resource name. The version
// suffix is required: asymmetric-sign keys have no primary version, so a
// version-less `.../cryptoKeys/<key>` path cannot be resolved to something
// signable. Rejecting it here turns a confusing runtime KMS error into a clear
// configuration message.
//
// Segments are permissive beyond excluding whitespace, which no GCP resource id
// may contain. Pinning each segment to its exact GCP grammar would risk
// rejecting valid names (numeric project ids, the "global" location) for no
// benefit, since KMS itself is the authority on whether the key exists.
var resourceNamePattern = regexp.MustCompile(
	`^projects/[^/\s]+/locations/[^/\s]+/keyRings/[^/\s]+/cryptoKeys/[^/\s]+/cryptoKeyVersions/[^/\s]+$`,
)

// ValidateKeyVersionName reports whether a resource name identifies a specific
// crypto key version.
func ValidateKeyVersionName(resourceName string) error {
	if !resourceNamePattern.MatchString(resourceName) {
		return fmt.Errorf(
			"%w: %q is not a projects/<p>/locations/<l>/keyRings/<r>/cryptoKeys/<k>/cryptoKeyVersions/<v> path",
			ErrInvalidResourceName, resourceName,
		)
	}

	return nil
}
