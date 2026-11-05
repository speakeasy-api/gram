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

// The agent owner persona's sessions. Both stores read this table: Postgres
// writes the chats and transcripts, ClickHouse the tool calls behind them,
// and the owner-scoped reads join the two by chat id and call id.

// ownerSession is one session of the agent owner persona. agent marks a
// session the agent's credential drove, which carries no user at all.
type ownerSession struct {
	key    string
	title  string
	prompt string
	answer string
	tools  []string
	ageMin int
	agent  bool
}

var ownerSessions = []ownerSession{
	{"nightly", "Nightly deploy watch: payments-api",
		"Check the latest payments-api deploy and report anything unhealthy.",
		"The latest deploy is serving and every health check passed. Nothing to escalate.",
		[]string{"list_deploys", "check_health"}, 6 * 60, true},
	{"canary", "Canary health check: checkout",
		"Compare the checkout canary's latency and errors against the baseline.",
		"p95 latency is within 5% of baseline and the error rate is flat. The canary can proceed.",
		[]string{"get_metrics", "fetch_traces"}, 27 * 60, true},
	{"rollback", "Rollback review: orders worker",
		"The orders worker was rolled back overnight. Find out why.",
		"The rollback followed a burst of timeouts after the previous deploy; the logs point at a missing index.",
		[]string{"list_deploys", "search_logs"}, 53 * 60, true},
	{"dryrun", "Deploy watcher dry run",
		"Run the deploy watcher checks for payments-api by hand so I can compare them.",
		"Same result as the watcher: the deploy is healthy and all checks pass.",
		[]string{"list_deploys", "check_health"}, 4 * 60, false},
	{"canary-why", "Why did the canary flag checkout?",
		"Pull the checkout canary metrics from yesterday and explain the flag.",
		"The flag came from a single p95 spike during the cache warm-up; it cleared within two minutes.",
		[]string{"get_metrics"}, 25 * 60, false},
}

// ownerAgentSessionCount is how many of ownerSessions the agent drove on its
// own credential, which the Postgres postflight expects to read back.
func ownerAgentSessionCount() int64 {
	var n int64
	for _, sess := range ownerSessions {
		if sess.agent {
			n++
		}
	}
	return n
}

// ownerHookSession is the agent session whose harness also sent hook rows:
// the first one the agent drove, wherever it sits in ownerSessions.
func ownerHookSession() ownerSession {
	for _, sess := range ownerSessions {
		if sess.agent {
			return sess
		}
	}
	var none ownerSession
	return none
}

// An owner session's timeline, in minutes after its prompt: the assistant's
// tool-call turn, then result n a minute apart, then the answer. Both stores
// derive their timestamps from it so the transcript and Tool Logs agree.
const ownerCallsTurnMin = 1

func ownerResultMin(call int) int { return ownerCallsTurnMin + call }

func ownerAnswerMin(calls int) int { return ownerResultMin(calls) + 1 }

// ownerCallOffset is when call n ran: after the tool-call turn, 30s before
// its result lands in the transcript.
func ownerCallOffset(call int) time.Duration {
	return time.Duration(ownerResultMin(call))*time.Minute - 30*time.Second
}

// ownerAgentID is the agent the owner persona owns.
func (s *seeder) ownerAgentID() uuid.UUID { return s.det("managed-agent-5") }

// ownerSessionChatID is the chat of owner session key.
func (s *seeder) ownerSessionChatID(key string) uuid.UUID { return s.det("owner-session-" + key) }

// ownerToolCallID is the id both stores give tool call k (1-based) of owner
// session key, so the transcript's tool message and the telemetry row pair.
func ownerToolCallID(key string, k int) string {
	return "call_demo_owner_" + key + "_" + strconv.Itoa(k)
}

// ownerToolIO is the per-call id and magnitude the arguments and result of
// tool call k are written from, so the transcript and the telemetry agree.
func (s *seeder) ownerToolIO(key string, k int) (string, uint64) {
	return md5Hex(s.name("ownerio-" + key + "-" + strconv.Itoa(k)))[:8], hash64("ownermag", key, k) % 900
}

// ownerToolArgs and ownerToolResult are tool call k's payloads, escaped for
// the attributes JSON like chToolArgs and chToolResult.
func (s *seeder) ownerToolArgs(sess ownerSession, k int) string {
	ioID, ioMag := s.ownerToolIO(sess.key, k)
	return chToolArgs(sess.tools[k-1], ioID, ioMag)
}

func (s *seeder) ownerToolResult(sess ownerSession, k int) string {
	ioID, ioMag := s.ownerToolIO(sess.key, k)
	return chToolResult(sess.tools[k-1], false, ioID, ioMag)
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
