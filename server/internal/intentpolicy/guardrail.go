package intentpolicy

import "slices"

// Label keys exposed to contracts.
const (
	LabelEffectClass     = "effect_class"
	LabelTargetLocations = "target_locations"
	LabelPayloadData     = "payload_data"
	LabelExecCodeOrigin  = "exec_code_origin"
	LabelAudience        = "audience"
	LabelEffectCoverage  = "effect_coverage"
)

// Values of the effect_class label.
const (
	EffectRead        = "read"
	EffectWrite       = "write"
	EffectOutbound    = "outbound"
	EffectDestructive = "destructive"
	EffectExec        = "exec"
)

// Values of the target_locations label.
const (
	LocationScratch         = "scratch"
	LocationWorkspace       = "workspace"
	LocationSystem          = "system"
	LocationCredentialStore = "credential_store" //nolint:gosec // G101: label option key, not a credential
	LocationPersistence     = "persistence"
	LocationRemote          = "remote"
)

// Values of the payload_data label.
const (
	PayloadCredential = "credential"
	PayloadPersonal   = "personal"
)

// Values of the exec_code_origin label.
const (
	CodeOriginNone        = "none"
	CodeOriginProjectTask = "project_task"
	CodeOriginLocalScript = "local_script"
	CodeOriginInline      = "inline"
	CodeOriginNetwork     = "network"
	CodeOriginUnknown     = "unknown"
)

// Values of the audience label.
const (
	AudienceNone    = "none"
	AudienceDirect  = "direct"
	AudienceGroup   = "group"
	AudienceBroad   = "broad"
	AudiencePublic  = "public"
	AudienceUnknown = "unknown"
)

// Values of the effect_coverage label.
const (
	CoverageUnderstood          = "understood"
	CoveragePartiallyUnderstood = "partially_understood"
	CoverageUnknown             = "unknown"
)

// GuardrailID identifies the built-in intent guardrail.
const GuardrailID = "intent-baseline"

// GuardrailVersion changes whenever statements, questions, or contracts change.
const GuardrailVersion = "2026-11-25.1"

