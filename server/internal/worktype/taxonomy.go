// Package worktype defines the preset work-type taxonomy used to
// auto-categorize agent session turns (DNO-540).
//
// The taxonomy is a closed, versioned, two-level hierarchy: top-level
// categories are org functions a CEO/CFO would slice AI spend by, and child
// categories are the activities within them. The LLM judge classifies each
// turn as exactly one classifiable (leaf) category; parents exist only for
// rollups. Keeping the set closed is the consistency mechanism — the judge can
// never invent near-duplicate labels like "Code Review" vs "PR Review".
//
// Customers cannot modify the taxonomy. Custom, customer-authored labels are a
// separate future concern and live in a different namespace.
package worktype

import "fmt"

// Version identifies the current taxonomy revision. Bump it whenever a
// category is added, removed, renamed, or has its meaning changed so stored
// labels can be told apart from labels produced by earlier revisions and
// re-classified if needed.
const Version = 1

// Key is the stable identifier of a taxonomy category. Child keys are
// namespaced under their parent as "<parent>.<child>". Keys are persisted with
// labels and must never be renamed within a taxonomy version.
type Key string

// Top-level category keys.
const (
	KeyEngineering       Key = "engineering"
	KeyDataAnalytics     Key = "data_analytics"
	KeyProductDesign     Key = "product_design"
	KeySalesMarketing    Key = "sales_marketing"
	KeyCustomerSupport   Key = "customer_support"
	KeyOperationsAdmin   Key = "operations_admin"
	KeyKnowledgeResearch Key = "knowledge_research"
	KeyPersonal          Key = "personal"
	KeyOther             Key = "other"
)

// Engineering child category keys.
const (
	KeyEngineeringFeatureDevelopment Key = "engineering.feature_development"
	KeyEngineeringBugFixing          Key = "engineering.bug_fixing"
	KeyEngineeringCodeReview         Key = "engineering.code_review"
	KeyEngineeringTestingQA          Key = "engineering.testing_qa"
	KeyEngineeringRefactoring        Key = "engineering.refactoring"
	KeyEngineeringDevOpsInfra        Key = "engineering.devops_infra"
	KeyEngineeringIncidentResponse   Key = "engineering.incident_response"
	KeyEngineeringDocumentation      Key = "engineering.documentation"
	KeyEngineeringCodebaseQA         Key = "engineering.codebase_qa"
)

// Data & analytics child category keys.
const (
	KeyDataAnalyticsAnalysisReporting Key = "data_analytics.analysis_reporting"
	KeyDataAnalyticsQueriesDashboards Key = "data_analytics.queries_dashboards"
)

// Product & design child category keys.
const (
	KeyProductDesignSpecsPlanning Key = "product_design.specs_planning"
	KeyProductDesignMocks         Key = "product_design.mocks"
)

// Sales & marketing child category keys.
const (
	KeySalesMarketingCollateral Key = "sales_marketing.collateral"
	KeySalesMarketingContent    Key = "sales_marketing.content"
)

// Customer support child category keys.
const (
	KeyCustomerSupportResponses Key = "customer_support.responses"
)

// Operations & admin child category keys.
const (
	KeyOperationsAdminMeetingsComms   Key = "operations_admin.meetings_comms"
	KeyOperationsAdminRecruitingHR    Key = "operations_admin.recruiting_hr"
	KeyOperationsAdminLegalCompliance Key = "operations_admin.legal_compliance"
)

// Knowledge & research child category keys.
const (
	KeyKnowledgeResearchInformationLookup Key = "knowledge_research.information_lookup"
	KeyKnowledgeResearchLearning          Key = "knowledge_research.learning"
)

// Category is one node of the taxonomy.
type Category struct {
	// Key is the stable, persisted identifier of the category.
	Key Key

	// Parent is the key of the top-level category this one belongs to. It is
	// empty for top-level categories.
	Parent Key

	// DisplayName is the human-facing name shown in dashboards and pickers.
	DisplayName string

	// JudgeGuidance describes, with concrete transcript signals, when the LLM
	// judge should pick this category. It is empty for parent categories,
	// which are never picked directly.
	JudgeGuidance string
}

