package orgslug

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
	"golang.org/x/text/unicode/norm"

	"github.com/speakeasy-api/gram/server/internal/conv"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

var slugifyRe = regexp.MustCompile(`[^a-z0-9]+`)

// apostropheRe drops apostrophes rather than letting them become separators, so
// "Bob's Bakery" slugifies to "bobs-bakery" instead of "bob-s-bakery". The
// curly form is what word processors and pasted text carry.
var apostropheRe = regexp.MustCompile("['`\u2018\u2019]")

// latinLigatures spells out the lowercase Latin letters that have no
// decomposition for foldToASCII to strip, and so would otherwise become
// separators: "Straße" is a better slug as "strasse" than as "stra-e".
var latinLigatures = strings.NewReplacer(
	"ß", "ss",
	"æ", "ae",
	"œ", "oe",
	"ø", "o",
	"đ", "d",
	"ð", "d",
	"þ", "th",
	"ł", "l",
	"ı", "i",
)

// minChars is the floor on a usable slug. One character is enough to route but
// not enough to identify, and zero would put an organization at a URL with an
// empty path segment.
const minChars = 2

type Lookup interface {
	GetOrganizationMetadataBySlug(ctx context.Context, slug string) (orgRepo.OrganizationMetadatum, error)
}

// Slugify reduces an organization name to the `[a-z0-9-]` form used in URLs. It
// returns "" for a name with nothing to build a slug out of, which includes
// every name written entirely in a non-Latin script; callers that need a slug
// they can store should use Base instead.
func Slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = latinLigatures.Replace(s)
	s = foldToASCII(s)
	s = apostropheRe.ReplaceAllString(s, "")
	s = slugifyRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

// foldToASCII drops the accents off Latin letters, so "Nestlé" slugifies to
// "nestle" rather than "nestl" and "Müller" to "muller" rather than "m-ller".
// Decomposing first splits an accented letter into its base letter and a
// combining mark, and only the mark is dropped.
func foldToASCII(s string) string {
	decomposed := norm.NFD.String(s)

	var b strings.Builder
	b.Grow(len(decomposed))
	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}

	return b.String()
}

// Base returns a slug base for name, falling back to a generated one when the
// name yields too little to build a slug from. Organization names are validated
// for meaning, not for URL-safety, so a perfectly good name written in a
// non-Latin script can slugify to nothing at all.
func Base(name string) (string, error) {
	if s := Slugify(name); len(s) >= minChars {
		return s, nil
	}

	suffix, err := conv.GenerateRandomSlug(8)
	if err != nil {
		return "", fmt.Errorf("generate fallback slug base: %w", err)
	}
	return "org-" + suffix, nil
}

const maxSlugAttempts = 10

func FindUnique(ctx context.Context, lookup Lookup, base string) (string, error) {
	candidate := base
	for range maxSlugAttempts {
		_, err := lookup.GetOrganizationMetadataBySlug(ctx, candidate)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return candidate, nil
		case err != nil:
			return "", fmt.Errorf("get organization metadata by slug %q: %w", candidate, err)
		}

		suffix, err := randomHexSuffix(4)
		if err != nil {
			return "", fmt.Errorf("generate slug suffix: %w", err)
		}
		candidate = base + "-" + suffix
	}
	return "", errors.New("unable to find unique slug after max attempts")
}

func randomHexSuffix(n int) (string, error) {
	b := make([]byte, (n+1)/2)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto/rand: %w", err)
	}
	return hex.EncodeToString(b)[:n], nil
}
