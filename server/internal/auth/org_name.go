package auth

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
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

const orgNameSuffixAlphabet = "abcdefghijkmnpqrstuvwxyz23456789" // base32 minus look-alikes (l/1, o/0)

// generateLegibleOrgName returns a slug-friendly random org name like
// "Swift Otter h7n2". The 4-char base32 suffix gives ~1M unique slots per
// adjective+noun pair, keeping collision risk negligible against the
// Speakeasy register endpoint without sacrificing legibility.
func generateLegibleOrgName() string {
	adj := orgNameAdjectives[randIndex(len(orgNameAdjectives))]
	noun := orgNameNouns[randIndex(len(orgNameNouns))]
	suffix := make([]byte, 4)
	for i := range suffix {
		suffix[i] = orgNameSuffixAlphabet[randIndex(len(orgNameSuffixAlphabet))]
	}
	return fmt.Sprintf("%s %s %s", adj, noun, suffix)
}

func randIndex(n int) int {
	if n <= 0 {
		return 0
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Errorf("crypto/rand failed: %w", err))
	}
	return int(binary.BigEndian.Uint64(b[:]) % uint64(n)) //nolint:gosec // bounded by `n`, caller-validated
}
