package risk_analysis

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/gobwas/glob"
	"github.com/tidwall/gjson"

	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

const SourceCustom = "custom"

// Target names the part of a message a condition reads. The target also
// implicitly scopes the condition to a message type (see targetMessageType): a
// tool_* target only ever matches a tool-request message, user_prompt only a
// user message, and so on. `content` is unscoped (the legacy regex target) and
// reads the message's raw text body.
type Target string

const (
	TargetContent      Target = "content"
	TargetUserPrompt   Target = "user_prompt"
	TargetAssistant    Target = "assistant_text"
	TargetToolResult   Target = "tool_result"
	TargetToolName     Target = "tool_name"
	TargetToolServer   Target = "tool_server"
	TargetToolFunction Target = "tool_function"
	TargetToolArgs     Target = "tool_args"
)

// Op is the comparison a condition applies to its resolved target value.
type Op string

const (
	OpRegex     Op = "regex"
	OpEquals    Op = "equals"
	OpNotEquals Op = "not_equals"
	OpGlob      Op = "glob"
	OpKeyword   Op = "keyword"
	OpExists    Op = "exists"
	// Query-bar operators. contains/not_contains/in are union-capable (match any
	// of Values, falling back to Value). contains/not_contains are
	// case-insensitive substring; in is exact equality; starts_with/ends_with are
	// prefix/suffix.
	OpContains    Op = "contains"
	OpNotContains Op = "not_contains"
	OpStartsWith  Op = "starts_with"
	OpEndsWith    Op = "ends_with"
	OpIn          Op = "in"
)

// MatchCombine controls how a rule's conditions are reduced to a verdict.
type MatchCombine string

const (
	CombineAnd MatchCombine = "and"
	CombineOr  MatchCombine = "or"
)

// Action is a rule's polarity within a policy. A deny rule produces a finding
// when it matches (the default). An allow rule is an exemption: when it matches
// a message it short-circuits the whole policy, dropping every finding for that
// message — even ones other detectors (gitleaks, presidio, the judge) produced.
// The action is determined by how the policy attaches the rule: rules in
// risk_policies.custom_rule_ids deny (detectors); rules in
// risk_policies.exempt_rule_ids allow (exemptions). The same rule can therefore
// deny in one policy and exempt in another; the caller sets Action accordingly.
type Action string

const (
	ActionDeny  Action = "deny"
	ActionAllow Action = "allow"
)

// effectiveAction defaults a blank/unknown action to deny.
func effectiveAction(a Action) Action {
	if a == ActionAllow {
		return ActionAllow
	}
	return ActionDeny
}

// Condition is one target/op/value test, decoded from a rule's match_config.
type Condition struct {
	Target Target `json:"target"`
	Op     Op     `json:"op"`
	// Value is the operand for the single-value ops: regex/equals/not_equals/glob.
	// Empty is meaningful for equals/not_equals — `tool_server == ""` matches
	// native/harness tools. The keyword op uses Values instead.
	Value string `json:"value,omitempty"`
	// Values holds the keywords for the keyword op (case-insensitive substring).
	Values []string `json:"values,omitempty"`
	// Path is a gjson path into the tool_args JSON (target=tool_args only). A
	// leading `$.`/`$` and `[i]` array syntax are normalised to gjson form.
	Path string `json:"path,omitempty"`
	// CaseInsensitive lowercases both sides for equals/not_equals/keyword.
	CaseInsensitive bool `json:"case_insensitive,omitempty"`
}

// MatchConfig is the sparse, self-describing matcher stored in the
// risk_custom_detection_rules.match_config JSONB column. The rule's polarity
// (deny/allow) is not part of the matcher — it is determined by how the policy
// attaches the rule (custom_rule_ids vs exempt_rule_ids) and supplied on
// CustomDetectionRule.Action.
type MatchConfig struct {
	Combine    MatchCombine `json:"combine,omitempty"`
	Conditions []Condition  `json:"conditions"`
}

func (m MatchConfig) combineOrDefault() MatchCombine {
	if m.Combine == CombineOr {
		return CombineOr
	}
	return CombineAnd
}

// CustomDetectionRule is a policy-selected custom rule as loaded from the
// database. MatchConfig is the raw match_config JSONB. Action is the rule's
// polarity within the owning policy (deny by default); the caller resolves it
// from the policy's rules map before compiling. Callers translate a legacy
// regex-column rule into match_config via EffectiveMatchConfig before
// compiling, so the engine is purely condition-driven.
type CustomDetectionRule struct {
	RuleID      string
	Title       string
	Description string
	MatchConfig []byte
	Action      Action
}

