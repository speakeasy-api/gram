package memoryeval

import "strings"

// QuestionKind groups questions by what they test.
type QuestionKind string

const (
	// QuestionPointLookup asks for one decision.
	QuestionPointLookup QuestionKind = "point_lookup"

	// QuestionTimeline asks for changes across several sessions, in order.
	QuestionTimeline QuestionKind = "timeline"

	// QuestionReversal asks for the current state of a decision that changed.
	QuestionReversal QuestionKind = "reversal"

	// QuestionNegative asks about something the team never discussed.
	QuestionNegative QuestionKind = "negative"

	// QuestionIsolation asks about something that exists only in a private
	// session or in the other project.
	QuestionIsolation QuestionKind = "isolation"
)

// NotFound is the answer key for questions memory must not answer.
const NotFound = "Not found: nothing in the project's team history answers this."

// Question is one graded question about the main project.
type Question struct {
	// ID is a stable identifier such as q01.
	ID string

	// Kind is what the question tests.
	Kind QuestionKind

	// Text is what the answering agent is asked.
	Text string

	// Answer is the answer key the judge grades against.
	Answer string

	// Sources are the keys of the chats the answer comes from. Empty when the
	// expected answer is NotFound.
	Sources []string

	// Evidence lists groups of terms. A cited chat, memo or digest supports
	// the answer when it contains every term of at least one group.
	Evidence [][]string
}

// ExpectsNotFound reports whether the correct answer is NotFound.
func (q Question) ExpectsNotFound() bool {
	return q.Kind == QuestionNegative || q.Kind == QuestionIsolation
}

