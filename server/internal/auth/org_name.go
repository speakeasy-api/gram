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

// maxOrgNameLength is measured in Unicode code points, not bytes.
const maxOrgNameLength = 100

// minOrgNameLettersOrNumbers keeps punctuation-only values from becoming
// organization names.
const minOrgNameLettersOrNumbers = 2

const shortOrgNameFormat = "organization name must contain at least %d letters or numbers"

// Zero-width joiners are allowed because they are required by several scripts.
const (
	zeroWidthNonJoiner = '\u200C'
	zeroWidthJoiner    = '\u200D'
)

// validateOrgName returns a whitespace-normalized display name when the input
// contains only graphic characters and permitted joiners.
func validateOrgName(name string) (string, error) {
	invalidChars := func() error {
		return oops.E(oops.CodeInvalid, errors.New("organization name contains invalid characters"), "organization name contains invalid characters")
	}

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

	lettersOrNumbers := 0
	for _, r := range normalized {
		if !unicode.IsGraphic(r) && r != zeroWidthJoiner && r != zeroWidthNonJoiner {
			return "", invalidChars()
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			lettersOrNumbers++
		}
	}

	if lettersOrNumbers < minOrgNameLettersOrNumbers {
		return "", oops.E(
			oops.CodeInvalid,
			fmt.Errorf(shortOrgNameFormat, minOrgNameLettersOrNumbers),
			shortOrgNameFormat,
			minOrgNameLettersOrNumbers,
		)
	}

	return normalized, nil
}

// normalizeOrgNameSpaces converts Unicode space separators to ASCII spaces,
// collapses runs, and trims the ends. Other whitespace remains for validation.
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