// CompiledCustomDetectionRule is a rule with its conditions compiled (regexes,
// globs, normalised gjson paths) ready for repeated evaluation.
type CompiledCustomDetectionRule struct {
	CustomDetectionRule
	action     Action
	combine    MatchCombine
	conditions []compiledCondition
}

type compiledCondition struct {
	target          Target
	op              Op
	value           string
	keywords        []string
	operands        []string // contains/not_contains/in: match any of these
	path            string
	caseInsensitive bool
	re              *regexp.Regexp
	glob            glob.Glob
}

// ToolView is one tool invocation surfaced from a message's recorded tool
// calls. Server/Function are the destructured MCP components (Server is "" for
// native/harness tools); Arguments is the raw arguments JSON.
type ToolView struct {
	Name      string
	Server    string
	Function  string
	Arguments string
}

// MessageView is the structured input the rule engine evaluates conditions
// against. Both scan paths (batch analyzer and realtime scanner) build an
// identical view so a rule behaves the same in either path. Content is the
// message's raw text body; Tools is populated for tool-request messages.
type MessageView struct {
	Content string
	Type    message.Type
	Tools   []ToolView
}

// NewToolView destructures a tool-call name into its MCP server/function
// components (Server is "" for native/harness tools) and pairs them with the
// raw arguments JSON, for one entry of a MessageView's Tools.
func NewToolView(name, arguments string) ToolView {
	return ToolView{
		Name:      name,
		Server:    toolref.MCPServerOf(name),
		Function:  toolref.MCPFunctionOf(name),
		Arguments: arguments,
	}
}

func CompileCustomDetectionRules(rules []CustomDetectionRule) ([]CompiledCustomDetectionRule, error) {
	compiled := make([]CompiledCustomDetectionRule, 0, len(rules))
	for _, rule := range rules {
		if isEmptyJSON(rule.MatchConfig) {
			// No matcher configured yet (e.g. a rule created ahead of its
			// matcher) — nothing to evaluate.
			continue
		}
		cfg, err := parseMatchConfig(rule.MatchConfig)
		if err != nil {
			return nil, fmt.Errorf("custom rule %s: %w", rule.RuleID, err)
		}
		if len(cfg.Conditions) == 0 {
			continue
		}
		conditions, err := compileConditions(cfg.Conditions)
		if err != nil {
			return nil, fmt.Errorf("compile custom rule %s: %w", rule.RuleID, err)
		}
		rule.RuleID = guard(rule.RuleID)
		compiled = append(compiled, CompiledCustomDetectionRule{
			CustomDetectionRule: rule,
			action:              effectiveAction(rule.Action),
			combine:             cfg.combineOrDefault(),
			conditions:          conditions,
		})
	}
	return compiled, nil
}

// ValidateMatchConfig compiles a raw match_config to surface any invalid
// target, op, regex, glob, or missing path before it is persisted. A nil/empty
// config is valid (the rule falls back to its regex column).
func ValidateMatchConfig(raw []byte) error {
	if isEmptyJSON(raw) {
		return nil
	}
	cfg, err := parseMatchConfig(raw)
	if err != nil {
		return err
	}
	if len(cfg.Conditions) == 0 {
		return fmt.Errorf("match_config must declare at least one condition")
	}
	if _, err := compileConditions(cfg.Conditions); err != nil {
		return err
	}
	return nil
}

// EffectiveMatchConfig returns the match_config a rule should evaluate: the
// stored config when present, otherwise a synthesised single content/regex
// condition translating the legacy regex column. Returns nil when neither is
// set. Callers apply this at the DB boundary so the engine stays purely
// condition-driven and the legacy regex column has no special case inside it.
func EffectiveMatchConfig(matchConfig []byte, legacyRegex string) []byte {
	if !isEmptyJSON(matchConfig) {
		return matchConfig
	}
	pattern := strings.TrimSpace(legacyRegex)
	if pattern == "" {
		return nil
	}
	raw, err := json.Marshal(MatchConfig{
		Combine: CombineAnd,
		Conditions: []Condition{{
			Target:          TargetContent,
			Op:              OpRegex,
			Value:           pattern,
			Values:          nil,
			Path:            "",
			CaseInsensitive: false,
		}},
	})
	if err != nil {
		return nil
	}
	return raw
}

