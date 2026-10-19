package demoseed

import (
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// demoUser is one member of the demo employee roster. The identity profile
// (division, department, ...) mirrors the user.attributes.* values on the
// ClickHouse rows, so directory data and telemetry dimensions agree.
type demoUser struct {
	// handle builds the tenant's user id and email: see seeder.userID and
	// seeder.email.
	handle     string
	name       string
	division   string
	department string
	title      string
	empType    string
	costCenter string
	team       string
	hostname   string
}

// demoUsers is the roster, in the order the old seed's parallel arrays used
// (index i here was element i+1 there).
var demoUsers = []demoUser{
	{"amara", "Amara Okafor", "Customer Experience", "Support Engineering", "Support Engineer", "full-time", "CC-SUP-4100", "Frontline Support", "amara-mbp.local"},
	{"jonas", "Jonas Lindqvist", "Customer Experience", "Support Engineering", "Senior Support Engineer", "full-time", "CC-SUP-4100", "Frontline Support", "jonas-mbp.local"},
	{"priya", "Priya Raman", "R&D", "Platform Engineering", "Platform Engineer", "full-time", "CC-ENG-2200", "Infra", "priya-mbp.local"},
	{"mateo", "Mateo Alvarez", "R&D", "Platform Engineering", "Site Reliability Engineer", "contractor", "CC-ENG-2200", "Reliability", "mateo-mbp.local"},
	{"hana", "Hana Sato", "Customer Experience", "Billing Operations", "Billing Analyst", "part-time", "CC-OPS-3300", "Billing Ops", "hana-mbp.local"},
	{"lucas", "Lucas Meyer", "R&D", "Engineering Leadership", "Engineering Manager", "full-time", "CC-ENG-2200", "Leadership", "lucas-mbp.local"},
}

// demoDirectoryPerson is a directory profile with no Speakeasy member behind
// it, so the directory people view shows more than members. Profiles that
// share an emailHandle are deliberately ambiguous: the same address appears
// twice in the directory, so neither is matched to an account.
type demoDirectoryPerson struct {
	handle      string
	emailHandle string
	firstName   string
	lastName    string
	department  string
	title       string
	team        string
}

// demoDirectoryPeople reuse roster teams, so their groups already exist.
var demoDirectoryPeople = []demoDirectoryPerson{
	{"noor", "noor", "Noor", "Haddad", "Support Engineering", "Support Engineer", "Frontline Support"},
	{"tomas", "tomas", "Tomás", "Novak", "Platform Engineering", "Platform Engineer", "Infra"},
	{"ines-a", "ines", "Inês", "Costa", "Billing Operations", "Billing Analyst", "Billing Ops"},
	{"ines-b", "ines", "Inês", "Costa", "Billing Operations", "Billing Analyst", "Billing Ops"},
}

// splitDemoName splits a roster display name into the first and last names a
// directory reports.
func splitDemoName(name string) (string, string) {
	first, last, _ := strings.Cut(name, " ")
	return first, last
}

// bulkChats is how many generated chats the seed writes.
const bulkChats = 180

var (
	chatTitles = []string{
		"Incident triage: payment 401s", "Refund request for order 4823",
		"Deploy rollout check", "Webhook delivery debugging",
		"Latency regression on /orders", "Billing dispute follow-up",
		"Schema migration review", "On-call escalation summary",
		"Cache hit rate investigation", "Rate limit tuning",
	}
	chatQuestions = []string{
		"Can you check why the checkout flow is erroring?",
		"Pull the latest metrics for the orders service.",
		"Investigate the spike in 500s since this morning.",
		"Why is the worker queue backing up?",
		"Find the slow query hitting the dashboard.",
		"Walk me through the last incident timeline.",
	}
	chatAnswers = []string{
		"Done. Metrics look within normal range — nothing alarming.",
		"Pulled the data: error rate is 0.2%, well under the alert threshold.",
		"The spike traces back to a slow downstream dependency; it recovered on its own.",
		"Queue depth is back to baseline after the worker pool scaled up.",
		"The slow query was missing an index; noted for a follow-up migration.",
		"Reconciled — every charge in the batch has a matching ledger entry.",
	}

	toolNames = []string{
		"search_logs", "get_metrics", "query_db", "get_customer",
		"list_deploys", "process_refund", "fetch_traces", "check_health",
	}
	auditActions = []string{
		"toolset:create", "deployments:create", "api_key:create",
		"toolset:update", "toolset:update", "plugin:publish",
	}

	skillNames = []string{"support-refunds", "triage-incident", "runbook"}
)

// toolURN is the deployment tool urn for a tool name, matching the
// ClickHouse telemetry: tools:http:acme:<name>.
func toolURN(name string) string { return "tools:http:acme:" + name }

// Read-only tool verbs the destructive-command policy exempts. Declared once
// because both of that policy's categories carry the same exemption.
const dsReadonlyExempt = `tool_calls.size() > 0 && tool_calls.all(t, ["get_","list_","search_","query_","fetch_","check_"].exists(v, t.function.matchPrefix(v)))`

// Marked as fake in the value itself. Nothing verifies these: no demo agent
// ever presents a secret, and the seed must never carry a real hash.
const demoSecretHash = "$2a$10$DEMOSEEDNOTAREALBCRYPTHASHxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" //nolint:gosec // inert placeholder marked fake in the value, not a credential

const demoCIMDURL = "https://agents.example.com/.well-known/oauth-client"

const (
	skillContentRefunds = "---\nname: support-refunds\ndescription: How to process customer refunds safely.\n---\n\n# Refund handling\n\n1. Verify the order id and amount with the customer.\n2. Use the process_refund tool with the confirmed amount.\n3. Never accept full card numbers in chat.\n"
	skillContentTriage  = "---\nname: triage-incident\ndescription: Steps for triaging a production incident.\n---\n\n# Incident triage\n\n1. Pull error logs with search_logs for the affected window.\n2. Correlate with recent deploys via list_deploys.\n3. Escalate to on-call when the error budget is at risk.\n"
	skillContentRunbook = "---\nname: runbook\ndescription: General operational runbook for the Acme stack.\n---\n\n# Acme runbook\n\n- check_health before and after every rollout.\n- fetch_traces for latency regressions.\n- get_metrics to confirm recovery.\n"
)

// Fixed ids. Each is the tenant's uuid prefix plus a fixed suffix, so every
// tenant gets its own.

func (s *seeder) policyA() uuid.UUID  { return s.fixed("0000-4000-a000-00000000f001") }
func (s *seeder) policyPI() uuid.UUID { return s.fixed("0000-4000-a000-00000000f002") }
func (s *seeder) policyDS() uuid.UUID { return s.fixed("0000-4000-a000-00000000f003") }
func (s *seeder) policySM() uuid.UUID { return s.fixed("0000-4000-a000-00000000f004") }
func (s *seeder) policyAI() uuid.UUID { return s.fixed("0000-4000-a000-00000000f005") }
func (s *seeder) policyCR() uuid.UUID { return s.fixed("0000-4000-a000-00000000f006") }
func (s *seeder) policyCD() uuid.UUID { return s.fixed("0000-4000-a000-00000000f007") }
func (s *seeder) policyTB() uuid.UUID { return s.fixed("0000-4000-a000-00000000f008") }
func (s *seeder) policyQ() uuid.UUID  { return s.fixed("0000-4000-a000-00000000f009") }
func (s *seeder) policyMA() uuid.UUID { return s.fixed("0000-4000-a000-00000000f010") }
func (s *seeder) policyAP() uuid.UUID { return s.fixed(approvalPolicyFixedID) }

// approvalPolicyFixedID is the tool approval policy's fixed id suffix, shared
// with the local fixture that adds the developer as an approver.
const approvalPolicyFixedID = "0000-4000-a000-00000000f011"

func (s *seeder) exclFixture() uuid.UUID    { return s.fixed("0000-4000-a000-00000000ec01") }
func (s *seeder) exclTestcard() uuid.UUID   { return s.fixed("0000-4000-a000-00000000ec02") }
func (s *seeder) exclExamplekey() uuid.UUID { return s.fixed("0000-4000-a000-00000000ec03") }

func (s *seeder) assetID() uuid.UUID        { return s.fixed("0000-4000-a000-00000000a001") }
func (s *seeder) deployID() uuid.UUID       { return s.fixed("0000-4000-a000-00000000d001") }
func (s *seeder) doaID() uuid.UUID          { return s.fixed("0000-4000-a000-00000000da01") }
func (s *seeder) toolset1() uuid.UUID       { return s.fixed("0000-4000-a000-000000005e01") }
func (s *seeder) toolset2() uuid.UUID       { return s.fixed("0000-4000-a000-000000005e02") }
func (s *seeder) toolset3() uuid.UUID       { return s.fixed("0000-4000-a000-000000005e03") }
func (s *seeder) toolset4() uuid.UUID       { return s.fixed("0000-4000-a000-000000005e04") }
func (s *seeder) usIssuer() uuid.UUID       { return s.fixed("0000-4000-a000-000000005a01") }
func (s *seeder) chainingIssuer() uuid.UUID { return s.fixed("0000-4000-a000-000000005a11") }
func (s *seeder) chainingClient() uuid.UUID { return s.fixed("0000-4000-a000-000000005c11") }

// One registered agent per credential kind the Connections list can report,
// plus the pre-column row whose kind is resolved from the rest of it.
func (s *seeder) uscKey() uuid.UUID    { return s.fixed("0000-4000-a000-000000005c01") }
func (s *seeder) uscPublic() uuid.UUID { return s.fixed("0000-4000-a000-000000005c02") }
func (s *seeder) uscSecret() uuid.UUID { return s.fixed("0000-4000-a000-000000005c03") }
func (s *seeder) uscLegacy() uuid.UUID { return s.fixed("0000-4000-a000-000000005c04") }
func (s *seeder) uscBroken() uuid.UUID { return s.fixed("0000-4000-a000-000000005c05") }

func (s *seeder) ruleMonthly() uuid.UUID { return s.fixed("0000-4000-a000-00000000e001") }
func (s *seeder) ruleWeekly() uuid.UUID  { return s.fixed("0000-4000-a000-00000000e002") }

// skillID and skillVersionID are the three manual skills (i is 1..3).
func (s *seeder) skillID(i int) uuid.UUID {
	return s.fixed("0000-4000-a000-0000000051a" + strconv.Itoa(i))
}

func (s *seeder) skillVersionID(i int) uuid.UUID {
	return s.fixed("0000-4000-a000-0000000052a" + strconv.Itoa(i))
}