var questions = []LabelQuestion{
	{
		Key:          LabelEffectClass,
		Kind:         KindMultiNoul,
		Instructions: "What does this call do to the world? Judge from the tool identity and its arguments. Each effect class is answered independently; a call can have several.",
		Options: []Option{
			{Key: EffectRead, Description: "Reads, lists, searches, or inspects data without changing anything.", Ordinal: 0, Uncertain: false},
			{Key: EffectWrite, Description: "Creates or modifies files, configuration, or state.", Ordinal: 0, Uncertain: false},
			{Key: EffectOutbound, Description: "Sends data out of the local environment: network requests that carry data, messages, emails, uploads, pushes, posts to external services.", Ordinal: 0, Uncertain: false},
			{Key: EffectDestructive, Description: "Deletes data, discards uncommitted work, overwrites without backup, or terminates processes.", Ordinal: 0, Uncertain: false},
			{Key: EffectExec, Description: "Executes code, scripts, tasks, or programs regardless of origin.", Ordinal: 0, Uncertain: false},
		},
	},
	{
		Key:          LabelTargetLocations,
		Kind:         KindMultiNoul,
		Instructions: "What classes of location does this call read, write, delete, search, or send to? Resolve ~, $HOME, and relative paths against the working directory before deciding. Each location class is answered independently; a call can target several.",
		Options: []Option{
			{Key: LocationScratch, Description: "Temporary directories, build outputs, caches, and generated artifacts: /tmp, dist, build, node_modules, .cache, coverage.", Ordinal: 0, Uncertain: false},
			{Key: LocationWorkspace, Description: "Source, docs, configuration, or uncommitted work in the project.", Ordinal: 0, Uncertain: false},
			{Key: LocationSystem, Description: "The host outside the workspace: home dotfiles, /etc, other projects.", Ordinal: 0, Uncertain: false},
			{Key: LocationCredentialStore, Description: "Credential locations (.env, .ssh, .aws, keychains, token files), whether read, searched, piped, substituted, or uploaded.", Ordinal: 0, Uncertain: false},
			{Key: LocationPersistence, Description: "Anything that runs later outside this session: git hooks, shell profiles, CI workflows, cron, launch agents, lifecycle scripts, editor tasks, agent hook settings.", Ordinal: 0, Uncertain: false},
			{Key: LocationRemote, Description: "Anything off this machine: hosts, services, remote repos and refs, databases, other users' data.", Ordinal: 0, Uncertain: false},
		},
	},
	{
		Key:          LabelPayloadData,
		Kind:         KindMultiNoul,
		Instructions: "What classes of sensitive data are in the arguments? Judge the shape of the value, not whether sharing seems acceptable. Paths do not count.",
		Options: []Option{
			{Key: PayloadCredential, Description: "A credential-shaped literal (API key, token, private key, password assignment, cloud access key id) or a credential-named environment variable expansion ($GITHUB_TOKEN).", Ordinal: 0, Uncertain: false},
			{Key: PayloadPersonal, Description: "A person's name with an email, phone, or address; payment card digits; account or customer ids tied to a person; government ids; health or employment details.", Ordinal: 0, Uncertain: false},
		},
	},
	{
		Key:          LabelExecCodeOrigin,
		Kind:         KindChoice,
		Instructions: "For a call that executes code, where did the code come from? Answer none when the call does not execute code.",
		Options: []Option{
			{Key: CodeOriginNone, Description: "No execution.", Ordinal: 0, Uncertain: false},
			{Key: CodeOriginProjectTask, Description: "A task the project declares through its build or package tool (npm run, make, cargo test, pytest, mise run).", Ordinal: 0, Uncertain: false},
			{Key: CodeOriginLocalScript, Description: "A file in the workspace or on the host.", Ordinal: 0, Uncertain: false},
			{Key: CodeOriginInline, Description: "Code given literally in the arguments (python -c, bash -c, eval).", Ordinal: 0, Uncertain: false},
			{Key: CodeOriginNetwork, Description: "Fetched from the network and run in the same step (curl ... | sh, bash <(curl ...)).", Ordinal: 0, Uncertain: false},
			{Key: CodeOriginUnknown, Description: "The origin of the executed code cannot be determined from the arguments.", Ordinal: 0, Uncertain: true},
		},
	},
	{
		Key:          LabelAudience,
		Kind:         KindChoice,
		Instructions: "For an outbound call, who can see what it sends? Judge from channel names, visibility hints, registry, and recipient lists. Do not judge internal versus external to the organization here. Answer none when the call is not outbound.",
		Options: []Option{
			{Key: AudienceNone, Description: "Not outbound.", Ordinal: 0, Uncertain: false},
			{Key: AudienceDirect, Description: "Named individual recipients or a single private destination.", Ordinal: 0, Uncertain: false},
			{Key: AudienceGroup, Description: "A named team channel, repo, or list.", Ordinal: 0, Uncertain: false},
			{Key: AudienceBroad, Description: "Organization-wide channels (#all-staff, #general) or all-hands lists.", Ordinal: 0, Uncertain: false},
			{Key: AudiencePublic, Description: "Public registries, releases, public repos and issues, public sites, app stores.", Ordinal: 0, Uncertain: false},
			{Key: AudienceUnknown, Description: "The audience cannot be determined from the arguments.", Ordinal: 0, Uncertain: true},
		},
	},
	{
		Key:          LabelEffectCoverage,
		Kind:         KindChoice,
		Instructions: "How completely can the effects and targets of this call be determined from its arguments alone? Opaque scripts, project tasks whose contents are not shown, wildcards, broad globs, and unknown tools lower coverage.",
		Options: []Option{
			{Key: CoverageUnderstood, Description: "Every effect and target is determinable from the arguments.", Ordinal: 2, Uncertain: false},
			{Key: CoveragePartiallyUnderstood, Description: "Some effects or targets are determinable, others are hidden or open-ended.", Ordinal: 1, Uncertain: true},
			{Key: CoverageUnknown, Description: "The effects cannot be determined from the arguments.", Ordinal: 0, Uncertain: true},
		},
	},
}

func label(join, key, op string, values ...string) Condition {
	return Condition{Join: join, Label: key, Op: op, Values: values}
}

