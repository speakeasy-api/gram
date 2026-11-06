// Package dailyoutcomes builds mcp_connection_outcomes_daily: per UTC day, how
// connection attempts ended and what fixed the failures, with no organization,
// project or person. Raw events last 30 days, this rollup lasts 13 months. The
// rules themselves live in mcpconnectionattempts, so other readers apply the same ones.
package dailyoutcomes

import (
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
)

// pairKey is the person, app and server a fix must share with the failure.
type pairKey struct {
	organizationID string
	subjectURN     string
	appKey         string
	serverID       string
}

func keyOf(a mcpconnectionattempts.AttemptSummary) pairKey {
	return pairKey{organizationID: a.OrganizationID, subjectURN: a.SubjectURN, appKey: a.AppKey, serverID: a.ServerID}
}

// successIndex groups candidate successes by person, app and server.
type successIndex map[pairKey][]mcpconnectionattempts.AttemptSummary

func newSuccessIndex(successes []mcpconnectionattempts.AttemptSummary) successIndex {
	index := successIndex{}
	for _, s := range successes {
		index[keyOf(s)] = append(index[keyOf(s)], s)
	}
	return index
}

// Row is one row of mcp_connection_outcomes_daily, without its day.
type Row struct {
	Provider     string
	App          string
	ClientFamily string
	Step         string
	Outcome      string
	Reason       string
	FixedBy      mcpconnectionattempts.FixedBy
	Attempts     uint64
}

// rowKey is everything in a Row but its count.
type rowKey struct {
	provider     string
	app          string
	clientFamily string
	step         string
	outcome      string
	reason       string
	fixedBy      mcpconnectionattempts.FixedBy
}

// tally adds up attempts per row. Every label passes its allowlist here, so no value
// that could name a customer reaches the long-term table.
type tally map[rowKey]uint64

func (t tally) add(a mcpconnectionattempts.AttemptSummary, e mcpconnectionattempts.Ending, fix mcpconnectionattempts.FixedBy) {
	key := rowKey{
		provider:     mcpconnectionattempts.ProviderLabel(a.ProviderHost),
		app:          mcpconnectionattempts.AppLabel(a.AppKey),
		clientFamily: mcpconnectionattempts.ClientFamilyLabel(a.ClientFamily),
		step:         e.Step,
		outcome:      string(e.Outcome),
		reason:       e.Reason,
		fixedBy:      fix,
	}
	t[key] += max(a.Weight, 1)
}

func (t tally) rows() []Row {
	rows := make([]Row, 0, len(t))
	for key, attempts := range t {
		rows = append(rows, Row{
			Provider:     key.provider,
			App:          key.app,
			ClientFamily: key.clientFamily,
			Step:         key.step,
			Outcome:      key.outcome,
			Reason:       key.reason,
			FixedBy:      key.fixedBy,
			Attempts:     attempts,
		})
	}
	return rows
}