// registry is the single source of truth for the taxonomy, in display order.
// Top-level parents precede their children; Personal and Other sort last.
var registry = []Category{
	{
		Key:           KeyEngineering,
		Parent:        "",
		DisplayName:   "Engineering",
		JudgeGuidance: "",
	},
	{
		Key:         KeyEngineeringFeatureDevelopment,
		Parent:      KeyEngineering,
		DisplayName: "Feature Development",
		JudgeGuidance: "Writing new product code: implementing features, endpoints, UI, " +
			"migrations, or new modules. Signals: requests to build or add functionality, " +
			"new files or functions being created, iterating on freshly written code.",
	},
	{
		Key:         KeyEngineeringBugFixing,
		Parent:      KeyEngineering,
		DisplayName: "Bug Fixing & Debugging",
		JudgeGuidance: "Diagnosing and fixing defects in existing behavior. Signals: error " +
			"messages or stack traces pasted in, reproduction steps, 'why is X broken', " +
			"patches that correct rather than add behavior.",
	},
	{
		Key:         KeyEngineeringCodeReview,
		Parent:      KeyEngineering,
		DisplayName: "Code Review",
		JudgeGuidance: "Reviewing changes someone else (or an agent) authored. Signals: " +
			"mentions of a PR, diff, or review comments; asking for critique, risks, or " +
			"approval of a change rather than authoring it.",
	},
	{
		Key:         KeyEngineeringTestingQA,
		Parent:      KeyEngineering,
		DisplayName: "Testing & QA",
		JudgeGuidance: "Writing or running tests and verifying behavior. Signals: test " +
			"files, coverage, flaky-test hunts, QA passes, 'write tests for', asserting " +
			"expected vs actual behavior without changing product code.",
	},
	{
		Key:         KeyEngineeringRefactoring,
		Parent:      KeyEngineering,
		DisplayName: "Refactoring & Tech Debt",
		JudgeGuidance: "Restructuring code without changing behavior. Signals: renames, " +
			"extractions, dead-code removal, dependency upgrades, lint cleanups, 'simplify' " +
			"or 'clean up' requests.",
	},
	{
		Key:         KeyEngineeringDevOpsInfra,
		Parent:      KeyEngineering,
		DisplayName: "DevOps, Infra & Releases",
		JudgeGuidance: "CI/CD, builds, deployments, releases, infrastructure-as-code, " +
			"cloud configuration. Signals: pipeline configs, Dockerfiles, Terraform or " +
			"Kubernetes manifests, release cuts, environment or secret management.",
	},
	{
		Key:         KeyEngineeringIncidentResponse,
		Parent:      KeyEngineering,
		DisplayName: "Incident Response",
		JudgeGuidance: "Urgent investigation of production problems. Signals: outages, " +
			"alerts, on-call context, log and metric spelunking under time pressure, " +
			"incident timelines and mitigations.",
	},
	{
		Key:         KeyEngineeringDocumentation,
		Parent:      KeyEngineering,
		DisplayName: "Technical Documentation",
		JudgeGuidance: "Writing docs for technical audiences: READMEs, API references, " +
			"runbooks, architecture notes, code comments requested as the primary output.",
	},
	{
		Key:         KeyEngineeringCodebaseQA,
		Parent:      KeyEngineering,
		DisplayName: "Codebase Q&A & Onboarding",
		JudgeGuidance: "Understanding existing code without changing it. Signals: 'how " +
			"does X work', 'where is Y implemented', tracing flows, explaining modules to " +
			"a newcomer. No code output beyond illustrative snippets.",
	},
	{
		Key:           KeyDataAnalytics,
		Parent:        "",
		DisplayName:   "Data & Analytics",
		JudgeGuidance: "",
	},
	{
		Key:         KeyDataAnalyticsAnalysisReporting,
		Parent:      KeyDataAnalytics,
		DisplayName: "Analysis & Reporting",
		JudgeGuidance: "Analyzing datasets and producing findings or reports. Signals: " +
			"CSVs or exports being interrogated, statistical questions, summaries of " +
			"metrics or trends intended for a business audience.",
	},
	{
		Key:         KeyDataAnalyticsQueriesDashboards,
		Parent:      KeyDataAnalytics,
		DisplayName: "Queries & Dashboards",
		JudgeGuidance: "Authoring queries or dashboards as the deliverable. Signals: SQL " +
			"being written or tuned, BI tool configuration, chart or dashboard building.",
	},
	{
		Key:           KeyProductDesign,
		Parent:        "",
		DisplayName:   "Product & Design",
		JudgeGuidance: "",
	},
	{
		Key:         KeyProductDesignSpecsPlanning,
		Parent:      KeyProductDesign,
		DisplayName: "Specs & Planning",
		JudgeGuidance: "Product specs, requirements, roadmaps, and ticket writing. " +
			"Signals: PRDs, user stories, acceptance criteria, project or sprint planning, " +
			"breaking work into tasks.",
	},
	{
		Key:         KeyProductDesignMocks,
		Parent:      KeyProductDesign,
		DisplayName: "Design Mocks & Prototypes",
		JudgeGuidance: "Visual design and prototyping. Signals: mockups, wireframes, " +
			"design-tool workflows, HTML/CSS prototypes whose purpose is exploring look " +
			"and feel rather than shipping product code.",
	},
	{
		Key:           KeySalesMarketing,
		Parent:        "",
		DisplayName:   "Sales & Marketing",
		JudgeGuidance: "",
	},
	{
		Key:         KeySalesMarketingCollateral,
		Parent:      KeySalesMarketing,
		DisplayName: "Sales Collateral & Outreach",
		JudgeGuidance: "Material aimed at winning specific customers. Signals: proposals, " +
			"pitch decks, outreach emails, RFP responses, account research, call prep.",
	},
	{
		Key:         KeySalesMarketingContent,
		Parent:      KeySalesMarketing,
		DisplayName: "Marketing Content",
		JudgeGuidance: "Material aimed at a broad audience. Signals: blog posts, landing " +
			"pages, social copy, newsletters, SEO work, campaign planning.",
	},
	{
		Key:           KeyCustomerSupport,
		Parent:        "",
		DisplayName:   "Customer Support",
		JudgeGuidance: "",
	},
	{
		Key:         KeyCustomerSupportResponses,
		Parent:      KeyCustomerSupport,
		DisplayName: "Support Responses & Triage",
		JudgeGuidance: "Helping specific customers with problems. Signals: support " +
			"tickets, drafting replies to customer issues, triaging or escalating reported " +
			"problems, troubleshooting on a customer's behalf.",
	},
	{
		Key:           KeyOperationsAdmin,
		Parent:        "",
		DisplayName:   "Operations & Admin",
		JudgeGuidance: "",
	},
	{
		Key:         KeyOperationsAdminMeetingsComms,
		Parent:      KeyOperationsAdmin,
		DisplayName: "Meetings, Notes & Comms",
		JudgeGuidance: "Internal communication overhead. Signals: meeting notes or " +
			"summaries, agendas, status updates, internal announcements, Slack or email " +
			"drafting for colleagues.",
	},
	{
		Key:         KeyOperationsAdminRecruitingHR,
		Parent:      KeyOperationsAdmin,
		DisplayName: "Recruiting & HR",
		JudgeGuidance: "People operations. Signals: job descriptions, candidate or resume " +
			"review, interview questions and feedback, onboarding plans, HR policy " +
			"questions.",
	},
	{
		Key:         KeyOperationsAdminLegalCompliance,
		Parent:      KeyOperationsAdmin,
		DisplayName: "Legal, Finance & Compliance",
		JudgeGuidance: "Corporate obligations. Signals: contract review, privacy or " +
			"compliance questions, invoices, budgeting and expense work, vendor and " +
			"procurement paperwork.",
	},
	{
		Key:           KeyKnowledgeResearch,
		Parent:        "",
		DisplayName:   "Knowledge & Research",
		JudgeGuidance: "",
	},
	{
		Key:         KeyKnowledgeResearchInformationLookup,
		Parent:      KeyKnowledgeResearch,
		DisplayName: "Information Lookup",
		JudgeGuidance: "Quick factual questions with short answers. Signals: 'what is', " +
			"'how do I', single-shot lookups of tools, APIs, or general knowledge where " +
			"the session ends once the fact is delivered.",
	},
	{
		Key:         KeyKnowledgeResearchLearning,
		Parent:      KeyKnowledgeResearch,
		DisplayName: "Research & Learning",
		JudgeGuidance: "Deeper investigation or study. Signals: comparing technologies or " +
			"vendors, literature or market research, tutorials, multi-turn exploration of " +
			"a topic to build understanding rather than to produce an artifact.",
	},
	{
		Key:         KeyPersonal,
		Parent:      "",
		DisplayName: "Personal / Non-work",
		JudgeGuidance: "Tasks with no plausible connection to the organization's work. " +
			"Signals: personal travel, shopping, hobbies, homework, side projects, " +
			"personal finances or correspondence.",
	},
	{
		Key:         KeyOther,
		Parent:      "",
		DisplayName: "Other / Uncategorized",
		JudgeGuidance: "The turn is work-related but no other category fits, or the " +
			"transcript carries too little signal to decide. Prefer this over guessing.",
	},
}