func parseMatchConfig(raw []byte) (MatchConfig, error) {
	var cfg MatchConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return MatchConfig{}, fmt.Errorf("parse match_config: %w", err)
	}
	return cfg, nil
}

func compileConditions(conditions []Condition) ([]compiledCondition, error) {
	out := make([]compiledCondition, 0, len(conditions))
	for i, cond := range conditions {
		cc, err := compileCondition(cond)
		if err != nil {
			return nil, fmt.Errorf("condition %d: %w", i, err)
		}
		out = append(out, cc)
	}
	return out, nil
}

func compileCondition(cond Condition) (compiledCondition, error) {
	target := Target(strings.TrimSpace(string(cond.Target)))
	if target == "" {
		target = TargetContent
	}
	if !target.valid() {
		return compiledCondition{}, fmt.Errorf("unknown target %q", target)
	}

	op := Op(strings.TrimSpace(string(cond.Op)))
	if op == "" {
		return compiledCondition{}, fmt.Errorf("condition requires an op")
	}

	cc := compiledCondition{
		target:          target,
		op:              op,
		value:           cond.Value,
		keywords:        nil,
		operands:        nil,
		path:            "",
		caseInsensitive: cond.CaseInsensitive,
		re:              nil,
		glob:            nil,
	}

	// tool_args may carry a gjson path (match the extracted value) or omit it
	// (match the raw arguments JSON).
	if target == TargetToolArgs && strings.TrimSpace(cond.Path) != "" {
		cc.path = normalizeJSONPath(cond.Path)
	}

	switch op {
	case OpRegex:
		re, err := regexp.Compile(cond.Value)
		if err != nil {
			return compiledCondition{}, fmt.Errorf("invalid regex: %w", err)
		}
		cc.re = re
	case OpGlob:
		if strings.TrimSpace(cond.Value) == "" {
			return compiledCondition{}, fmt.Errorf("glob condition requires a value")
		}
		g, err := glob.Compile(cond.Value)
		if err != nil {
			return compiledCondition{}, fmt.Errorf("invalid glob: %w", err)
		}
		cc.glob = g
	case OpKeyword:
		cleaned := make([]string, 0, len(cond.Values))
		for _, kw := range cond.Values {
			kw = strings.TrimSpace(kw)
			if kw == "" {
				continue
			}
			if cond.CaseInsensitive {
				kw = strings.ToLower(kw)
			}
			cleaned = append(cleaned, kw)
		}
		if len(cleaned) == 0 {
			return compiledCondition{}, fmt.Errorf("keyword condition requires at least one keyword in values")
		}
		cc.keywords = cleaned
	case OpContains, OpNotContains:
		operands := conditionOperands(cond)
		if len(operands) == 0 {
			return compiledCondition{}, fmt.Errorf("%s requires a value", op)
		}
		for i := range operands {
			operands[i] = strings.ToLower(operands[i])
		}
		cc.operands = operands
	case OpIn:
		operands := conditionOperands(cond)
		if len(operands) == 0 {
			return compiledCondition{}, fmt.Errorf("in requires a value")
		}
		cc.operands = operands
	case OpStartsWith, OpEndsWith:
		if strings.TrimSpace(cond.Value) == "" {
			return compiledCondition{}, fmt.Errorf("%s requires a value", op)
		}
	case OpEquals, OpNotEquals:
		// Empty value is intentional (e.g. tool_server == "" matches native tools).
	case OpExists:
		// No operand needed.
	default:
		return compiledCondition{}, fmt.Errorf("unknown op %q", op)
	}

	return cc, nil
}

// CustomRuleScan is the outcome of evaluating a message against a policy's
// compiled custom rules.
type CustomRuleScan struct {
	// Findings are matches from deny-action rules.
	Findings []Finding
	// Allowed is true when an allow-action rule matched the message. The caller
	// short-circuits the policy: every finding for this message is dropped,
	// including ones produced by other detectors.
	Allowed bool
}