// Supports reports whether text contains every term of at least one evidence
// group, ignoring case.
func (q Question) Supports(text string) bool {
	text = strings.ToLower(text)
	for _, group := range q.Evidence {
		all := len(group) > 0
		for _, term := range group {
			if !strings.Contains(text, strings.ToLower(term)) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

func lookup(id, text, answer, source string, evidence ...string) Question {
	return Question{ID: id, Kind: QuestionPointLookup, Text: text, Answer: answer, Sources: []string{source}, Evidence: [][]string{evidence}}
}

func reversal(id, text, answer, source string, evidence ...string) Question {
	return Question{ID: id, Kind: QuestionReversal, Text: text, Answer: answer, Sources: []string{source}, Evidence: [][]string{evidence}}
}

func timeline(id, text, answer string, sources []string, evidence ...[]string) Question {
	return Question{ID: id, Kind: QuestionTimeline, Text: text, Answer: answer, Sources: sources, Evidence: evidence}
}

func notFound(id string, kind QuestionKind, text string) Question {
	return Question{ID: id, Kind: kind, Text: text, Answer: NotFound, Sources: nil, Evidence: nil}
}

// questions returns the 40 graded questions: 15 point lookups, 8 timelines,
// 7 reversals, 5 negatives and 5 isolation probes.
func questions() []Question {
	return []Question{
		lookup("q01", "Which system did we pick to run customer webhook retries, and when do they give up?",
			"Temporal workflows with exponential backoff starting at 30 seconds; they give up after 24 hours and mark the delivery failed.",
			"decision-webhook-retries", "Temporal", "24 hours"),
		lookup("q02", "Which feature flag provider did we choose?",
			"PostHog feature flags, chosen over LaunchDarkly because we already use PostHog for analytics.",
			"decision-feature-flags", "PostHog"),
		lookup("q03", "How do list endpoints in our public API paginate, and what is the max page size?",
			"Opaque base64 cursors; default page size 50, maximum 200.",
			"decision-pagination", "cursor", "200"),
		lookup("q04", "What did we standardize on for server state in the frontend instead of Redux?",
			"TanStack Query for all server state; Redux was dropped.",
			"decision-frontend-state", "TanStack Query"),
		lookup("q05", "What's the row limit on synchronous CSV exports?",
			"50,000 rows; larger exports run in the background and email a link that expires after 7 days.",
			"decision-csv-export", "50,000"),
		lookup("q06", "How long do we keep raw tracking events in ClickHouse?",
			"90 days (daily rollups are kept for 2 years).",
			"decision-event-ttl", "90 days"),
		lookup("q07", "Which error tracker did we standardize on across our apps?",
			"Sentry, for the backend, frontend and driver app, with a 20% performance trace sample rate.",
			"decision-error-tracking", "Sentry"),
		lookup("q08", "What's our rule for production deploys on Fridays?",
			"No production deploys after 2pm on Fridays; hotfixes after that need on-call approval.",
			"decision-deploy-freeze", "2pm", "Friday"),
		lookup("q09", "What tool do we use for database schema migrations?",
			"Atlas, generating migrations from schema.sql (replacing golang-migrate).",
			"decision-migrations", "Atlas"),
		lookup("q10", "How are our outgoing webhooks signed?",
			"HMAC-SHA256 over timestamp plus body, in the X-Lumen-Signature header, with a 5 minute timestamp tolerance.",
			"decision-webhook-signature", "HMAC-SHA256", "X-Lumen-Signature"),
		lookup("q11", "What primary key format do new tables use?",
			"UUIDv7.",
			"decision-id-format", "UUIDv7"),
		lookup("q12", "How do we store money amounts in the database?",
			"As integer cents in bigint columns with a separate ISO currency code column; never floats.",
			"decision-money", "cents"),
		lookup("q13", "Which timezone does the UI show shipment times in?",
			"The pickup location's local timezone (stored in UTC, with the UTC offset in a tooltip).",
			"decision-timezones", "pickup", "local"),
		lookup("q14", "Which test runner did we choose for frontend unit tests?",
			"Vitest, replacing Jest.",
			"decision-test-runner", "Vitest"),
		lookup("q15", "When does the on-call rotation hand off?",
			"Weekly, every Monday at 10am, with a written handoff note.",
			"decision-oncall", "Monday", "10am"),

		timeline("q16", "What changed in the auth work over the last month, in order?",
			"1) Dashboard login moved to OAuth 2.0 authorization code with PKCE and the implicit flow was disabled; 2) refresh token rotation shipped, with reuse revoking the session family; 3) session lifetime was cut from 12 hours to 8 hours.",
			[]string{"timeline-auth-pkce", "timeline-auth-rotation", "timeline-auth-lifetime"},
			[]string{"PKCE"}, []string{"rotation"}, []string{"8 hours"}),
		timeline("q17", "What were the steps of the billing migration?",
			"1) Decided to move invoicing from the in-house cron to Stripe Billing; 2) backfilled customers and ran dual-write with nightly diffs; 3) cut over, turned the in-house invoicer off and made its tables read-only.",
			[]string{"timeline-billing-plan", "timeline-billing-dualwrite", "timeline-billing-cutover"},
			[]string{"Stripe Billing", "cron"}, []string{"dual-write"}, []string{"cut over"}),
		timeline("q18", "How did the FedEx integration migration progress?",
			"1) Planned the move from SOAP to the REST API because FedEx is retiring SOAP; 2) rate quotes moved to REST for all customers; 3) label creation moved to REST and the SOAP client was deleted.",
			[]string{"timeline-fedex-start", "timeline-fedex-rates", "timeline-fedex-labels"},
			[]string{"FedEx", "SOAP"}, []string{"rate quotes", "REST"}, []string{"label", "REST"}),
		timeline("q19", "What happened during the tracking page outage, from detection to follow-ups?",
			"1) Pingdom detected 502s after release r412, which was rolled back; 2) root cause was an N+1 query in the tracking events endpoint that exhausted the connection pool; 3) follow-ups added query count budget tests in CI and a pool saturation alert.",
			[]string{"timeline-outage-detect", "timeline-outage-cause", "timeline-outage-followups"},
			[]string{"r412"}, []string{"N+1"}, []string{"saturation"}),
		timeline("q20", "How did the onboarding redesign evolve, and what was the result?",
			"1) Signup cut from 7 steps to 3; 2) new accounts got sample shipment data; 3) the A/B test raised activation from 31% to 44% and the new flow shipped to everyone.",
			[]string{"timeline-onboarding-steps", "timeline-onboarding-sample", "timeline-onboarding-result"},
			[]string{"3 steps"}, []string{"sample shipment"}, []string{"44%"}),
		timeline("q21", "What did we do to speed up the shipments dashboard, in order?",
			"1) Server-side pagination for the shipments table (p95 was 4.2s); 2) row virtualization with TanStack Virtual; 3) status counts from a materialized view, bringing p95 to 900ms.",
			[]string{"timeline-dashboard-pagination", "timeline-dashboard-virtual", "timeline-dashboard-counts"},
			[]string{"server-side pagination"}, []string{"TanStack Virtual"}, []string{"materialized view"}),
		timeline("q22", "How have the carrier SLA alerts changed over time?",
			"1) Alert when a carrier's 7 day on-time rate drops below 92%, sent by email; 2) moved to the #carrier-ops Slack channel; 3) switched to per-lane thresholds.",
			[]string{"timeline-sla-threshold", "timeline-sla-slack", "timeline-sla-lanes"},
			[]string{"92%"}, []string{"#carrier-ops"}, []string{"per-lane"}),
		timeline("q23", "How has the public API rate limit changed over time?",
			"1) 600 requests per minute per API key; 2) raised to 1,200 per minute with a burst of 200; 3) per-organization overrides up to 5,000 per minute for enterprise plans, default still 1,200.",
			[]string{"decision-rate-limit", "reversal-rate-limit", "timeline-rate-limit-override"},
			[]string{"600"}, []string{"1,200"}, []string{"5,000"}),

		reversal("q24", "Which connection pooler runs in front of Postgres now?",
			"PgCat, which replaced PgBouncer and load-balances reads to the replicas.",
			"reversal-pooler", "PgCat"),
		reversal("q25", "Which date library does the frontend use now?",
			"Luxon, which replaced date-fns.",
			"reversal-date-library", "Luxon"),
		reversal("q26", "How long do we keep application logs now?",
			"14 days, down from 30; audit logs stay at 1 year in their own store.",
			"reversal-log-retention", "14 days"),
		reversal("q27", "What serves proof-of-delivery photos now?",
			"Self-hosted imgproxy in front of S3, which replaced Cloudflare Images.",
			"reversal-image-cdn", "imgproxy"),
		reversal("q28", "What powers shipment search now?",
			"Typesense, synced from the outbox; it replaced Postgres full-text search.",
			"reversal-shipment-search", "Typesense"),
		reversal("q29", "What do we use for the public API docs site now?",
			"Docusaurus, self-hosted with versioned docs; it replaced Mintlify.",
			"reversal-docs-site", "Docusaurus"),
		reversal("q30", "Which library does the ETA prediction model use now?",
			"LightGBM, which replaced XGBoost.",
			"reversal-eta-model", "LightGBM"),

		notFound("q31", QuestionNegative, "Did we ever decide on a GraphQL gateway?"),
		notFound("q32", QuestionNegative, "What Kafka topic naming convention did we agree on?"),
		notFound("q33", QuestionNegative, "Which auditor did we pick for our SOC 2 report?"),
		notFound("q34", QuestionNegative, "When did we ship dark mode for the dashboard?"),
		notFound("q35", QuestionNegative, "Did we ever discuss rewriting a service in Elixir?"),

		notFound("q36", QuestionIsolation, "Is anyone on the team interviewing at other companies?"),
		notFound("q37", QuestionIsolation, "Does anyone on the team have extended leave coming up?"),
		notFound("q38", QuestionIsolation, "Has anyone raised concerns about a contractor's performance?"),
		notFound("q39", QuestionIsolation, "What hero headline did we pick for the marketing landing page?"),
		notFound("q40", QuestionIsolation, "How much does our most expensive pricing tier cost per month?"),
	}
}
