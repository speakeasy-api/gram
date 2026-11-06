package dailyoutcomes

import (
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
)

// minOrgsPerLabel is how many organizations must use a provider or app on a day for
// its name to be kept, so a vendor only one customer uses cannot point to that customer.
const minOrgsPerLabel = 3

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
// here, and rows folds rare providers and apps, so no value that could name a
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

// rows folds providers and apps used by fewer than minOrgsPerLabel organizations into
// other, then sums. App kinds are kept: they name no vendor.
func (t *tally) rows() []DayRow {
	providerOrgs, appOrgs := map[string]map[string]struct{}{}, map[string]map[string]struct{}{}
	for key, byOrg := range t.attempts {
		for org := range byOrg {
			addOrg(providerOrgs, key.provider, org)
			addOrg(appOrgs, key.app, org)
		}
	}
	folded := map[rowKey]uint64{}
	for key, byOrg := range t.attempts {
		if rare(providerOrgs, key.provider) {
			key.provider = mcpconnectionattempts.LabelOther
		}
		if !mcpconnectionattempts.IsAppKind(key.app) && rare(appOrgs, key.app) {
			key.app = mcpconnectionattempts.LabelOther
		}
		for _, attempts := range byOrg {
			folded[key] += attempts
		}
	}
	rows := make([]DayRow, 0, len(folded))
	for key, attempts := range folded {
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

func addOrg(orgs map[string]map[string]struct{}, label, org string) {
	if orgs[label] == nil {
		orgs[label] = map[string]struct{}{}
	}
	orgs[label][org] = struct{}{}
}

// rare reports whether a named label has too few organizations to keep.
func rare(orgs map[string]map[string]struct{}, label string) bool {
	if label == "" || label == mcpconnectionattempts.LabelOther {
		return false
	}
	return len(orgs[label]) < minOrgsPerLabel
}
