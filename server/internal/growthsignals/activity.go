// Package growthsignals turns notable moments in Gram — projects created, MCP
// servers deployed, members joining, security policies written — into a single
// PostHog event that internal Slack destinations render as ops signal.
//
// Everything is reported as one event name with a stable property shape, so
// which moments count as significant is a filter on the PostHog destination
// rather than a deploy. The taxonomy of what happened lives here; the judgement
// of what matters does not.
//
// Nothing in this package sits on a request's critical path. An activity whose
// lookups fail still ships with the properties that did resolve, and an
// activity that cannot be captured is logged and dropped: analytics must never
// fail the work that produced it.
package growthsignals

// EventName is the single PostHog event every activity is captured as. One
// name with a stable property shape keeps destination filters expressible as
// property matches instead of a growing list of event names.
const EventName = "gram_activity"

// Activity is the taxonomy name an event carries in its `activity` property.
// It answers what happened in terms Growth reads, rather than in terms of the
// audit action or table that produced it.
type Activity string

const (
	// ActivityOrganizationCreated is a new Gram organization being provisioned.
	ActivityOrganizationCreated Activity = "organization_created"

	// ActivityUserSignedUp is a person's first Gram user record being created.
	// It carries a signup_source distinguishing an invited arrival from an
	// organic one.
	ActivityUserSignedUp Activity = "user_signed_up"

	// ActivityMemberJoinedOrganization is a pending invitation being accepted.
	// It carries the role the new member was granted.
	ActivityMemberJoinedOrganization Activity = "member_joined_organization"

	// ActivityProjectCreated is a new project inside an organization.
	ActivityProjectCreated Activity = "project_created"

	// ActivityMcpServerCreated is any flavour of MCP server being created. The
	// flavour survives as the mcp_kind property, so the five creation paths
	// read as one moment.
	ActivityMcpServerCreated Activity = "mcp_server_created"

	// ActivitySecurityPolicyCreated is a new risk policy.
	ActivitySecurityPolicyCreated Activity = "security_policy_created"

	// ActivitySecurityPolicyUpdated is a change to an existing risk policy.
	ActivitySecurityPolicyUpdated Activity = "security_policy_updated"

	// ActivityMcpServerUpdated is a change to an MCP server, its metadata, or
	// its tool metadata.
	ActivityMcpServerUpdated Activity = "mcp_server_updated"

	// ActivityMemberInvited is an invitation being sent to join an
	// organization. The join itself is ActivityMemberJoinedOrganization.
	ActivityMemberInvited Activity = "member_invited"

	// ActivityDeviceFirstSeen is a device appearing in an organization's fleet
	// for the first time.
	ActivityDeviceFirstSeen Activity = "device_first_seen"

	// ActivityAgentFirstDetected is an AI agent being attributed to an
	// organization for the first time.
	ActivityAgentFirstDetected Activity = "agent_first_detected"
)

// ActivitySkip marks an audit action that carries no ops value and must not be
// emitted. It is a decision, not a taxonomy name: it never reaches PostHog, and
// the emitter drops any activity carrying it.
//
// Excluding at the source rather than at the destination filter matters because
// the excluded actions are the high-volume ones — an assistant's tool calls
// alone would dwarf everything else on the topic.
const ActivitySkip Activity = "skip"
