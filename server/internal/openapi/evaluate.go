package openapi

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
)

// EvaluationSeverity ranks how likely a change is to break an MCP server
// that already serves tools from the document.
type EvaluationSeverity string

const (
	// EvaluationSeverityBreaking marks changes that stop working tool calls,
	// such as a removed tool or a credential type that is no longer accepted.
	EvaluationSeverityBreaking EvaluationSeverity = "breaking"

	// EvaluationSeverityWarning marks changes that keep tools callable but
	// need attention, such as new required inputs or renamed environment
	// variables.
	EvaluationSeverityWarning EvaluationSeverity = "warning"

	// EvaluationSeverityInfo marks additive or descriptive changes.
	EvaluationSeverityInfo EvaluationSeverity = "info"
)

// EvaluationCategory groups changes by the part of the MCP they affect.
type EvaluationCategory string

const (
	// EvaluationCategorySecurity covers security schemes and the credentials
	// tools send.
	EvaluationCategorySecurity EvaluationCategory = "security"

	// EvaluationCategoryTools covers the tools generated from operations.
	EvaluationCategoryTools EvaluationCategory = "tools"

	// EvaluationCategoryServer covers the base URL tools call.
	EvaluationCategoryServer EvaluationCategory = "server"
)

// maxEvaluationDetails bounds the detail lines on one change so a large
// document cannot produce an unbounded response. Fifty lines is more than a
// reviewer reads before acting. Tool names are never capped: callers use them
// to count the tools each MCP server loses, and the document size already
// bounds them.
const maxEvaluationDetails = 50

// evaluationPlaceholderID stands in for the project, deployment and document
// ids that tool extraction requires. Evaluation never writes rows, so the
// value is never stored.
var evaluationPlaceholderID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// noEvaluationChange is returned alongside false by comparisons that find no
// change.
var noEvaluationChange = EvaluationChange{
	Severity: "",
	Category: "",
	Code:     "",
	Title:    "",
	Details:  nil,
	Tools:    nil,
}

// ErrDeployedDocumentUnreadable reports that the deployed version of a
// document could not be extracted. The fault lies with the stored document,
// not with the new version being evaluated.
var ErrDeployedDocumentUnreadable = errors.New("extract deployed document")

// EvaluationChange is one finding from comparing two versions of a document.
type EvaluationChange struct {
	// Severity ranks the impact of the change on existing MCP servers.
	Severity EvaluationSeverity

	// Category names the part of the MCP the change affects.
	Category EvaluationCategory

	// Code is a stable identifier for the kind of change.
	Code string

	// Title is a one-line summary of the change.
	Title string

	// Details holds supporting lines, such as before and after values.
	Details []string

	// Tools lists the names of the tools the change affects.
	Tools []string
}

// EvaluationSecurityScheme describes one security scheme in a document.
type EvaluationSecurityScheme struct {
	// Key is the scheme's name under components.securitySchemes.
	Key string

	// Type is the OpenAPI security scheme type.
	Type string

	// Summary is a short human-readable description of the scheme.
	Summary string

	// EnvVariables are the environment variables an MCP server uses to
	// supply credentials for the scheme.
	EnvVariables []string
}

// DocumentSnapshot summarizes one version of a document.
type DocumentSnapshot struct {
	// ToolCount is the number of tools the document produces.
	ToolCount int

	// SkippedOperationCount is the number of operations that could not be
	// turned into tools.
	SkippedOperationCount int

	// SecuritySchemes lists the supported security schemes.
	SecuritySchemes []EvaluationSecurityScheme

	// DefaultSecurity renders the document-level security requirement.
	DefaultSecurity string

	// ServerURL is the default server URL tools call, if any.
	ServerURL *string
}

// DocumentEvaluation is the result of comparing a new version of a document
// with the version currently deployed.
type DocumentEvaluation struct {
	// Base summarizes the deployed version. It is nil for a new source.
	Base *DocumentSnapshot

	// Next summarizes the new version.
	Next DocumentSnapshot

	// Changes lists every finding, most severe first.
	Changes []EvaluationChange
}

// EvaluateDocumentChange compares two versions of an OpenAPI document using
// the same extraction that deployments run, so the findings describe the
// tools, inputs and credentials an MCP server will actually expose. A nil
// base evaluates a new source.
func EvaluateDocumentChange(
	ctx context.Context,
	tracer trace.Tracer,
	docInfo *types.OpenAPIv3DeploymentAsset,
	base []byte,
	next []byte,
) (*DocumentEvaluation, error) {
	// Extraction logs one line per skipped operation. Those are deployment
	// events, not server logs, so evaluation discards them.
	logger := slog.New(slog.DiscardHandler)
	task := ToolExtractorTask{
		Parser:             "speakeasy",
		ProjectID:          evaluationPlaceholderID,
		DeploymentID:       evaluationPlaceholderID,
		DocumentID:         evaluationPlaceholderID,
		DocInfo:            docInfo,
		DocURL:             nil,
		ProjectSlug:        "",
		OrgSlug:            "",
		OnOperationSkipped: nil,
	}

	nextDoc, err := extractDocumentSpeakeasy(ctx, logger, tracer, next, task)
	if err != nil {
		return nil, fmt.Errorf("extract new document: %w", err)
	}

	eval := &DocumentEvaluation{
		Base:    nil,
		Next:    snapshotDocument(nextDoc),
		Changes: nil,
	}

	if base == nil {
		eval.Changes = skippedOperationChanges(nil, nextDoc)
		sortChanges(eval.Changes)
		return eval, nil
	}

	baseDoc, err := extractDocumentSpeakeasy(ctx, logger, tracer, base, task)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDeployedDocumentUnreadable, err)
	}

	baseSnapshot := snapshotDocument(baseDoc)
	eval.Base = &baseSnapshot

	var changes []EvaluationChange
	changes = append(changes, securitySchemeChanges(baseDoc, nextDoc)...)
	changes = append(changes, defaultSecurityChanges(baseDoc, nextDoc)...)
	changes = append(changes, serverChanges(baseDoc, nextDoc)...)
	changes = append(changes, toolChanges(baseDoc, nextDoc)...)
	changes = append(changes, skippedOperationChanges(baseDoc, nextDoc)...)
	sortChanges(changes)
	eval.Changes = changes

	return eval, nil
}

