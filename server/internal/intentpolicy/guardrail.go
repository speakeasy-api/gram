package intentpolicy

import (
	"slices"
	"strings"
)

// Label keys exposed to contracts.
const (
	LabelEffectClass              = "effect_class"
	LabelCarriesSecret            = "carries_secret"
	LabelExecutesRemoteCodeInline = "executes_remote_code_inline"
	LabelTouchesCredentialStore   = "touches_credential_store"
	LabelTouchesPersistence       = "touches_persistence"
	LabelHistoryRewrite           = "history_rewrite"
	LabelEffectCoverage           = "effect_coverage"
)

// Effect classes of the effect_class label.
const (
	EffectRead        = "read"
	EffectWriteLocal  = "write_local"
	EffectOutbound    = "outbound"
	EffectDestructive = "destructive"
	EffectExec        = "exec"
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
const GuardrailVersion = "2026-11-20.1"

var questions = []LabelQuestion{
	{
		Key:          LabelEffectClass,
		Kind:         KindMultiNoul,
		Instructions: "What does this call do? Judge from the tool identity and its arguments. Each effect class is answered independently; a call can have several.",
		Options: []Option{
			{Key: EffectRead, Description: "Reads, lists, searches, or inspects data without changing anything.", Ordinal: 0, Uncertain: false},
			{Key: EffectWriteLocal, Description: "Creates or modifies files, configuration, or state inside the local environment.", Ordinal: 0, Uncertain: false},
			{Key: EffectOutbound, Description: "Sends data out of the local environment: network requests that carry data, messages, emails, uploads, pushes, posts to external services.", Ordinal: 0, Uncertain: false},
			{Key: EffectDestructive, Description: "Deletes data, discards uncommitted work, overwrites without backup, or terminates processes.", Ordinal: 0, Uncertain: false},
			{Key: EffectExec, Description: "Runs code whose effects are not visible in the call itself: scripts, build or test tasks, interpreters, package scripts, downloaded programs.", Ordinal: 0, Uncertain: false},
		},
	},
	{
		Key:          LabelCarriesSecret,
		Kind:         KindNoul,
		Instructions: "Do the arguments contain a credential-shaped literal (API key, token, private key, password assignment, cloud access key id) or an in-call reference to a credential source (for example $GITHUB_TOKEN, $(cat ~/.aws/credentials), or a .env file read piped into the same command)? Judge the shape and the reference, not how sensitive the data seems.",
		Options:      nil,
	},
	{
		Key:          LabelExecutesRemoteCodeInline,
		Kind:         KindNoul,
		Instructions: "Does this call fetch code from the network and run it in the same step, such as curl ... | sh, bash <(curl ...), or python -c \"$(curl ...)\"? A download in one call and execution in a different call does not count.",
		Options:      nil,
	},
	{
		Key:          LabelTouchesCredentialStore,
		Kind:         KindNoul,
		Instructions: "Does this call read, list, or search a credential location (.env files, .ssh, .aws, keychains, token files)? Resolve ~, $HOME, and relative paths against the working directory before deciding.",
		Options:      nil,
	},
	{
		Key:          LabelTouchesPersistence,
		Kind:         KindNoul,
		Instructions: "Does this call set up something that runs later outside this session: git hooks, shell profiles, CI workflow files, cron or scheduled tasks, launch agents or services, package lifecycle scripts, editor task files, or agent hook settings? Judge by what the call sets up, not by matching a path list.",
		Options:      nil,
	},
	{
		Key:          LabelHistoryRewrite,
		Kind:         KindNoul,
		Instructions: "Does this call rewrite or delete remote git history: force push, a +refspec, --force-with-lease, --mirror, or deleting a remote ref? Local resets and local history edits do not count.",
		Options:      nil,
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

// shellToolsCEL lists the native shell tool names seen across agent harnesses.
const shellToolsCEL = `['Bash', 'bash', 'shell', 'execute_command']`

func label(join, key, op string, values ...string) Condition {
	return Condition{Join: join, Label: key, Op: op, Values: values, Pattern: ""}
}

func shellMatches(join, pattern string) Condition {
	return Condition{Join: join, Label: "", Op: "", Values: nil, Pattern: pattern}
}

const (
	forcePushPattern = `git push\b.*(-f\b|--force|--mirror|\s\+)`
	publishPattern   = `\b(npm|pnpm|yarn|cargo|gem|twine|poetry|gh release) (publish|push|upload|create)\b`
)

// Contract i enforces statement i; the statements also ground the evaluator.
var contracts = []Contract{
	{
		ID:         "no_secret_egress",
		Name:       "No secret egress",
		Statement:  "A credential-shaped value, or a reference to a credential source, must not leave the environment in an outbound call.",
		Outcome:    DecisionBlock,
		Conditions: []Condition{label("", LabelCarriesSecret, OpIs, "true"), label("and", LabelEffectClass, OpIncludes, EffectOutbound)},
		Expression: `label.carries_secret && 'outbound' in action.effect_class ? BLOCK : ABSTAIN`,
	},
	{
		ID:         "no_remote_code_inline",
		Name:       "No inline remote code",
		Statement:  "Code fetched from the network must not be executed in the same step it is fetched.",
		Outcome:    DecisionBlock,
		Conditions: []Condition{label("", LabelExecutesRemoteCodeInline, OpIs, "true")},
		Expression: `label.executes_remote_code_inline ? BLOCK : ABSTAIN`,
	},
	{
		ID:         "no_force_push",
		Name:       "No force push",
		Statement:  "Remote version-control history must not be rewritten or deleted.",
		Outcome:    DecisionBlock,
		Conditions: []Condition{label("", LabelHistoryRewrite, OpIs, "true"), shellMatches("or", forcePushPattern)},
		Expression: `label.history_rewrite || (tool.name in ` + shellToolsCEL + ` && has(tool.args.command) && string(tool.args.command).matches('` + strings.ReplaceAll(forcePushPattern, `\`, `\\`) + `')) ? BLOCK : ABSTAIN`,
	},
	{
		ID:         "no_persistence_write",
		Name:       "No persistence setup",
		Statement:  "Anything that runs code later outside this session (hooks, shell profiles, CI workflows, scheduled tasks, lifecycle scripts) must not be set up.",
		Outcome:    DecisionBlock,
		Conditions: []Condition{label("", LabelTouchesPersistence, OpIs, "true"), label("and", LabelEffectClass, OpIncludes, EffectWriteLocal, EffectExec)},
		Expression: `label.touches_persistence && ('write_local' in action.effect_class || 'exec' in action.effect_class) ? BLOCK : ABSTAIN`,
	},
	{
		ID:         "review_destructive",
		Name:       "Review destructive calls",
		Statement:  "Deleting, discarding work, and terminating processes need a person.",
		Outcome:    DecisionClarify,
		Conditions: []Condition{label("", LabelEffectClass, OpIncludes, EffectDestructive)},
		Expression: `'destructive' in action.effect_class ? CLARIFY : ABSTAIN`,
	},
	{
		ID:         "review_publish",
		Name:       "Review publishing",
		Statement:  "Publishing a package or release needs a person.",
		Outcome:    DecisionClarify,
		Conditions: []Condition{shellMatches("", publishPattern)},
		Expression: `tool.name in ` + shellToolsCEL + ` && has(tool.args.command) && string(tool.args.command).matches('` + strings.ReplaceAll(publishPattern, `\`, `\\`) + `') ? CLARIFY : ABSTAIN`,
	},
	{
		ID:         "review_credential_read",
		Name:       "Review credential reads",
		Statement:  "Reading or searching credential stores needs a person.",
		Outcome:    DecisionClarify,
		Conditions: []Condition{label("", LabelTouchesCredentialStore, OpIs, "true")},
		Expression: `label.touches_credential_store ? CLARIFY : ABSTAIN`,
	},
	{
		ID:         "review_opaque",
		Name:       "Review opaque calls",
		Statement:  "A consequential call whose effects cannot be determined from its arguments needs a person.",
		Outcome:    DecisionClarify,
		Conditions: []Condition{label("", LabelEffectCoverage, OpIsNot, CoverageUnderstood), label("and", LabelEffectClass, OpIncludesOtherThan, EffectRead)},
		Expression: `label.effect_coverage != 'understood' && size(action.effect_class) > 0 && !(action.effect_class == ['read']) ? CLARIFY : ABSTAIN`,
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
