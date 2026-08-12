package auth

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

var orgNameAdjectives = []string{
	"Agile", "Amber", "Ample", "Ardent", "Arid", "Astral", "Autumn", "Azure",
	"Balmy", "Beaming", "Bold", "Boundless", "Brave", "Breezy", "Bright", "Brisk",
	"Calm", "Candid", "Cheerful", "Chill", "Classic", "Clever", "Cosmic",
	"Cozy", "Crafty", "Crisp", "Crystal", "Curious", "Dapper", "Daring",
	"Dawning", "Dazzling", "Deft", "Diligent", "Distant", "Eager", "Earnest",
	"Electric", "Elegant", "Ember", "Endless", "Epic", "Eternal", "Fabled",
	"Fancy", "Faraway", "Fearless", "Feisty", "Fierce", "Flora", "Flying",
	"Forest", "Fortune", "Friendly", "Frosty", "Gallant", "Gentle", "Gilded",
	"Glacial", "Gleaming", "Glowing", "Golden", "Grand", "Graceful", "Happy",
	"Hardy", "Harmonic", "Hazel", "Hearty", "Heroic", "Hidden", "Humble",
	"Indigo", "Inky", "Iron", "Ivory", "Jade", "Jaunty", "Jolly", "Joyful",
	"Keen", "Kind", "Lively", "Lucid", "Lucky", "Lunar", "Marble", "Marigold",
	"Maverick", "Meadow", "Mellow", "Merry", "Mighty", "Misty", "Modern",
	"Mystic", "Nebula", "Neon", "Nimble", "Noble", "Northern", "Oasis",
	"Onyx", "Open", "Orbit", "Pacific", "Patient", "Plucky", "Polished",
	"Pristine", "Prudent", "Quick", "Quiet", "Radiant", "Rapid", "Roaring",
	"Royal", "Rugged", "Saffron", "Sapphire", "Scarlet", "Serene", "Silver",
	"Sleek", "Smooth", "Snowy", "Solar", "Sparkly", "Spry", "Stellar",
	"Stormy", "Subtle", "Sunny", "Swift", "Tidy", "Tranquil", "Trusty",
	"Twilight", "Valiant", "Velvet", "Verdant", "Vibrant", "Vivid",
	"Wandering", "Warm", "Whimsical", "Winsome", "Witty", "Zealous", "Zesty",
}

var orgNameNouns = []string{
	"Albatross", "Antelope", "Aurora", "Badger", "Beacon", "Beaver", "Bison",
	"Boulder", "Cactus", "Canyon", "Cardinal", "Cascade", "Cedar", "Cheetah",
	"Cobra", "Comet", "Condor", "Coral", "Cosmos", "Coyote", "Cricket",
	"Crystal", "Cypress", "Dahlia", "Daisy", "Dolphin", "Dragon", "Dune",
	"Eagle", "Echo", "Eclipse", "Elder", "Elk", "Ember", "Falcon", "Fawn",
	"Fern", "Ferret", "Finch", "Forge", "Forest", "Fox", "Fjord", "Galaxy",
	"Garnet", "Gazelle", "Geyser", "Giraffe", "Glacier", "Goose", "Granite",
	"Griffin", "Grove", "Harbor", "Hawk", "Heron", "Hippo", "Horizon", "Ibex",
	"Iceberg", "Iguana", "Iris", "Jackal", "Jaguar", "Jasmine", "Jay",
	"Juniper", "Koala", "Lagoon", "Lark", "Lemur", "Leopard", "Lighthouse",
	"Lily", "Lion", "Lotus", "Lynx", "Magpie", "Maple", "Marble", "Marlin",
	"Marmot", "Meadow", "Meerkat", "Meteor", "Mongoose", "Moose", "Mountain",
	"Narwhal", "Nebula", "Ocelot", "Onyx", "Opal", "Orca", "Orchid",
	"Otter", "Owl", "Panda", "Panther", "Pearl", "Pelican", "Penguin",
	"Phoenix", "Pine", "Plover", "Prairie", "Puma", "Quartz", "Quokka",
	"Rapids", "Raven", "Reef", "Ridge", "River", "Robin", "Sable", "Sage",
	"Salmon", "Sapphire", "Savanna", "Seal", "Sequoia", "Sparrow", "Spruce",
	"Stoat", "Stream", "Sunrise", "Tiger", "Topaz", "Tundra", "Valley",
	"Walrus", "Willow", "Wolf", "Wren", "Yarrow", "Zephyr",
}

