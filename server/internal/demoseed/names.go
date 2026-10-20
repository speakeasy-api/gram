package demoseed

import (
	"crypto/md5" //nolint:gosec // deterministic ids, not security
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// detUUID is a deterministic RFC-compliant UUID from a name: the md5 of the
// name with the version nibble forced to '5' and the variant nibble to '8'.
// Plain md5 bytes leave random version/variant nibbles, which fails the
// dashboard SDK's strict uuid validation (zod requires version 1-8 + variant
// [89ab]).
func detUUID(name string) uuid.UUID {
	h := md5Hex(name)
	b := []byte(h)
	b[12] = '5'
	b[16] = '8'
	return uuid.MustParse(string(b))
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s)) //nolint:gosec // deterministic ids, not security
	return hex.EncodeToString(sum[:])
}

// hashByte reads the byte at hex offset off (0-based, two hex chars) of
// md5(name).
func hashByte(name string, off int) int {
	h := md5Hex(name)
	v, err := strconv.ParseUint(h[off:off+2], 16, 8)
	if err != nil {
		panic(err)
	}
	return int(v)
}

// The humanized chat schedule. Bytes of md5(<seed>chat-<n>) pick a weighted
// day offset (busy days, quiet days, dead days), a work-hours-biased hour, and
// a minute, so usage charts do not read as a flat repeating pattern.
// Deterministic per n, relative to the run's start so the window keeps
// trailing. Postgres and ClickHouse rows both derive from these helpers, which
// is what keeps the two stores joined.

var (
	chatDayOffsets = [16]int{0, 0, 1, 1, 1, 2, 3, 3, 3, 3, 4, 5, 5, 7, 8, 11}
	chatHours      = [16]int{8, 9, 9, 10, 10, 11, 11, 13, 14, 14, 15, 16, 16, 17, 18, 20}
	// Weighted owner draw: priya and amara dominate, hana and lucas are
	// occasional users. Values index demoUsers.
	chatOwnerDraw = [16]int{2, 2, 2, 2, 2, 0, 0, 0, 0, 3, 3, 3, 1, 1, 4, 5}
)

func (s *seeder) chatName(n int) string { return s.spec.NameSeed + "chat-" + strconv.Itoa(n) }

// chatID is the id of bulk chat n.
func (s *seeder) chatID(n int) uuid.UUID { return detUUID(s.chatName(n)) }

// chatDayOff is the day offset (0 = today) chat n lands on; also keys "recent"
// behavior like the runbook v2 rollout split.
func (s *seeder) chatDayOff(n int) int {
	return chatDayOffsets[hashByte(s.chatName(n), 0)%16]
}

// chatTS is when chat n started. A day-0 chat placed later than "now" slides
// back a day so the seed never writes future timestamps.
func (s *seeder) chatTS(n int) time.Time {
	name := s.chatName(n)
	day := s.now.UTC().Truncate(24 * time.Hour)
	ts := day.AddDate(0, 0, -s.chatDayOff(n)).
		Add(time.Duration(chatHours[hashByte(name, 2)%16]) * time.Hour).
		Add(time.Duration(hashByte(name, 4)%60) * time.Minute)
	if ts.After(s.now.Add(-30 * time.Minute)) {
		ts = ts.AddDate(0, 0, -1)
	}
	return ts
}

// chatOwner is the demo user who owns chat n.
func (s *seeder) chatOwner(n int) demoUser {
	return demoUsers[chatOwnerDraw[hashByte(s.chatName(n), 12)%16]]
}

// chatSurface is the product surface chat n came from, in
// chat.CanonicalSource slugs. Odd chats use the fixed 8-message Claude Code
// shape, even ones the varied Cursor shape, with every 6th even chat
// attributed to Codex so the Watchdog "App" grouping and the sessions
// agent-type filter both have more than two values.
func chatSurface(n int) string {
	switch {
	case n%2 == 1:
		return "claude-code"
	case n%6 == 2:
		return "codex"
	default:
		return "cursor"
	}
}

// riskTypeDraw is the weighted 64-slot table that keeps the Watchdog signal
// list from being a flat rotation: customer-data noise (types 3 and 12)
// dominates, regulated-data and prompt-injection hits are rare.
var riskTypeDraw = [64]int{
	-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1,
	3, 3, 3, 3, 3, 3, 3, 3,
	12, 12, 12, 12, 12,
	0, 0, 0, 0,
	1, 1, 1,
	6, 6, 6, 6, 6,
	10, 10, 10, 10,
	2, 2, 2,
	9, 9, 9,
	4, 4,
	11, 11,
	5, 5,
}

func (s *seeder) riskPickName(n int) string {
	return s.spec.NameSeed + "riskpick-" + strconv.Itoa(n)
}

// riskType is the risk finding type chat n carries, or -1 for none. The two
// rarest custom-rule types (7 and 8) fire ONLY inside the trailing 3 days so
// they read as newly-emerged signals with an empty previous window.
func (s *seeder) riskType(n int) int {
	name := s.riskPickName(n)
	if s.chatDayOff(n) <= 2 && hashByte(name, 2)%8 == 0 {
		return 7 + hashByte(name, 4)%2
	}
	return riskTypeDraw[hashByte(name, 0)%64]
}

var riskSuppressionDraw = [16]int{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 2, 2, 3, 3}

// riskSuppression is how a finding was suppressed after the fact: 0 live, 2
// dismissed by a reviewer, 3 swept by the offline false-positive job. (1,
// suppressed by an exclusion rule, follows from the match value, so callers
// set it directly.)
func (s *seeder) riskSuppression(n int) int {
	return riskSuppressionDraw[hashByte(s.riskPickName(n), 6)%16]
}

// hash64 is the ClickHouse seed's source of deterministic variety (which chat
// fails, how long a call took, ...), replacing the old script's cityHash64
// calls. It only has to be stable per input, not equal to cityHash64: nothing
// joins on these values.
func hash64(parts ...any) uint64 {
	h := md5.New() //nolint:gosec // deterministic variety, not security
	for _, p := range parts {
		_, _ = fmt.Fprint(h, p)
		_, _ = h.Write([]byte{0})
	}
	// md5's output is well mixed in every bit; FNV's low bits are not, and
	// these values are almost always taken modulo a small number.
	return binary.BigEndian.Uint64(h.Sum(nil))
}
