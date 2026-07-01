package wire

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"
)

// RendezvousOrder ranks candidates by highest-random-weight hashing for a stable key.
func RendezvousOrder(key string, candidates []string) []string {
	if key == "" || len(candidates) == 0 {
		return nil
	}

	ranked := make([]rendezvousCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		sum := sha256.Sum256([]byte(key + "\x00" + candidate))
		ranked = append(ranked, rendezvousCandidate{
			value: candidate,
			score: binary.BigEndian.Uint64(sum[:8]),
		})
	}
	return orderRendezvousCandidates(ranked)
}

func orderRendezvousCandidates(ranked []rendezvousCandidate) []string {
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score == ranked[j].score {
			return ranked[i].value < ranked[j].value
		}
		return ranked[i].score > ranked[j].score
	})

	ordered := make([]string, len(ranked))
	for i, candidate := range ranked {
		ordered[i] = candidate.value
	}
	return ordered
}

func RendezvousPick(key string, candidates []string, exclude map[string]struct{}) (string, bool) {
	for _, candidate := range RendezvousOrder(key, candidates) {
		if _, skip := exclude[candidate]; skip {
			continue
		}
		return candidate, true
	}
	return "", false
}

type rendezvousCandidate struct {
	value string
	score uint64
}