func snapshotDocument(doc *extractedDocumentSpeakeasy) DocumentSnapshot {
	var toolCount, skipped int
	for _, op := range doc.operations {
		if op.def != nil {
			toolCount++
		} else {
			skipped++
		}
	}

	schemes := make([]EvaluationSecurityScheme, 0, len(doc.securitySchemes))
	for _, key := range slices.Sorted(maps.Keys(doc.securitySchemes)) {
		scheme := doc.securitySchemes[key]
		schemes = append(schemes, EvaluationSecurityScheme{
			Key:          key,
			Type:         scheme.Type.String,
			Summary:      describeSecurityScheme(scheme),
			EnvVariables: slices.Clone(scheme.EnvVariables),
		})
	}

	return DocumentSnapshot{
		ToolCount:             toolCount,
		SkippedOperationCount: skipped,
		SecuritySchemes:       schemes,
		DefaultSecurity:       renderSecurity(parseSecurity(doc.globalSecurity)),
		ServerURL:             doc.defaultServerURL,
	}
}

func describeSecurityScheme(scheme *repo.CreateHTTPSecurityParams) string {
	switch scheme.Type.String {
	case "apiKey":
		return fmt.Sprintf("API key in %s `%s`", scheme.InPlacement.String, scheme.Name.String)
	case "http":
		switch scheme.Scheme.String {
		case "bearer":
			return "HTTP bearer token"
		case "basic":
			return "HTTP basic auth"
		default:
			return fmt.Sprintf("HTTP %s", scheme.Scheme.String)
		}
	case "oauth2":
		return fmt.Sprintf("OAuth 2.0 (%s)", strings.Join(scheme.OauthTypes, ", "))
	case "openIdConnect":
		return "OpenID Connect"
	default:
		return scheme.Type.String
	}
}

// securityRequirement is one alternative in an OpenAPI security requirement
// list: every scheme in it must be satisfied, keyed by scheme name with the
// required scopes as values.
type securityRequirement = map[string][]string

func parseSecurity(raw []byte) []securityRequirement {
	if len(raw) == 0 {
		return nil
	}

	var reqs []securityRequirement
	if err := json.Unmarshal(raw, &reqs); err != nil {
		return nil
	}

	return reqs
}

// securityAlternativeKey identifies an alternative by its scheme names alone,
// so scope changes within an alternative can be reported separately.
func securityAlternativeKey(req securityRequirement) string {
	return strings.Join(slices.Sorted(maps.Keys(req)), " + ")
}

func renderSecurityAlternative(req securityRequirement) string {
	if len(req) == 0 {
		return "anonymous"
	}

	parts := make([]string, 0, len(req))
	for _, name := range slices.Sorted(maps.Keys(req)) {
		scopes := req[name]
		if len(scopes) == 0 {
			parts = append(parts, fmt.Sprintf("`%s`", name))
			continue
		}

		sorted := slices.Sorted(slices.Values(scopes))
		parts = append(parts, fmt.Sprintf("`%s` (%s)", name, strings.Join(sorted, ", ")))
	}

	return strings.Join(parts, " + ")
}

func renderSecurity(reqs []securityRequirement) string {
	if len(reqs) == 0 {
		return "no authentication"
	}

	alts := make([]string, 0, len(reqs))
	for _, req := range reqs {
		alts = append(alts, renderSecurityAlternative(req))
	}
	slices.Sort(alts)

	return strings.Join(alts, " or ")
}