// ScanCustomDetectionRules evaluates every compiled rule against a single
// message view. Deny rules contribute findings; an allow rule that matches sets
// Allowed so the caller can short-circuit the policy.
func ScanCustomDetectionRules(view MessageView, rules []CompiledCustomDetectionRule) CustomRuleScan {
	out := CustomRuleScan{Findings: nil, Allowed: false}
	for _, rule := range rules {
		if rule.action == ActionAllow {
			if rule.matches(view) {
				out.Allowed = true
			}
			continue
		}
		out.Findings = append(out.Findings, rule.scan(view)...)
	}
	return out
}

// matches reports whether the rule's conditions are satisfied by the view,
// without expanding per-occurrence findings — used for the allowlist check.
func (r CompiledCustomDetectionRule) matches(view MessageView) bool {
	ok, _ := r.evaluate(view)
	return ok
}

func (r CompiledCustomDetectionRule) scan(view MessageView) []Finding {
	// Fast path preserving legacy behaviour: a single content-targeted regex
	// (which every legacy regex rule resolves to) emits one finding per match
	// occurrence, carrying the matched span — so existing rules and their
	// positions are unchanged.
	if len(r.conditions) == 1 {
		c := r.conditions[0]
		if c.op == OpRegex && c.target.isText() {
			if !c.appliesTo(view.Type) {
				return nil
			}
			return r.regexSpanFindings(c, view.Content)
		}
	}

	matched, matchValue := r.evaluate(view)
	if !matched {
		return nil
	}
	f := r.newFinding()
	f.Match = matchValue
	return []Finding{f}
}

func (r CompiledCustomDetectionRule) evaluate(view MessageView) (bool, string) {
	if r.combine == CombineOr {
		for _, c := range r.conditions {
			if !c.appliesTo(view.Type) {
				continue
			}
			if ok, mv := c.eval(view); ok {
				return true, mv
			}
		}
		return false, ""
	}

	// AND: every condition must apply to this message type and match.
	var firstMatch string
	for _, c := range r.conditions {
		if !c.appliesTo(view.Type) {
			return false, ""
		}
		ok, mv := c.eval(view)
		if !ok {
			return false, ""
		}
		if firstMatch == "" {
			firstMatch = mv
		}
	}
	return true, firstMatch
}

func (r CompiledCustomDetectionRule) regexSpanFindings(c compiledCondition, text string) []Finding {
	matches := c.re.FindAllStringIndex(text, -1)
	findings := make([]Finding, 0, len(matches))
	for _, m := range matches {
		f := r.newFinding()
		f.Match = text[m[0]:m[1]]
		f.StartPos = m[0]
		f.EndPos = m[1]
		findings = append(findings, f)
	}
	return findings
}

func (r CompiledCustomDetectionRule) newFinding() Finding {
	return Finding{
		Source:           SourceCustom,
		RuleID:           r.RuleID,
		Description:      customRuleDescription(r),
		Match:            "",
		StartPos:         0,
		EndPos:           0,
		Tags:             nil,
		Confidence:       1.0,
		DeadLetterReason: "",
		toolCallID:       "",
	}
}

// appliesTo reports whether the condition's target can be read from a message
// of the given type. A content-targeted condition applies to any type.
func (c compiledCondition) appliesTo(messageType message.Type) bool {
	req, scoped := targetMessageType(c.target)
	return !scoped || req == messageType
}

func (c compiledCondition) eval(view MessageView) (bool, string) {
	switch c.target {
	case TargetContent, TargetUserPrompt, TargetAssistant, TargetToolResult:
		return c.match(view.Content)
	case TargetToolName:
		return c.matchAnyTool(view.Tools, func(t ToolView) string { return t.Name })
	case TargetToolServer:
		return c.matchAnyTool(view.Tools, func(t ToolView) string { return t.Server })
	case TargetToolFunction:
		return c.matchAnyTool(view.Tools, func(t ToolView) string { return t.Function })
	case TargetToolArgs:
		for _, t := range view.Tools {
			if ok, mv := c.matchArgs(t.Arguments); ok {
				return true, mv
			}
		}
		return false, ""
	default:
		return false, ""
	}
}

func (c compiledCondition) matchAnyTool(tools []ToolView, pick func(ToolView) string) (bool, string) {
	for _, t := range tools {
		if ok, mv := c.match(pick(t)); ok {
			return true, mv
		}
	}
	return false, ""
}

