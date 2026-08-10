package auth

import (
	"errors"
	"strings"
	"unicode"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

// maxSignupEmailLength is the RFC 5321 limit on a forward path. The bound is
// here to keep an unauthenticated query parameter from being unbounded, not to
// be tight — a shorter cap would reject deliverable addresses on the first
// screen of the sign-up form.
const maxSignupEmailLength = 254

// validateSignupEmail checks the email the sign-up page collected before it
// becomes a login_hint query parameter on the identity-provider redirect.
//
// Deliberately not an RFC 5322 grammar. The identity provider is authoritative
// and the user can overwrite this value on the very next screen, so an
// over-strict rule here can only reject addresses WorkOS would have accepted.
// What it does have to catch is anything that would corrupt the URL the value
// is spliced into — CRLF above all.
func validateSignupEmail(email string) error {
	if email == "" {
		return oops.E(oops.CodeInvalid, errors.New("email is required"), "email is required")
	}

	if len(email) > maxSignupEmailLength {
		return oops.E(oops.CodeInvalid, errors.New("email is too long"), "email is too long")
	}

	for _, r := range email {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return oops.E(oops.CodeInvalid, errors.New("email contains invalid characters"), "email contains invalid characters")
		}
	}

	local, domain, found := strings.Cut(email, "@")
	if !found || local == "" || domain == "" || strings.Contains(domain, "@") {
		return oops.E(oops.CodeInvalid, errors.New("email is not a valid address"), "email is not a valid address")
	}

	return nil
}