func securitySchemeChanges(base, next *extractedDocumentSpeakeasy) []EvaluationChange {
	var changes []EvaluationChange

	keys := slices.Sorted(maps.Keys(base.securitySchemes))
	for _, key := range slices.Sorted(maps.Keys(next.securitySchemes)) {
		if _, ok := base.securitySchemes[key]; !ok {
			keys = append(keys, key)
		}
	}

	for _, key := range keys {
		before, inBase := base.securitySchemes[key]
		after, inNext := next.securitySchemes[key]

		switch {
		case inBase && !inNext:
			users := toolsUsingScheme(base, key)
			details := []string{
				describeSecurityScheme(before),
				"Used by " + countTools(len(users)) + " in the deployed version",
			}
			if len(before.EnvVariables) > 0 {
				details = append(details, "No longer used: "+strings.Join(before.EnvVariables, ", "))
			}
			changes = append(changes, newChange(
				EvaluationSeverityBreaking,
				EvaluationCategorySecurity,
				"security_scheme_removed",
				fmt.Sprintf("Security scheme `%s` was removed", key),
				details,
				users,
			))
		case !inBase && inNext:
			details := []string{describeSecurityScheme(after)}
			if len(after.EnvVariables) > 0 {
				details = append(details, "New environment variables: "+strings.Join(after.EnvVariables, ", "))
			}
			changes = append(changes, newChange(
				EvaluationSeverityInfo,
				EvaluationCategorySecurity,
				"security_scheme_added",
				fmt.Sprintf("Security scheme `%s` was added", key),
				details,
				toolsUsingScheme(next, key),
			))
		default:
			changes = append(changes, compareSecurityScheme(key, before, after, next)...)
		}
	}

	baseErrs := make(map[string]struct{}, len(base.securitySchemeErrors))
	for _, err := range base.securitySchemeErrors {
		baseErrs[securitySchemeErrorKey(err)] = struct{}{}
	}

	var unsupported []string
	for _, err := range next.securitySchemeErrors {
		if _, ok := baseErrs[securitySchemeErrorKey(err)]; !ok {
			unsupported = append(unsupported, err.Error())
		}
	}
	if len(unsupported) > 0 {
		changes = append(changes, newChange(
			EvaluationSeverityWarning,
			EvaluationCategorySecurity,
			"security_scheme_unsupported",
			fmt.Sprintf("%d security schemes cannot be used by tools", len(unsupported)),
			unsupported,
			nil,
		))
	}

	return changes
}

// securitySchemeErrorKey returns the scheme name an extraction error is
// about. Errors are formatted as "<key> (<line>:<col>) <message>", and line
// numbers shift between versions, so only the key is compared.
func securitySchemeErrorKey(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, " ("); i > 0 {
		return msg[:i]
	}
	return msg
}

func compareSecurityScheme(key string, before, after *repo.CreateHTTPSecurityParams, next *extractedDocumentSpeakeasy) []EvaluationChange {
	var changes []EvaluationChange

	var shape []string
	if before.Type.String != after.Type.String {
		shape = append(shape, fmt.Sprintf("Type: %s → %s", before.Type.String, after.Type.String))
	}
	if before.Scheme.String != after.Scheme.String {
		shape = append(shape, fmt.Sprintf("HTTP scheme: %s → %s", conv.Default(before.Scheme.String, "none"), conv.Default(after.Scheme.String, "none")))
	}
	if before.InPlacement.String != after.InPlacement.String || before.Name.String != after.Name.String {
		shape = append(shape, fmt.Sprintf("API key location: %s `%s` → %s `%s`", before.InPlacement.String, before.Name.String, after.InPlacement.String, after.Name.String))
	}
	if len(shape) > 0 {
		changes = append(changes, newChange(
			EvaluationSeverityBreaking,
			EvaluationCategorySecurity,
			"security_scheme_changed",
			fmt.Sprintf("Security scheme `%s` changed how credentials are sent", key),
			shape,
			toolsUsingScheme(next, key),
		))
	}

	if flowChange, ok := compareOAuthFlows(key, before.OauthFlows, after.OauthFlows); ok {
		flowChange.Tools = toolsUsingScheme(next, key)
		changes = append(changes, flowChange)
	}

	added, removed := diffStrings(before.EnvVariables, after.EnvVariables)
	if len(added) > 0 || len(removed) > 0 {
		var details []string
		if len(added) > 0 {
			details = append(details, "Set in MCP server environments: "+strings.Join(added, ", "))
		}
		if len(removed) > 0 {
			details = append(details, "No longer used: "+strings.Join(removed, ", "))
		}
		changes = append(changes, newChange(
			EvaluationSeverityWarning,
			EvaluationCategorySecurity,
			"security_env_vars_changed",
			fmt.Sprintf("Environment variables for `%s` changed", key),
			details,
			toolsUsingScheme(next, key),
		))
	}

	return changes
}

// oauthFlow mirrors an OpenAPI OAuth Flow Object as serialized by the
// extractor.
type oauthFlow struct {
	// AuthorizationURL is the authorization endpoint for the flow.
	AuthorizationURL string `json:"authorizationUrl"`

	// TokenURL is the token endpoint for the flow.
	TokenURL string `json:"tokenUrl"`

	// RefreshURL is the endpoint for refreshing tokens.
	RefreshURL string `json:"refreshUrl"`

	// Scopes maps each available scope to its description.
	Scopes map[string]string `json:"scopes"`
}