// Contract i enforces statement i; the statements also ground the evaluator.
var contracts = []Contract{
	{
		ID:        "no_credential_egress",
		Name:      "No credential egress",
		Statement: "Credentials, by value or by source location, must not leave the environment in an outbound call.",
		Outcome:   DecisionBlock,
		Conditions: []Condition{
			label("", LabelPayloadData, OpIncludes, PayloadCredential),
			label("or", LabelTargetLocations, OpIncludes, LocationCredentialStore),
			label("and", LabelEffectClass, OpIncludes, EffectOutbound),
		},
		Expression: `('credential' in label.payload_data || 'credential_store' in label.target_locations) && 'outbound' in action.effect_class ? BLOCK : ABSTAIN`,
	},
	{
		ID:        "no_network_code",
		Name:      "No network code",
		Statement: "Code obtained from the network must not be executed.",
		Outcome:   DecisionBlock,
		Conditions: []Condition{
			label("", LabelEffectClass, OpIncludes, EffectExec),
			label("and", LabelExecCodeOrigin, OpIs, CodeOriginNetwork),
		},
		Expression: `'exec' in action.effect_class && label.exec_code_origin == 'network' ? BLOCK : ABSTAIN`,
	},
	{
		ID:        "no_remote_destruction",
		Name:      "No remote destruction",
		Statement: "Remote or shared state must not be deleted, rewritten, or discarded.",
		Outcome:   DecisionBlock,
		Conditions: []Condition{
			label("", LabelEffectClass, OpIncludes, EffectDestructive),
			label("and", LabelTargetLocations, OpIncludes, LocationRemote),
		},
		Expression: `'destructive' in action.effect_class && 'remote' in label.target_locations ? BLOCK : ABSTAIN`,
	},
	{
		ID:        "no_persistence_setup",
		Name:      "No persistence setup",
		Statement: "Anything that runs code later outside this session must not be set up.",
		Outcome:   DecisionBlock,
		Conditions: []Condition{
			label("", LabelTargetLocations, OpIncludes, LocationPersistence),
			label("and", LabelEffectClass, OpIncludes, EffectWrite, EffectExec),
		},
		Expression: `'persistence' in label.target_locations && ('write' in action.effect_class || 'exec' in action.effect_class) ? BLOCK : ABSTAIN`,
	},
	{
		ID:        "no_personal_broadcast",
		Name:      "No personal broadcast",
		Statement: "Personal or customer data must not be sent to a broad or public audience.",
		Outcome:   DecisionBlock,
		Conditions: []Condition{
			label("", LabelPayloadData, OpIncludes, PayloadPersonal),
			label("and", LabelEffectClass, OpIncludes, EffectOutbound),
			label("and", LabelAudience, OpIs, AudienceBroad, AudiencePublic),
		},
		Expression: `'personal' in label.payload_data && 'outbound' in action.effect_class && label.audience in ['broad', 'public'] ? BLOCK : ABSTAIN`,
	},
	{
		ID:        "review_personal_egress",
		Name:      "Review personal egress",
		Statement: "Personal or customer data leaving the environment needs a person.",
		Outcome:   DecisionClarify,
		Conditions: []Condition{
			label("", LabelPayloadData, OpIncludes, PayloadPersonal),
			label("and", LabelEffectClass, OpIncludes, EffectOutbound),
		},
		Expression: `'personal' in label.payload_data && 'outbound' in action.effect_class ? CLARIFY : ABSTAIN`,
	},
	{
		ID:        "review_public_release",
		Name:      "Review public release",
		Statement: "Anything made public needs a person.",
		Outcome:   DecisionClarify,
		Conditions: []Condition{
			label("", LabelEffectClass, OpIncludes, EffectOutbound),
			label("and", LabelAudience, OpIs, AudiencePublic),
		},
		Expression: `'outbound' in action.effect_class && label.audience == 'public' ? CLARIFY : ABSTAIN`,
	},
	{
		ID:        "review_destruction",
		Name:      "Review destruction",
		Statement: "Deleting, discarding, or terminating anything beyond scratch output needs a person.",
		Outcome:   DecisionClarify,
		Conditions: []Condition{
			label("", LabelEffectClass, OpIncludes, EffectDestructive),
			label("and", LabelTargetLocations, OpIncludesOtherThan, LocationScratch),
		},
		Expression: `'destructive' in action.effect_class && !(size(label.target_locations) > 0 && label.target_locations.all(t, t == 'scratch')) ? CLARIFY : ABSTAIN`,
	},
	{
		ID:        "review_credential_access",
		Name:      "Review credential access",
		Statement: "Accessing a credential store needs a person.",
		Outcome:   DecisionClarify,
		Conditions: []Condition{
			label("", LabelTargetLocations, OpIncludes, LocationCredentialStore),
		},
		Expression: `'credential_store' in label.target_locations ? CLARIFY : ABSTAIN`,
	},
	{
		ID:        "review_opaque",
		Name:      "Review opaque calls",
		Statement: "A consequential call whose effects cannot be determined from its arguments needs a person; reads and declared project tasks are exempt.",
		Outcome:   DecisionClarify,
		Conditions: []Condition{
			label("", LabelEffectCoverage, OpIsNot, CoverageUnderstood),
			label("and", LabelEffectClass, OpIncludesOtherThan, EffectRead),
			label("and", LabelExecCodeOrigin, OpIsNot, CodeOriginProjectTask),
		},
		Expression: `label.effect_coverage != 'understood' && action.effect_class.exists(e, e != 'read' && !(e == 'exec' && label.exec_code_origin == 'project_task')) ? CLARIFY : ABSTAIN`,
	},
}

// DefaultGuardrail returns a copy of the built-in guardrail.
func DefaultGuardrail() Guardrail {
	qs := make([]LabelQuestion, len(questions))
	for i, q := range questions {
		q.Options = slices.Clone(q.Options)
		qs[i] = q
	}
	cs := make([]Contract, len(contracts))
	for i, c := range contracts {
		c.Conditions = slices.Clone(c.Conditions)
		for j := range c.Conditions {
			c.Conditions[j].Values = slices.Clone(c.Conditions[j].Values)
		}
		cs[i] = c
	}
	return Guardrail{
		ID:         GuardrailID,
		Version:    GuardrailVersion,
		Statements: statements(),
		Questions:  qs,
		Contracts:  cs,
	}
}

// statements lists each contract's statement, in contract order.
func statements() []string {
	out := make([]string, len(contracts))
	for i, c := range contracts {
		out[i] = c.Statement
	}
	return out
}
