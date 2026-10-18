package signals

import (
	"fmt"
	"slices"
	"strings"
)

// EvidenceCandidates bounds exact-resource access checks and supporting examples.
const EvidenceCandidates = 20

// Summary computes whole-window companions without building chart series.
func (p *Plan) Summary() Query {
	return Query{SQL: "SELECT " + strings.Join(measures, ", ") + " FROM (" + p.Prepared.SQL + ") LIMIT 1" + settings, Args: slices.Clone(p.Prepared.Args)}
}

// Concentration counts messages per conversation before selecting the top five.
// The window sum includes every matching conversation, including unreturned ones.
func (p *Plan) Concentration() Query {
	return Query{SQL: `SELECT session_matches, sum(session_matches) OVER () AS linked_matched_messages
 FROM (SELECT conversation_id, countIf(hit) AS session_matches FROM (` + p.Prepared.SQL + `)
 WHERE isNotNull(conversation_id) GROUP BY conversation_id HAVING session_matches > 0)
 ORDER BY session_matches DESC LIMIT 5` + settings, Args: slices.Clone(p.Prepared.Args)}
}

// Cooccurrence counts joint answers after collapsing each side to the named unit.
// Both source branches retain their full tenant, run, sensor and time prefix.
func (p *Plan) Cooccurrence(peer *Plan, sessions bool) Query {
	unit, scope := "event_id", ""
	if sessions {
		unit, scope = "conversation_id", " WHERE isNotNull(conversation_id)"
	}
	query := `SELECT countIf(a_measured AND b_measured) AS joint_answered,
 countIf(a_hit AND b_hit) AS joint_matches,
 countIf(a_hit AND b_measured) AS a_matches,
 countIf(b_hit AND a_measured) AS b_matches
 FROM (SELECT ` + unit + `,
 maxIf(measured, side = 0) AS a_measured, maxIf(measured, side = 1) AS b_measured,
 maxIf(hit, side = 0) AS a_hit, maxIf(hit, side = 1) AS b_hit
 FROM (SELECT event_id, conversation_id, measured, hit, 0 AS side FROM (` + p.Prepared.SQL + `)
 UNION ALL SELECT event_id, conversation_id, measured, hit, 1 AS side FROM (` + peer.Prepared.SQL + `))` + scope + ` GROUP BY ` + unit + `) LIMIT 1` + settings
	return Query{SQL: query, Args: append(slices.Clone(p.Prepared.Args), peer.Prepared.Args...)}
}

// Evidence samples the most recent hit in each of at most twenty conversations.
// These are candidates only: callers must check exact conversation access.
func (p *Plan) Evidence(peer *Plan) Query {
	peerFilter := ""
	args := slices.Clone(p.Prepared.Args)
	if peer != nil {
		peerFilter = " AND conversation_id IN (SELECT conversation_id FROM (" + peer.Prepared.SQL + ") WHERE hit AND isNotNull(conversation_id))"
		args = append(args, peer.Prepared.Args...)
	}
	return Query{SQL: `SELECT toString(id) AS reading_id, event_id AS message_id,
 ifNull(toString(conversation_id), '') AS conversation_id, event_occurred_at, definition_hash, match_probability AS rule_value
 FROM (` + p.Prepared.SQL + `) WHERE hit AND isNotNull(conversation_id)` + peerFilter + `
 ORDER BY event_occurred_at DESC, id ASC LIMIT 1 BY conversation_id LIMIT ` + fmt.Sprint(EvidenceCandidates) + settings,
		Args: args}
}