func compareOAuthFlows(key string, beforeRaw, afterRaw []byte) (EvaluationChange, bool) {
	if bytes.Equal(beforeRaw, afterRaw) {
		return noEvaluationChange, false
	}

	var before, after map[string]oauthFlow
	_ = json.Unmarshal(beforeRaw, &before)
	_ = json.Unmarshal(afterRaw, &after)

	severity := EvaluationSeverityInfo
	var details []string
	raise := func(s EvaluationSeverity) {
		if severityRank(s) < severityRank(severity) {
			severity = s
		}
	}

	names := slices.Sorted(maps.Keys(before))
	for _, name := range slices.Sorted(maps.Keys(after)) {
		if _, ok := before[name]; !ok {
			names = append(names, name)
		}
	}

	for _, name := range names {
		b, inBefore := before[name]
		a, inAfter := after[name]
		switch {
		case inBefore && !inAfter:
			raise(EvaluationSeverityBreaking)
			details = append(details, fmt.Sprintf("Flow `%s` removed", name))
		case !inBefore && inAfter:
			details = append(details, fmt.Sprintf("Flow `%s` added", name))
		default:
			for _, u := range []struct{ label, before, after string }{
				{"authorization URL", b.AuthorizationURL, a.AuthorizationURL},
				{"token URL", b.TokenURL, a.TokenURL},
				{"refresh URL", b.RefreshURL, a.RefreshURL},
			} {
				if u.before != u.after {
					raise(EvaluationSeverityWarning)
					details = append(details, fmt.Sprintf("`%s` %s: %s → %s", name, u.label, conv.Default(u.before, "none"), conv.Default(u.after, "none")))
				}
			}

			added, removed := diffStrings(slices.Collect(maps.Keys(b.Scopes)), slices.Collect(maps.Keys(a.Scopes)))
			if len(removed) > 0 {
				raise(EvaluationSeverityWarning)
				details = append(details, fmt.Sprintf("`%s` scopes removed: %s", name, strings.Join(removed, ", ")))
			}
			if len(added) > 0 {
				details = append(details, fmt.Sprintf("`%s` scopes added: %s", name, strings.Join(added, ", ")))
			}
		}
	}

	if len(details) == 0 {
		return noEvaluationChange, false
	}

	return newChange(
		severity,
		EvaluationCategorySecurity,
		"oauth_flows_changed",
		fmt.Sprintf("OAuth flows for `%s` changed", key),
		details,
		nil,
	), true
}

func defaultSecurityChanges(base, next *extractedDocumentSpeakeasy) []EvaluationChange {
	before := renderSecurity(parseSecurity(base.globalSecurity))
	after := renderSecurity(parseSecurity(next.globalSecurity))
	if before == after {
		return nil
	}

	return []EvaluationChange{newChange(
		EvaluationSeverityWarning,
		EvaluationCategorySecurity,
		"default_security_changed",
		"Default security requirement changed",
		[]string{"Before: " + before, "After: " + after, "Applies to every operation that does not declare its own security"},
		nil,
	)}
}

func serverChanges(base, next *extractedDocumentSpeakeasy) []EvaluationChange {
	before := conv.PtrValOr(base.defaultServerURL, "")
	after := conv.PtrValOr(next.defaultServerURL, "")
	if before == after {
		return nil
	}

	return []EvaluationChange{newChange(
		EvaluationSeverityWarning,
		EvaluationCategoryServer,
		"server_url_changed",
		"Default server URL changed",
		[]string{
			"Before: " + conv.Default(before, "none"),
			"After: " + conv.Default(after, "none"),
			fmt.Sprintf("MCP server environments that set %s keep using their own URL", next.serverEnvVar),
		},
		nil,
	)}
}

func operationKey(method, path string) string {
	return strings.ToUpper(method) + " " + path
}