var byKey = func() map[Key]Category {
	m := make(map[Key]Category, len(registry))
	for _, c := range registry {
		if _, ok := m[c.Key]; ok {
			panic(fmt.Sprintf("worktype: duplicate taxonomy key %q", c.Key))
		}
		m[c.Key] = c
	}
	return m
}()

// All returns every category, parents included, in display order.
func All() []Category {
	out := make([]Category, len(registry))
	copy(out, registry)
	return out
}

// Get returns the category for key, reporting whether it exists.
func Get(key Key) (Category, bool) {
	c, ok := byKey[key]
	return c, ok
}

// Classifiable returns the categories the LLM judge may assign to a turn, in
// display order: every child category plus the childless top-level ones
// (Personal, Other). Parent categories are excluded — they are rollups, not
// labels.
func Classifiable() []Category {
	hasChildren := make(map[Key]bool, len(registry))
	for _, c := range registry {
		if c.Parent != "" {
			hasChildren[c.Parent] = true
		}
	}
	out := make([]Category, 0, len(registry))
	for _, c := range registry {
		if !hasChildren[c.Key] {
			out = append(out, c)
		}
	}
	return out
}

// TopLevel resolves key to its top-level ancestor, reporting whether the key
// exists. Top-level keys resolve to themselves.
func TopLevel(key Key) (Key, bool) {
	c, ok := byKey[key]
	if !ok {
		return "", false
	}
	if c.Parent != "" {
		return c.Parent, true
	}
	return c.Key, true
}
