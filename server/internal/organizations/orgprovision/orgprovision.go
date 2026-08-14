// Package orgprovision holds the parts of creating an organization that every
// caller must do identically: validating the display name, and creating the
// WorkOS organization so that its Gram organization ID is derivable from its
// WorkOS ID.
//
// Callers own their own database transaction. Self-serve signup attaches the
// first user, an authz admin grant and possibly a trial; a platform admin
// creating an empty organization attaches none of those. Those transactions are
// deliberately not shared, because the writes genuinely differ. What must not
// differ is everything in this package.
package orgprovision

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/speakeasy-api/gram/server/internal/oops"
	orgid "github.com/speakeasy-api/gram/server/internal/organizations/id"
)

// WorkOSOrganizationCreator is the WorkOS surface CreateInWorkOS needs. It
// takes two methods rather than one because the Gram organization ID is derived
// from the WorkOS ID that only the create call returns, so external_id can only
// be set by a second call.
type WorkOSOrganizationCreator interface {
	// CreateOrganization creates a WorkOS organization and returns its WorkOS
	// ID. The second argument sets external_id and the idempotency key.
	CreateOrganization(ctx context.Context, name, gramOrgID string) (string, error)

	// UpdateOrganizationExternalID sets external_id on an existing WorkOS
	// organization.
	UpdateOrganizationExternalID(ctx context.Context, workosOrgID, externalID string) error
}

// CreatedOrganization is an organization that exists in WorkOS and has no Gram
// row yet.
type CreatedOrganization struct {
	// WorkOSOrganizationID is the ID WorkOS assigned.
	WorkOSOrganizationID string

	// GramOrganizationID is derived from WorkOSOrganizationID, so it is the
	// same value whoever computes it.
	GramOrganizationID string
}

// CreateInWorkOS creates a WorkOS organization for name and returns it paired
// with the Gram organization ID derived from the ID WorkOS assigned.
//
// Deriving the Gram ID rather than minting one is what lets two writers reach
// the same row. The WorkOS organization webhook derives the same ID for an
// organization it has not seen before (see
// background/activities.ProcessWorkOSOrganizationEvents), so whichever of the
// two writes lands first, the second one collides on the primary key and
// updates instead of inserting a duplicate. A freshly generated UUID would
// instead produce two rows for one WorkOS organization, and the second insert
// would fail on the unique index over workos_id.
//
// external_id is set in a second call because the value depends on the ID the
// create returns. A caller that sees an error from this function must assume
// nothing about whether a WorkOS organization was left behind.
func CreateInWorkOS(ctx context.Context, client WorkOSOrganizationCreator, name string) (CreatedOrganization, error) {
	var empty CreatedOrganization

	workosOrgID, err := client.CreateOrganization(ctx, name, "")
	if err != nil {
		return empty, fmt.Errorf("create WorkOS organization: %w", err)
	}

	gramOrgID := orgid.FromWorkOSID(workosOrgID)

	if err := client.UpdateOrganizationExternalID(ctx, workosOrgID, gramOrgID); err != nil {
		return empty, fmt.Errorf("set external_id on WorkOS organization: %w", err)
	}

	return CreatedOrganization{
		WorkOSOrganizationID: workosOrgID,
		GramOrganizationID:   gramOrgID,
	}, nil
}

// ErrUnavailable is returned by Unavailable. Callers translate it into whatever
// their transport reports for "this deployment cannot do that".
var ErrUnavailable = errors.New("WorkOS is not configured on this server")

// Unavailable is the WorkOSOrganizationCreator for a deployment with no WorkOS
// configuration. Every call fails with ErrUnavailable, because an organization
// the identity provider does not know about cannot be logged into. Failing is
// more honest than creating a Gram-only row that looks like a success.
type Unavailable struct{}

// CreateOrganization always fails with ErrUnavailable.
func (Unavailable) CreateOrganization(context.Context, string, string) (string, error) {
	return "", ErrUnavailable
}

// UpdateOrganizationExternalID always fails with ErrUnavailable.
func (Unavailable) UpdateOrganizationExternalID(context.Context, string, string) error {
	return ErrUnavailable
}

// MaxNameLength is the longest accepted organization name, measured in Unicode
// code points rather than bytes.
const MaxNameLength = 100

// MaxRawNameBytes bounds the input before any of it is copied. A name at
// MaxNameLength occupies at most four bytes per code point, so this leaves
// ample room for surrounding whitespace while keeping an unauthenticated caller
// from making the server allocate a normalized copy of an arbitrary payload.
const MaxRawNameBytes = 4 * MaxNameLength * 10

// MinNameLettersOrNumbers keeps punctuation-only values from becoming
// organization names.
const MinNameLettersOrNumbers = 2

// ShortNameFormat is the message template for a name carrying too few letters
// or numbers. It takes MinNameLettersOrNumbers.
const ShortNameFormat = "organization name must contain at least %d letters or numbers"

// Zero-width joiners are allowed because they are required by several scripts.
const (
	zeroWidthNonJoiner = '\u200C'
	zeroWidthJoiner    = '\u200D'
)

// ValidateName returns a whitespace-normalized display name when the input
// contains only graphic characters and permitted joiners. Every path that names
// an organization runs this, so a name accepted by one is accepted by all.
func ValidateName(name string) (string, error) {
	invalidChars := func() error {
		return oops.E(oops.CodeInvalid, errors.New("organization name contains invalid characters"), "organization name contains invalid characters")
	}

	if len(name) > MaxRawNameBytes {
		return "", oops.E(oops.CodeInvalid, errors.New("organization name is too long"), "organization name is too long")
	}

	if !utf8.ValidString(name) {
		return "", invalidChars()
	}

	normalized := normalizeSpaces(name)
	if normalized == "" {
		return "", oops.E(oops.CodeInvalid, errors.New("org name is required"), "org name is required")
	}

	if utf8.RuneCountInString(normalized) > MaxNameLength {
		return "", oops.E(oops.CodeInvalid, errors.New("organization name is too long"), "organization name is too long")
	}

	lettersOrNumbers := 0
	for _, r := range normalized {
		if !unicode.IsGraphic(r) && r != zeroWidthJoiner && r != zeroWidthNonJoiner {
			return "", invalidChars()
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			lettersOrNumbers++
		}
	}

	if lettersOrNumbers < MinNameLettersOrNumbers {
		return "", oops.E(
			oops.CodeInvalid,
			fmt.Errorf(ShortNameFormat, MinNameLettersOrNumbers),
			ShortNameFormat,
			MinNameLettersOrNumbers,
		)
	}

	return normalized, nil
}

// normalizeSpaces converts Unicode space separators to ASCII spaces, collapses
// runs, and trims the ends. Other whitespace remains for validation.
func normalizeSpaces(name string) string {
	var b strings.Builder
	b.Grow(len(name))

	pendingSpace := false
	for _, r := range name {
		if unicode.Is(unicode.Zs, r) {
			pendingSpace = b.Len() > 0
			continue
		}
		if pendingSpace {
			b.WriteRune(' ')
			pendingSpace = false
		}
		b.WriteRune(r)
	}

	return b.String()
}
