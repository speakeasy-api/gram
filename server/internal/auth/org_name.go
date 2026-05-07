package auth

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
)

var orgNameAdjectives = []string{
	"Brave", "Bright", "Calm", "Clever", "Cosmic", "Crisp", "Curious",
	"Dapper", "Daring", "Dazzling", "Eager", "Earnest", "Electric", "Fancy",
	"Fearless", "Fierce", "Flying", "Friendly", "Gentle", "Glowing", "Grand",
	"Happy", "Hardy", "Hidden", "Humble", "Jolly", "Keen", "Kind",
	"Lively", "Lucky", "Merry", "Mighty", "Mystic", "Nimble", "Noble",
	"Plucky", "Polished", "Quick", "Quiet", "Radiant", "Roaring", "Royal",
	"Rugged", "Silver", "Sleek", "Smooth", "Snowy", "Sparkly", "Spry",
	"Stellar", "Stormy", "Sunny", "Swift", "Tidy", "Trusty", "Vivid",
	"Wandering", "Warm", "Witty", "Zesty",
}

var orgNameNouns = []string{
	"Albatross", "Antelope", "Badger", "Beaver", "Bison", "Cardinal", "Cheetah",
	"Cobra", "Comet", "Condor", "Coyote", "Cricket", "Dolphin", "Dragon",
	"Eagle", "Elk", "Falcon", "Ferret", "Finch", "Fox", "Gazelle",
	"Giraffe", "Goose", "Griffin", "Hawk", "Heron", "Hippo", "Ibex",
	"Iguana", "Jackal", "Jaguar", "Koala", "Lemur", "Leopard", "Lion",
	"Lynx", "Magpie", "Marmot", "Meerkat", "Mongoose", "Moose", "Narwhal",
	"Ocelot", "Orca", "Otter", "Owl", "Panda", "Panther", "Penguin",
	"Phoenix", "Puma", "Quokka", "Raven", "Robin", "Sable", "Seal",
	"Sparrow", "Stoat", "Tiger", "Walrus",
}

// generateLegibleOrgName returns a slug-friendly random org name like
// "Swift Otter 42".
func generateLegibleOrgName() string {
	adj := orgNameAdjectives[randIndex(len(orgNameAdjectives))]
	noun := orgNameNouns[randIndex(len(orgNameNouns))]
	suffix := 10 + randIndex(90)
	return fmt.Sprintf("%s %s %d", adj, noun, suffix)
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