func toolChanges(base, next *extractedDocumentSpeakeasy) []EvaluationChange {
	baseTools := make(map[string]*repo.CreateOpenAPIv3ToolDefinitionParams)
	var baseOrder []string
	for _, op := range base.operations {
		if op.def != nil {
			key := operationKey(op.method, op.path)
			baseTools[key] = op.def
			baseOrder = append(baseOrder, key)
		}
	}

	nextTools := make(map[string]*repo.CreateOpenAPIv3ToolDefinitionParams)
	nextErrors := make(map[string]error)
	var nextOrder []string
	for _, op := range next.operations {
		key := operationKey(op.method, op.path)
		if op.def != nil {
			nextTools[key] = op.def
			nextOrder = append(nextOrder, key)
		} else {
			nextErrors[key] = op.err
		}
	}

	var removed, removedDetails, invalid, invalidDetails []string
	for _, key := range baseOrder {
		if _, ok := nextTools[key]; ok {
			continue
		}

		def := baseTools[key]
		if err, ok := nextErrors[key]; ok {
			invalid = append(invalid, def.Name)
			invalidDetails = append(invalidDetails, fmt.Sprintf("%s (%s): %s", def.Name, key, err.Error()))
			continue
		}

		removed = append(removed, def.Name)
		removedDetails = append(removedDetails, fmt.Sprintf("%s (%s)", def.Name, key))
	}

	var added, addedDetails []string
	var renamed, renamedDetails []string
	var descriptions, settings, settingsDetails []string
	var inputWarnings, inputWarningTools, inputInfo, inputInfoTools []string
	transitions := make(map[[2]string]*securityTransition)
	var transitionOrder [][2]string

	for _, key := range nextOrder {
		after := nextTools[key]
		before, ok := baseTools[key]
		if !ok {
			added = append(added, after.Name)
			addedDetails = append(addedDetails, fmt.Sprintf("%s (%s)", after.Name, key))
			continue
		}

		if before.Name != after.Name {
			// MCP servers hold the deployed name, so report that one: it is
			// the tool they lose.
			renamed = append(renamed, before.Name)
			renamedDetails = append(renamedDetails, fmt.Sprintf("%s → %s (%s)", before.Name, after.Name, key))
		}

		beforeSec := renderSecurity(parseSecurity(before.Security))
		afterSec := renderSecurity(parseSecurity(after.Security))
		if beforeSec != afterSec {
			pair := [2]string{beforeSec, afterSec}
			t, ok := transitions[pair]
			if !ok {
				t = &securityTransition{
					before: parseSecurity(before.Security),
					after:  parseSecurity(after.Security),
					tools:  nil,
				}
				transitions[pair] = t
				transitionOrder = append(transitionOrder, pair)
			}
			t.tools = append(t.tools, after.Name)
		}

		if before.Description != after.Description || before.Summary != after.Summary {
			descriptions = append(descriptions, after.Name)
		}

		if diffs := compareToolSettings(before, after); len(diffs) > 0 {
			settings = append(settings, after.Name)
			for _, d := range diffs {
				settingsDetails = append(settingsDetails, fmt.Sprintf("%s: %s", after.Name, d))
			}
		}

		warnings, info := compareToolInputs(before.Schema, after.Schema)
		if len(warnings) > 0 {
			inputWarningTools = append(inputWarningTools, after.Name)
			for _, w := range warnings {
				inputWarnings = append(inputWarnings, fmt.Sprintf("%s: %s", after.Name, w))
			}
		}
		if len(info) > 0 {
			inputInfoTools = append(inputInfoTools, after.Name)
			for _, i := range info {
				inputInfo = append(inputInfo, fmt.Sprintf("%s: %s", after.Name, i))
			}
		}
	}

	var changes []EvaluationChange

	for _, pair := range transitionOrder {
		changes = append(changes, transitions[pair].change(pair))
	}

	if len(removed) > 0 {
		changes = append(changes, newChange(
			EvaluationSeverityBreaking,
			EvaluationCategoryTools,
			"tool_removed",
			countTools(len(removed))+" removed",
			append([]string{"Removed or deprecated in the new version. MCP servers that include these tools will stop serving them."}, removedDetails...),
			removed,
		))
	}

	if len(invalid) > 0 {
		changes = append(changes, newChange(
			EvaluationSeverityBreaking,
			EvaluationCategoryTools,
			"tool_now_invalid",
			countTools(len(invalid))+" can no longer be generated",
			invalidDetails,
			invalid,
		))
	}

	if len(renamed) > 0 {
		changes = append(changes, newChange(
			EvaluationSeverityBreaking,
			EvaluationCategoryTools,
			"tool_renamed",
			countTools(len(renamed))+" renamed",
			append([]string{"MCP servers reference tools by name, so renamed tools drop out of servers that include them."}, renamedDetails...),
			renamed,
		))
	}

	if len(inputWarnings) > 0 {
		changes = append(changes, newChange(
			EvaluationSeverityWarning,
			EvaluationCategoryTools,
			"tool_inputs_changed",
			"Inputs changed on "+countTools(len(inputWarningTools)),
			inputWarnings,
			inputWarningTools,
		))
	}

	if len(added) > 0 {
		changes = append(changes, newChange(
			EvaluationSeverityInfo,
			EvaluationCategoryTools,
			"tool_added",
			countTools(len(added))+" added",
			addedDetails,
			added,
		))
	}

	if len(inputInfo) > 0 {
		changes = append(changes, newChange(
			EvaluationSeverityInfo,
			EvaluationCategoryTools,
			"tool_input_schema_changed",
			"Input schemas updated on "+countTools(len(inputInfoTools)),
			inputInfo,
			inputInfoTools,
		))
	}

	if len(descriptions) > 0 {
		changes = append(changes, newChange(
			EvaluationSeverityInfo,
			EvaluationCategoryTools,
			"tool_description_changed",
			"Descriptions changed on "+countTools(len(descriptions)),
			nil,
			descriptions,
		))
	}

	if len(settings) > 0 {
		changes = append(changes, newChange(
			EvaluationSeverityInfo,
			EvaluationCategoryTools,
			"tool_settings_changed",
			"Settings changed on "+countTools(len(settings)),
			settingsDetails,
			settings,
		))
	}

	return changes
}

// securityTransition collects the tools whose effective security moved from
// one requirement to another.
type securityTransition struct {
	before []securityRequirement
	after  []securityRequirement
	tools  []string
}

