// Package email is the application-facing facade for sending transactional
// emails. It defines a strongly typed Template interface so the variables
// passed to a send call are always validated against the template they target.
//
// Adding a new template
//
//  1. Add a new Loops transactional ID constant to this file. This is the
//     single registry of template IDs known to the application.
//  2. In a new file (template_<name>.go), define a struct whose exported
//     fields are exactly the variables the template consumes.
//  3. Implement the Template interface on the struct: TransactionalID returns
//     the constant from step 1, Variables returns the merge data Loops
//     expects, and AddToAudience controls whether Loops should upsert the
//     recipient as a contact when the email is sent.
//  4. Append the zero value of the new template to RegisteredTemplates so
//     unit tests catch a duplicate ID.
package email

// TransactionalID is the opaque identifier Loops assigns to each template.
type TransactionalID string

// Loops transactional template IDs. Keep all IDs declared here so the
// registry is grep-friendly.
const (
	transactionalIDTeamInvite                TransactionalID = "cml3n1h2n27o50i2rakc30bwb"
	transactionalIDEnterpriseAdminOnboarding TransactionalID = "cmpqyxnzl00hj0jwtkibhyjdz"
	transactionalIDAccessRequestCreated      TransactionalID = "cmq12wqp901sl0j0ll8cpr0yj"
	transactionalIDTumUsageThreshold         TransactionalID = "cmrdon75q00390jvq44l87erv"
	transactionalIDTumUsageOverage           TransactionalID = "cmrdopjpd028m0jx0v8sl25wj"
	// TODO(DNO-529): replace both with the real Loops transactional IDs once
	// the "OpenRouter chat credits threshold" and "OpenRouter internal credits
	// threshold" templates are created in the Loops dashboard — merge
	// variables for each: organization_name, threshold_percent, exhausted.
	// Until then each crossed threshold retries its send roughly hourly (the
	// alert activity's backoff reservation), failing fast at Loops and logging
	// "failed to send openrouter credits alert".
	transactionalIDOpenRouterChatCreditsThreshold     TransactionalID = "openrouter_chat_credits_threshold_pending_loops_id"
	transactionalIDOpenRouterInternalCreditsThreshold TransactionalID = "openrouter_internal_credits_threshold_pending_loops_id"
)

// Template is implemented by every concrete email template. Concrete types
// hold the typed variables the template consumes and translate them to the
// Loops merge variable shape via Variables.
type Template interface {
	// TransactionalID returns the Loops template identifier this template
	// instance dispatches against.
	TransactionalID() TransactionalID
	// Variables returns the merge data Loops substitutes into the template.
	// The returned map may be nil for templates with no dynamic content.
	Variables() map[string]string
	// AddToAudience reports whether sending this template should upsert the
	// recipient into the Loops audience.
	AddToAudience() bool
}

// RegisteredTemplates is the canonical list of templates the application is
// allowed to send. Tests assert that every entry maps to a non-empty,
// non-duplicated transactional ID so misconfigured templates fail fast.
//
// The entries hold zero-valued instances of each template type — they are
// only used to look up the template's metadata (TransactionalID,
// AddToAudience), never to render an actual email.
var RegisteredTemplates = []Template{
	TeamInvite{
		InviteLink:       "",
		InviterName:      "",
		InviterEmail:     "",
		OrganizationName: "",
	},
	EnterpriseAdminOnboarding{
		SetupLink: "",
	},
	AccessRequestCreated{
		RequesterEmail: "",
		DisplayName:    "",
		ApprovalURL:    "",
	},
	TumUsageThreshold{
		OrganizationName: "",
		ThresholdPercent: "",
		UsageTokens:      "",
		TokenLimit:       "",
		CycleStart:       "",
		CycleEnd:         "",
	},
	TumUsageOverage{
		OrganizationName: "",
		ThresholdPercent: "",
		UsageTokens:      "",
		TokenLimit:       "",
		OverageTokens:    "",
		CycleStart:       "",
		CycleEnd:         "",
	},
	OpenRouterChatCreditsThreshold{
		OrganizationName: "",
		ThresholdPercent: "",
		Exhausted:        false,
	},
	OpenRouterInternalCreditsThreshold{
		OrganizationName: "",
		ThresholdPercent: "",
		Exhausted:        false,
	},
}
