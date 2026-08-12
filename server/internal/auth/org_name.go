package auth

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/auth/orgslug"
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

// validOrgNameRegex allows alphanumeric characters, spaces, hyphens, and
// underscores.
//
// A literal space, not `\s`: Go's `\s` is `[\t\n\f\r ]`, and this runs on an
// unauthenticated endpoint, so the client's whitespace normalization is not a
// control. A name carrying a newline would otherwise reach the org record, the
// identity provider, and every log line that prints an org name.
var validOrgNameRegex = regexp.MustCompile(`^[a-zA-Z0-9 _-]+$`)

// maxOrgNameLength bounds the org name. validOrgNameRegex constrains the
// character set but not the length, and the signup path accepts this value on
// an unauthenticated endpoint that writes it to Redis.
const maxOrgNameLength = 100

// minOrgNameSlugChars is the floor on what survives slugification, mirroring
// MIN_ORG_NAME_SLUG_CHARS in the sign-up form. Two rather than one: a single
// alphanumeric such as "A-" still yields the one-character slug "a".
const minOrgNameSlugChars = 2

// shortOrgNameFormat matches the sign-up form's constraint and keeps the
// server's established "organization name" terminology; the form calls it a
// "Company name".
const shortOrgNameFormat = "organization name must contain at least %d letters or numbers"

// validateOrgName is the single org-name rule, shared by the authenticated
// register endpoint and the unauthenticated signup parameter on login.
func validateOrgName(name string) error {
	if strings.TrimSpace(name) == "" {
		return oops.E(oops.CodeInvalid, errors.New("org name is required"), "org name is required")
	}

	if len(name) > maxOrgNameLength {
		return oops.E(oops.CodeInvalid, errors.New("organization name is too long"), "organization name is too long")
	}

	if !validOrgNameRegex.MatchString(name) {
		return oops.E(oops.CodeInvalid, errors.New("organization name contains invalid characters"), "organization name contains invalid characters")
	}

	// Measured on Slugify's own output rather than restating its character rule,
	// so the two cannot drift. The character set above admits names made only of
	// punctuation ("-----", "___", "- _ -"), which slugify to nothing and would
	// otherwise create an org reachable at app.getgram.ai//.
	if len(orgslug.Slugify(name)) < minOrgNameSlugChars {
		return oops.E(
			oops.CodeInvalid,
			fmt.Errorf(shortOrgNameFormat, minOrgNameSlugChars),
			shortOrgNameFormat,
			minOrgNameSlugChars,
		)
	}

	return nil
}