func (t *securityTransition) change(pair [2]string) EvaluationChange {
	severity := EvaluationSeverityInfo
	details := []string{"Before: " + pair[0], "After: " + pair[1]}

	beforeAlts := make(map[string]securityRequirement, len(t.before))
	for _, req := range t.before {
		beforeAlts[securityAlternativeKey(req)] = req
	}
	afterAlts := make(map[string]securityRequirement, len(t.after))
	for _, req := range t.after {
		afterAlts[securityAlternativeKey(req)] = req
	}

	switch {
	case len(t.before) == 0 && slices.ContainsFunc(t.after, func(req securityRequirement) bool { return len(req) == 0 }):
		details = append(details, "Credentials are now accepted but still optional, so existing calls keep working.")
	case len(t.before) == 0:
		severity = EvaluationSeverityBreaking
		details = append(details, "These tools did not need credentials before. Calls fail until the MCP server environment supplies them.")
	case len(t.after) == 0:
		severity = EvaluationSeverityWarning
		details = append(details, "These tools will stop sending credentials to the API.")
	default:
		var dropped, gained []string
		for _, key := range slices.Sorted(maps.Keys(beforeAlts)) {
			if _, ok := afterAlts[key]; !ok {
				dropped = append(dropped, renderSecurityAlternative(beforeAlts[key]))
			}
		}
		for _, key := range slices.Sorted(maps.Keys(afterAlts)) {
			if _, ok := beforeAlts[key]; !ok {
				gained = append(gained, renderSecurityAlternative(afterAlts[key]))
			}
		}

		if len(dropped) > 0 {
			severity = EvaluationSeverityBreaking
			details = append(details, "No longer accepted: "+strings.Join(dropped, ", ")+". MCP servers that authenticate this way will fail.")
		}
		if len(gained) > 0 {
			details = append(details, "Newly accepted: "+strings.Join(gained, ", "))
		}

		for _, key := range slices.Sorted(maps.Keys(beforeAlts)) {
			b := beforeAlts[key]
			a, ok := afterAlts[key]
			if ok && renderSecurityAlternative(a) != renderSecurityAlternative(b) {
				if severity != EvaluationSeverityBreaking {
					severity = EvaluationSeverityWarning
				}
				details = append(details, fmt.Sprintf("Scopes changed: %s → %s", renderSecurityAlternative(b), renderSecurityAlternative(a)))
			}
		}
	}

	return newChange(
		severity,
		EvaluationCategorySecurity,
		"tool_security_changed",
		"Security changed on "+countTools(len(t.tools)),
		details,
		t.tools,
	)
}

func compareToolSettings(before, after *repo.CreateOpenAPIv3ToolDefinitionParams) []string {
	var diffs []string
	if before.Confirm.String != after.Confirm.String {
		diffs = append(diffs, fmt.Sprintf("confirmation %s → %s", conv.Default(before.Confirm.String, "default"), conv.Default(after.Confirm.String, "default")))
	}
	if before.RequestContentType.String != after.RequestContentType.String {
		diffs = append(diffs, fmt.Sprintf("request content type %s → %s", conv.Default(before.RequestContentType.String, "none"), conv.Default(after.RequestContentType.String, "none")))
	}
	if !reflect.DeepEqual(before.ResponseFilter, after.ResponseFilter) {
		diffs = append(diffs, "response filter changed")
	}
	return diffs
}

// toolInputSchema is the subset of a generated tool input schema that the
// evaluation compares.
type toolInputSchema struct {
	// Properties holds the input groups: pathParameters, queryParameters,
	// headerParameters and body.
	Properties map[string]json.RawMessage `json:"properties"`

	// Required lists the input groups the tool requires.
	Required []string `json:"required"`

	// Defs holds schemas that the input groups reference.
	Defs map[string]json.RawMessage `json:"$defs"`
}

// inputObjectSchema is the subset of an object schema used to compare the
// fields inside one input group.
type inputObjectSchema struct {
	// Properties holds the object's fields.
	Properties map[string]json.RawMessage `json:"properties"`

	// Required lists the object's required fields.
	Required []string `json:"required"`
}

var inputGroupLabels = map[string]string{
	"pathParameters":   "path parameter",
	"queryParameters":  "query parameter",
	"headerParameters": "header",
	"body":             "body field",
}

// compareToolInputs reports differences between two generated tool input
// schemas. Warnings are changes an agent calling the tool must adapt to; info
// lines are other schema changes.
func compareToolInputs(beforeRaw, afterRaw []byte) (warnings, info []string) {
	if bytes.Equal(beforeRaw, afterRaw) {
		return nil, nil
	}

	var before, after toolInputSchema
	if err := json.Unmarshal(conv.DefaultSlice(beforeRaw, []byte("{}")), &before); err != nil {
		return nil, []string{"input schema changed"}
	}
	if err := json.Unmarshal(conv.DefaultSlice(afterRaw, []byte("{}")), &after); err != nil {
		return nil, []string{"input schema changed"}
	}

	for _, group := range []string{"pathParameters", "queryParameters", "headerParameters", "body"} {
		b, inBefore := before.Properties[group]
		a, inAfter := after.Properties[group]
		label := inputGroupLabels[group]

		switch {
		case !inBefore && !inAfter:
			continue
		case group == "body" && inBefore && !inAfter:
			warnings = append(warnings, "request body removed")
			continue
		case group == "body" && !inBefore && inAfter:
			if slices.Contains(after.Required, "body") {
				warnings = append(warnings, "required request body added")
			} else {
				info = append(info, "optional request body added")
			}
			continue
		}

		if group == "body" && slices.Contains(after.Required, "body") && !slices.Contains(before.Required, "body") {
			warnings = append(warnings, "request body is now required")
		}

		// Inputs often point at shared schemas under $defs, so a change to a
		// shared schema leaves the input itself untouched. Compare the
		// schemas with every reference resolved.
		bInlined, bOK := inlineDefs(b, before.Defs)
		aInlined, aOK := inlineDefs(a, after.Defs)
		if !bOK || !aOK {
			// Comparing a resolved schema with an unresolved one would report
			// every field as added or removed.
			if !jsonEqual(b, a) || !defsEqual(before.Defs, after.Defs) {
				warnings = append(warnings, "shared input schema too large to compare in detail")
			}
			continue
		}

		bObj := parseInputObject(bInlined)
		aObj := parseInputObject(aInlined)

		for _, name := range slices.Sorted(maps.Keys(bObj.Properties)) {
			if _, ok := aObj.Properties[name]; !ok {
				warnings = append(warnings, fmt.Sprintf("%s `%s` removed", label, name))
			}
		}

		for _, name := range slices.Sorted(maps.Keys(aObj.Properties)) {
			aProp := aObj.Properties[name]
			bProp, ok := bObj.Properties[name]
			requiredAfter := slices.Contains(aObj.Required, name)
			requiredBefore := slices.Contains(bObj.Required, name)

			switch {
			case !ok && requiredAfter:
				warnings = append(warnings, fmt.Sprintf("required %s `%s` added", label, name))
			case !ok:
				info = append(info, fmt.Sprintf("optional %s `%s` added", label, name))
			case requiredAfter && !requiredBefore:
				warnings = append(warnings, fmt.Sprintf("%s `%s` is now required", label, name))
			case schemaType(bProp) != schemaType(aProp):
				warnings = append(warnings, fmt.Sprintf("%s `%s` type %s → %s", label, name, schemaType(bProp), schemaType(aProp)))
			default:
				if fields := newRequiredFields(bProp, aProp); len(fields) > 0 {
					warnings = append(warnings, fmt.Sprintf("%s `%s` now requires %s", label, name, quoteAll(fields)))
				} else if !jsonEqual(bProp, aProp) {
					info = append(info, fmt.Sprintf("%s `%s` schema changed", label, name))
				}
			}
		}
	}

	if len(warnings) == 0 && len(info) == 0 {
		info = append(info, "input schema changed")
	}

	return warnings, info
}

