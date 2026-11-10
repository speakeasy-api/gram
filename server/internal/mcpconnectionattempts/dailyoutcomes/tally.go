package dailyoutcomes

import (
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
)

// minOrgsPerLabel is how many organizations must share a row's provider, app and
// client family for those names to be kept, so no stored row points to one customer.
const minOrgsPerLabel = 3

// uncountedOrganizations are Speakeasy's own. Their attempts are kept but do not count
// toward minOrgsPerLabel, so they cannot make one customer's vendor look shared.
var uncountedOrganizations = map[string]struct{}{
	constants.SpeakeasyTeamOrganizationID: {},
}

// DayRow is one row of mcp_connection_outcomes_daily, without its day.
type DayRow struct {
	// Provider is a known vendor host used by enough organizations, other, or empty.
	Provider string

	// App is a catalog app used by enough organizations, a kind of app, other, or empty.
	App string

	// ClientFamily is a known client family, other, or empty.
	ClientFamily string

	// Step is where the attempts ended.
	Step string

	// Outcome is how the attempts ended.
	Outcome string

	// Reason is the reason code, empty for success.
	Reason string

	// FixedBy is what fixed the failures, empty when nothing did.
	FixedBy string

	// Attempts is how many attempts the row counts.
	Attempts uint64
}

// rowKey is everything in a DayRow but its count.
type rowKey struct {
	provider     string
	app          string
	clientFamily string
	step         string
	outcome      string
	reason       string
	fixedBy      string
}

// tally adds up attempts per row and organization. Every label passes its allowlist
// here, and rows folds rare names, so no value that could name a
// customer reaches the long-term table.
type tally struct {
	attempts map[rowKey]map[string]uint64

	// unknown counts values outside the known steps, outcomes, reasons and fixes, by column.
	unknown map[string]int
}

func newTally() *tally {
	return &tally{attempts: map[rowKey]map[string]uint64{}, unknown: map[string]int{}}
}

// known returns label, counting it when it replaced a value outside the known set.
func (t *tally) known(column, value, label string) string {
	if label != value {
		t.unknown[column]++
	}
	return label
}

func (t *tally) add(a mcpconnectionattempts.AttemptSummary, e mcpconnectionattempts.Ending, fix mcpconnectionattempts.FixedBy) {
	key := rowKey{
		provider:     mcpconnectionattempts.ProviderLabel(a.ProviderHost),
		app:          mcpconnectionattempts.AppLabel(a.AppKey),
		clientFamily: mcpconnectionattempts.ClientFamilyLabel(a.ClientFamily),
		step:         t.known("step", e.Step, mcpconnectionattempts.StepLabel(e.Step)),
		outcome:      t.known("outcome", string(e.Outcome), mcpconnectionattempts.OutcomeLabel(string(e.Outcome))),
		reason:       t.known("reason", e.Reason, mcpconnectionattempts.ReasonLabel(e.Reason)),
		fixedBy:      t.known("fixed_by", string(fix), mcpconnectionattempts.FixedByLabel(fix)),
	}
	if t.attempts[key] == nil {
		t.attempts[key] = map[string]uint64{}
	}
	t.attempts[key][a.OrganizationID] += max(a.Weight, 1)
}

// rows folds names until every row's named provider, app and client family together
// come from at least minOrgsPerLabel counted organizations, then sums. A rare row
// loses its app first, then its provider, then its client family. App kinds and
// other name nothing, so they are never folded.
func (t *tally) rows() []DayRow {
	byKey := t.attempts
	for {
		orgs := map[rowKey]map[string]struct{}{}
		for key, byOrg := range byKey {
			for org := range byOrg {
				if _, skip := uncountedOrganizations[org]; !skip {
					addOrg(orgs, named(key), org)
				}
			}
		}
		next, changed := map[rowKey]map[string]uint64{}, false
		for key, byOrg := range byKey {
			if n := named(key); n.hasName() && len(orgs[n]) < minOrgsPerLabel {
				key, changed = foldOne(key), true
			}
			if next[key] == nil {
				next[key] = map[string]uint64{}
			}
			for org, attempts := range byOrg {
				next[key][org] += attempts
			}
		}
		byKey = next
		if !changed {
			break
		}
	}
	rows := make([]DayRow, 0, len(byKey))
	for key, byOrg := range byKey {
		var attempts uint64
		for _, n := range byOrg {
			attempts += n
		}
		rows = append(rows, DayRow{
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

// named keeps only the provider, app and client family names that could point to a customer.
func named(key rowKey) rowKey {
	app := key.app
	if mcpconnectionattempts.IsAppKind(app) {
		app = ""
	}
	return rowKey{provider: nameOf(key.provider), app: nameOf(app), clientFamily: nameOf(key.clientFamily), step: "", outcome: "", reason: "", fixedBy: ""}
}

func (k rowKey) hasName() bool {
	return k.provider != "" || k.app != "" || k.clientFamily != ""
}

// foldOne replaces the most specific name in a rare row with other.
func foldOne(key rowKey) rowKey {
	n := named(key)
	switch {
	case n.app != "":
		key.app = mcpconnectionattempts.LabelOther
	case n.provider != "":
		key.provider = mcpconnectionattempts.LabelOther
	default:
		key.clientFamily = mcpconnectionattempts.LabelOther
	}
	return key
}

// nameOf drops other, which names nothing.
func nameOf(label string) string {
	if label == mcpconnectionattempts.LabelOther {
		return ""
	}
	return label
}

func addOrg(orgs map[rowKey]map[string]struct{}, key rowKey, org string) {
	if orgs[key] == nil {
		orgs[key] = map[string]struct{}{}
	}
	orgs[key][org] = struct{}{}
}