// generateLegibleOrgName returns a slug-friendly random org name like
// "Swift Otter h7n2". The suffix gives ~1M+ unique slots per adjective+noun
// pair so collisions against the Speakeasy register endpoint stay negligible.
func generateLegibleOrgName() string {
	adjIdx, err := rand.Int(rand.Reader, big.NewInt(int64(len(orgNameAdjectives))))
	if err != nil {
		panic(fmt.Errorf("crypto/rand failed: %w", err))
	}
	nounIdx, err := rand.Int(rand.Reader, big.NewInt(int64(len(orgNameNouns))))
	if err != nil {
		panic(fmt.Errorf("crypto/rand failed: %w", err))
	}
	suffix, err := conv.GenerateRandomSlug(4)
	if err != nil {
		panic(fmt.Errorf("generate random slug: %w", err))
	}
	adj := orgNameAdjectives[adjIdx.Int64()]
	noun := orgNameNouns[nounIdx.Int64()]
	return fmt.Sprintf("%s %s %s", adj, noun, suffix)
}

// maxOrgNameLength bounds the org name in runes, not bytes: a name in a
// non-Latin script spends two to four bytes per character, and a byte cap would
// give it a third of the room a Latin name gets. The signup path accepts this
// value on an unauthenticated endpoint that writes it to Redis.
const maxOrgNameLength = 100

// minOrgNameLetterOrDigit is the floor on letters and digits in the name,
// mirroring MIN_ORG_NAME_LETTERS_OR_DIGITS in the sign-up form. It keeps out
// names made only of punctuation ("-----", "___", "- _ -"), which carry no
// meaning and slugify to nothing. Two rather than one so a name is a name
// rather than an initial.
const minOrgNameLetterOrDigit = 2

// shortOrgNameFormat matches the sign-up form's constraint and keeps the
// server's established "organization name" terminology; the form calls it a
// "Company name".
const shortOrgNameFormat = "organization name must contain at least %d letters or numbers"

// Zero-width joiner and non-joiner. Both are Cf, the category this rejects
// wholesale for carrying bidi overrides, but Indic, Arabic and Persian
// orthography needs them to render correct glyphs — an allowlist that drops
// them mangles names in the very scripts the rest of this rule admits.
const (
	zeroWidthNonJoiner = '\u200C'
	zeroWidthJoiner    = '\u200D'
)

// validateOrgName is the single org-name rule, shared by the authenticated
// register endpoint and the unauthenticated signup parameter on login. It
// returns the name to store: whitespace-normalized, so a pasted non-breaking
// space or a run of spaces does not reach the org record and the identity
// provider verbatim.
//
// The rule is a denylist, and deliberately a permissive one. A display name
// never has to be URL-safe — orgslug derives and sanitizes the slug separately —
// so it only has to be safe to render and to log. "Acme, Inc.", "Bob's Bakery",
// "Café Zoë" and every name written in a non-Latin script are names, not
// attacks, and a company that has one should not have to transliterate it to
// sign up.
func validateOrgName(name string) (string, error) {
	invalidChars := func() error {
		return oops.E(oops.CodeInvalid, errors.New("organization name contains invalid characters"), "organization name contains invalid characters")
	}

	// Invalid UTF-8 would otherwise survive as replacement characters in the
	// org record and in every log line that prints the name.
	if !utf8.ValidString(name) {
		return "", invalidChars()
	}

	normalized := normalizeOrgNameSpaces(name)
	if normalized == "" {
		return "", oops.E(oops.CodeInvalid, errors.New("org name is required"), "org name is required")
	}

	if utf8.RuneCountInString(normalized) > maxOrgNameLength {
		return "", oops.E(oops.CodeInvalid, errors.New("organization name is too long"), "organization name is too long")
	}

	letterOrDigit := 0
	for _, r := range normalized {
		// IsGraphic covers letters, marks, numbers, punctuation, symbols and
		// space separators, and so admits every script while rejecting control
		// characters, bidi overrides and other formatting codes, private-use
		// and surrogate code points, and unassigned ones.
		if !unicode.IsGraphic(r) && r != zeroWidthJoiner && r != zeroWidthNonJoiner {
			return "", invalidChars()
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			letterOrDigit++
		}
	}

	if letterOrDigit < minOrgNameLetterOrDigit {
		return "", oops.E(
			oops.CodeInvalid,
			fmt.Errorf(shortOrgNameFormat, minOrgNameLetterOrDigit),
			shortOrgNameFormat,
			minOrgNameLetterOrDigit,
		)
	}

	return normalized, nil
}

// normalizeOrgNameSpaces maps every Unicode space separator to a plain space,
// collapses runs of them, and trims the ends. Pasted names routinely carry a
// non-breaking or ideographic space, and this runs on an unauthenticated
// endpoint, so the client's own normalization is not a control.
//
// Tabs, newlines and other control characters are deliberately left alone here
// for validateOrgName to reject rather than quietly absorb.
func normalizeOrgNameSpaces(name string) string {
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