func parseInputObject(raw json.RawMessage) inputObjectSchema {
	var obj inputObjectSchema
	_ = json.Unmarshal(raw, &obj)
	return obj
}

// maxInlinedSchemaNodes bounds how many schema nodes inlining one input may
// produce. Shared schemas that reference each other more than once per level
// expand exponentially; real tool inputs stay well under a few thousand
// nodes, so ten thousand only stops pathological documents.
const maxInlinedSchemaNodes = 10_000

// schemaKeywords are the JSON Schema keywords whose value is one schema.
var schemaKeywords = map[string]bool{
	"items": true, "additionalItems": true, "additionalProperties": true,
	"not": true, "if": true, "then": true, "else": true, "contains": true,
	"propertyNames": true, "unevaluatedItems": true, "unevaluatedProperties": true,
}

// schemaListKeywords are the keywords whose value is a list of schemas.
var schemaListKeywords = map[string]bool{
	"allOf": true, "anyOf": true, "oneOf": true, "prefixItems": true,
}

// schemaMapKeywords are the keywords whose value maps names to schemas.
var schemaMapKeywords = map[string]bool{
	"properties": true, "patternProperties": true, "$defs": true,
	"definitions": true, "dependentSchemas": true,
}

// inlineDefs returns the schema with every `#/$defs/` reference replaced by
// the schema it points at. Only schema keywords are followed, so literal data
// such as defaults and examples is left untouched. A reference already being
// expanded higher up is left in place so recursive schemas terminate. It
// reports false, with the schema unchanged, when the result would exceed
// maxInlinedSchemaNodes.
func inlineDefs(raw json.RawMessage, defs map[string]json.RawMessage) (json.RawMessage, bool) {
	var v any
	if len(defs) == 0 || json.Unmarshal(raw, &v) != nil {
		return raw, true
	}

	in := &defInliner{defs: defs, expanding: map[string]bool{}, budget: maxInlinedSchemaNodes}
	resolved := in.schema(v)
	if in.budget < 0 {
		return raw, false
	}

	out, err := json.Marshal(resolved)
	if err != nil {
		return raw, true
	}
	return out, true
}

// defsEqual reports whether two sets of shared schemas are the same.
func defsEqual(a, b map[string]json.RawMessage) bool {
	return maps.EqualFunc(a, b, jsonEqual)
}

type defInliner struct {
	// defs holds the schemas references point at.
	defs map[string]json.RawMessage

	// expanding holds the definitions on the current path, to stop cycles.
	expanding map[string]bool

	// budget counts the schema nodes that may still be produced. It goes
	// negative once the limit is passed.
	budget int
}

func (in *defInliner) schema(v any) any {
	node, ok := v.(map[string]any)
	if !ok {
		return v
	}

	in.budget--
	if in.budget < 0 {
		return v
	}

	if ref, ok := node["$ref"].(string); ok {
		name, isDef := strings.CutPrefix(ref, "#/$defs/")
		def, found := in.defs[name]
		var resolved any
		if isDef && found && !in.expanding[name] && json.Unmarshal(def, &resolved) == nil {
			in.expanding[name] = true
			out := in.schema(resolved)
			delete(in.expanding, name)
			return out
		}
	}

	out := make(map[string]any, len(node))
	for key, child := range node {
		switch {
		case schemaKeywords[key]:
			if list, ok := child.([]any); ok {
				out[key] = in.schemaList(list)
			} else {
				out[key] = in.schema(child)
			}
		case schemaListKeywords[key]:
			if list, ok := child.([]any); ok {
				out[key] = in.schemaList(list)
			} else {
				out[key] = child
			}
		case schemaMapKeywords[key]:
			if m, ok := child.(map[string]any); ok {
				resolved := make(map[string]any, len(m))
				for name, sub := range m {
					resolved[name] = in.schema(sub)
				}
				out[key] = resolved
			} else {
				out[key] = child
			}
		default:
			out[key] = child
		}
	}
	return out
}