func (c compiledCondition) matchArgs(argsJSON string) (bool, string) {
	// No path: match against the raw arguments JSON (e.g. tool_call.args:contains:rm).
	if c.path == "" {
		if c.op == OpExists {
			if strings.TrimSpace(argsJSON) != "" {
				return true, argsJSON
			}
			return false, ""
		}
		return c.match(argsJSON)
	}
	res := gjson.Get(argsJSON, c.path)
	if c.op == OpExists {
		if res.Exists() {
			return true, res.String()
		}
		return false, ""
	}
	if !res.Exists() {
		return false, ""
	}
	return c.match(res.String())
}

func (c compiledCondition) match(s string) (bool, string) {
	switch c.op {
	case OpRegex:
		if loc := c.re.FindStringIndex(s); loc != nil {
			return true, s[loc[0]:loc[1]]
		}
		return false, ""
	case OpEquals:
		if foldEqual(s, c.value, c.caseInsensitive) {
			return true, s
		}
		return false, ""
	case OpNotEquals:
		if !foldEqual(s, c.value, c.caseInsensitive) {
			return true, s
		}
		return false, ""
	case OpGlob:
		if c.glob.Match(s) {
			return true, s
		}
		return false, ""
	case OpKeyword:
		hay := s
		if c.caseInsensitive {
			hay = strings.ToLower(s)
		}
		for _, kw := range c.keywords {
			if strings.Contains(hay, kw) {
				return true, kw
			}
		}
		return false, ""
	case OpContains:
		hay := strings.ToLower(s)
		for _, op := range c.operands {
			if strings.Contains(hay, op) {
				return true, op
			}
		}
		return false, ""
	case OpNotContains:
		hay := strings.ToLower(s)
		for _, op := range c.operands {
			if strings.Contains(hay, op) {
				return false, ""
			}
		}
		return true, s
	case OpIn:
		if slices.Contains(c.operands, s) {
			return true, s
		}
		return false, ""
	case OpStartsWith:
		if strings.HasPrefix(s, c.value) {
			return true, s
		}
		return false, ""
	case OpEndsWith:
		if strings.HasSuffix(s, c.value) {
			return true, s
		}
		return false, ""
	case OpExists:
		if s != "" {
			return true, s
		}
		return false, ""
	default:
		return false, ""
	}
}

// conditionOperands collects a condition's operand list: Values if present,
// else a single-element list of Value. Blank entries are dropped.
func conditionOperands(cond Condition) []string {
	vals := cond.Values
	if len(vals) == 0 && cond.Value != "" {
		vals = []string{cond.Value}
	}
	cleaned := make([]string, 0, len(vals))
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			cleaned = append(cleaned, v)
		}
	}
	return cleaned
}

func foldEqual(a, b string, caseInsensitive bool) bool {
	if caseInsensitive {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func (t Target) valid() bool {
	switch t {
	case TargetContent, TargetUserPrompt, TargetAssistant, TargetToolResult,
		TargetToolName, TargetToolServer, TargetToolFunction, TargetToolArgs:
		return true
	default:
		return false
	}
}

func (t Target) isText() bool {
	switch t {
	case TargetContent, TargetUserPrompt, TargetAssistant, TargetToolResult:
		return true
	default:
		return false
	}
}

// targetMessageType returns the message type a target is scoped to, and whether
// it is scoped at all. `content` is unscoped (matches any message type).
func targetMessageType(t Target) (message.Type, bool) {
	switch t {
	case TargetUserPrompt:
		return message.User, true
	case TargetAssistant:
		return message.Assistant, true
	case TargetToolResult:
		return message.ToolResponse, true
	case TargetToolName, TargetToolServer, TargetToolFunction, TargetToolArgs:
		return message.ToolRequest, true
	default:
		return "", false
	}
}

// normalizeJSONPath converts a JSONPath-ish expression to gjson path syntax:
// strips a leading `$`/`$.`, turns `[i]` array access into `.i`, and trims
// stray separators. `$` alone selects the whole document (`@this`).
func normalizeJSONPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "$")
	p = strings.TrimPrefix(p, ".")
	p = strings.ReplaceAll(p, "[", ".")
	p = strings.ReplaceAll(p, "]", "")
	p = strings.Trim(p, ".")
	if p == "" {
		return "@this"
	}
	return p
}

func isEmptyJSON(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

func customRuleDescription(rule CompiledCustomDetectionRule) string {
	if strings.TrimSpace(rule.Description) != "" {
		return rule.Description
	}
	if strings.TrimSpace(rule.Title) != "" {
		return rule.Title
	}
	return "Custom detection rule match"
}