func (in *defInliner) schemaList(list []any) []any {
	out := make([]any, len(list))
	for i, child := range list {
		out[i] = in.schema(child)
	}
	return out
}

// newRequiredFields lists the fields an object schema requires after a
// change that it did not require before.
func newRequiredFields(before, after json.RawMessage) []string {
	var b, a inputObjectSchema
	_ = json.Unmarshal(before, &b)
	_ = json.Unmarshal(after, &a)

	var fields []string
	for _, name := range a.Required {
		if !slices.Contains(b.Required, name) {
			fields = append(fields, name)
		}
	}
	slices.Sort(fields)
	return fields
}

func quoteAll(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "`" + n + "`"
	}
	return strings.Join(quoted, ", ")
}

func schemaType(raw json.RawMessage) string {
	var s struct {
		Type any    `json:"type"`
		Ref  string `json:"$ref"`
	}
	_ = json.Unmarshal(raw, &s)

	switch t := s.Type.(type) {
	case string:
		return t
	case []any:
		parts := make([]string, 0, len(t))
		for _, p := range t {
			parts = append(parts, fmt.Sprint(p))
		}
		return strings.Join(parts, "|")
	}

	if s.Ref != "" {
		return "object"
	}

	return "any"
}

func jsonEqual(a, b json.RawMessage) bool {
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return bytes.Equal(a, b)
	}
	ab, _ := json.Marshal(av)
	bb, _ := json.Marshal(bv)
	return bytes.Equal(ab, bb)
}

func skippedOperationChanges(base, next *extractedDocumentSpeakeasy) []EvaluationChange {
	baseSkipped := make(map[string]struct{})
	baseTools := make(map[string]struct{})
	if base != nil {
		for _, op := range base.operations {
			key := operationKey(op.method, op.path)
			if op.def == nil {
				baseSkipped[key] = struct{}{}
			} else {
				baseTools[key] = struct{}{}
			}
		}
	}

	var details []string
	for _, op := range next.operations {
		if op.def != nil {
			continue
		}

		key := operationKey(op.method, op.path)
		if _, ok := baseSkipped[key]; ok {
			continue
		}
		// Operations that produced a tool before are reported as tools that
		// can no longer be generated.
		if _, ok := baseTools[key]; ok {
			continue
		}

		details = append(details, fmt.Sprintf("%s: %s", key, op.err.Error()))
	}

	if len(details) == 0 {
		return nil
	}

	return []EvaluationChange{newChange(
		EvaluationSeverityWarning,
		EvaluationCategoryTools,
		"operations_skipped",
		fmt.Sprintf("%d operations cannot be turned into tools", len(details)),
		details,
		nil,
	)}
}

func toolsUsingScheme(doc *extractedDocumentSpeakeasy, key string) []string {
	var names []string
	for _, op := range doc.operations {
		if op.def == nil {
			continue
		}
		for _, req := range parseSecurity(op.def.Security) {
			if _, ok := req[key]; ok {
				names = append(names, op.def.Name)
				break
			}
		}
	}
	return names
}

func diffStrings(before, after []string) (added, removed []string) {
	for _, s := range after {
		if !slices.Contains(before, s) {
			added = append(added, s)
		}
	}
	for _, s := range before {
		if !slices.Contains(after, s) {
			removed = append(removed, s)
		}
	}
	slices.Sort(added)
	slices.Sort(removed)
	return added, removed
}

func newChange(severity EvaluationSeverity, category EvaluationCategory, code, title string, details, tools []string) EvaluationChange {
	return EvaluationChange{
		Severity: severity,
		Category: category,
		Code:     code,
		Title:    title,
		Details:  capList(details, maxEvaluationDetails),
		Tools:    tools,
	}
}

// countTools renders a tool count with the noun agreeing in number.
func countTools(n int) string {
	if n == 1 {
		return "1 tool"
	}
	return fmt.Sprintf("%d tools", n)
}

func capList(items []string, limit int) []string {
	if len(items) <= limit {
		return items
	}
	return append(slices.Clone(items[:limit]), fmt.Sprintf("…and %d more", len(items)-limit))
}

func severityRank(s EvaluationSeverity) int {
	switch s {
	case EvaluationSeverityBreaking:
		return 0
	case EvaluationSeverityWarning:
		return 1
	default:
		return 2
	}
}

func categoryRank(c EvaluationCategory) int {
	switch c {
	case EvaluationCategorySecurity:
		return 0
	case EvaluationCategoryServer:
		return 1
	default:
		return 2
	}
}

func sortChanges(changes []EvaluationChange) {
	slices.SortStableFunc(changes, func(a, b EvaluationChange) int {
		return cmp.Or(
			cmp.Compare(severityRank(a.Severity), severityRank(b.Severity)),
			cmp.Compare(categoryRank(a.Category), categoryRank(b.Category)),
		)
	})
}
