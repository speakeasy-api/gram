package platformmcp

import (
	"context"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

// Which tools draw a result card, and where each card's button goes.
//
// A tool earns a card when its result is something the user would open in the
// dashboard or a change they care about, and the card links to that page.
// Every other external tool is text-only, with the reason in textOnlyTools.
// TestEveryExternalToolHasACardOrAWrittenReason keeps the two lists complete.

// cardSpec is a carded tool's card kind and its dashboard link.
type cardSpec struct {
	kind cardKind
	// route is the dashboard page the card links to. It starts with "P/"
	// for a project page (/<org>/projects/<project>/...) or "O/" for an
	// organization page (/<org>/...). A {name} segment is filled from the
	// result or the call's arguments using cardRouteParams; the special
	// route "{dashboard_path}" uses the path the result returns. An empty
	// route is allowed only for a tool that builds its own card.
	route string
	label string
}

// cardTools lists every tool that draws a card.
var cardTools = map[string]cardSpec{
	// Who the session is and MCP server setup build their own cards.
	// Catalog search draws its own carousel of server cards.
	"search_mcp_catalog":                    {kind: cardKindList, route: "P/mcp/catalog", label: "Open catalog"},
	"whoami":                                {kind: cardKindDetail, route: "", label: ""},
	"register_catalog_mcp":                  {kind: cardKindReceipt, route: "P/mcp/{mcp}", label: "Open MCP server"},
	"register_remote_mcp":                   {kind: cardKindReceipt, route: "P/mcp/{mcp}", label: "Open MCP server"},
	"attach_platform_mcp_identity_provider": {kind: cardKindReceipt, route: "P/mcp/{mcp}/settings", label: "Open settings"},

	// MCP servers.
	"find_mcp":                              {kind: cardKindList, route: "P/mcp", label: "Open MCP servers"},
	"get_mcp":                               {kind: cardKindDetail, route: "P/mcp/{mcp}", label: "Open MCP server"},
	"get_mcp_readiness":                     {kind: cardKindDetail, route: "P/mcp/{mcp}", label: "Open MCP server"},
	"get_mcp_access":                        {kind: cardKindDetail, route: "P/mcp/{mcp}/team-access", label: "Open team access"},
	"get_mcp_client_admission":              {kind: cardKindDetail, route: "P/mcp/{mcp}/settings", label: "Open settings"},
	"get_mcp_connection_settings":           {kind: cardKindDetail, route: "P/mcp/{mcp}/settings", label: "Open settings"},
	"get_setup_handoff":                     {kind: cardKindDetail, route: "P/mcp/{mcp}/setup", label: "Finish setup"},
	"get_my_mcp_review_request":             {kind: cardKindDetail, route: "P/mcp/{mcp}", label: "Open MCP server"},
	"create_mcp_from_functions":             {kind: cardKindReceipt, route: "P/mcp/{mcp}", label: "Open MCP server"},
	"update_mcp_metadata":                   {kind: cardKindReceipt, route: "P/mcp/{mcp}", label: "Open MCP server"},
	"enable_mcp":                            {kind: cardKindReceipt, route: "P/mcp/{mcp}", label: "Open MCP server"},
	"disable_mcp":                           {kind: cardKindReceipt, route: "P/mcp/{mcp}", label: "Open MCP server"},
	"set_mcp_address":                       {kind: cardKindReceipt, route: "P/mcp/{mcp}/settings", label: "Open settings"},
	"set_mcp_network_access":                {kind: cardKindReceipt, route: "P/mcp/{mcp}/settings", label: "Open settings"},
	"set_mcp_client_admission":              {kind: cardKindReceipt, route: "P/mcp/{mcp}/settings", label: "Open settings"},
	"set_mcp_role_auth_client_access":       {kind: cardKindReceipt, route: "P/mcp/{mcp}/team-access", label: "Open team access"},
	"offer_mcp_upstream_credential":         {kind: cardKindReceipt, route: "P/mcp/{mcp}/settings", label: "Open settings"},
	"choose_mcp_headless_caller_credential": {kind: cardKindReceipt, route: "P/mcp/{mcp}/settings", label: "Open settings"},
	"add_tools_to_mcp":                      {kind: cardKindReceipt, route: "P/mcp/{mcp}/tools", label: "Open tools"},
	"remove_tools_from_mcp":                 {kind: cardKindReceipt, route: "P/mcp/{mcp}/tools", label: "Open tools"},
	"request_mcp_review":                    {kind: cardKindReceipt, route: "P/mcp/{mcp}", label: "Open MCP server"},
	"redeploy_deployment":                   {kind: cardKindReceipt, route: "P/deployments/{deployment}", label: "Open deployment"},

	// Plugins.
	"list_plugins":             {kind: cardKindList, route: "P/plugins", label: "Open plugins"},
	"get_plugin":               {kind: cardKindDetail, route: "P/plugins/{plugin}", label: "Open plugin"},
	"list_plugin_assignments":  {kind: cardKindList, route: "P/plugins/{plugin}/assignments", label: "Open assignments"},
	"create_plugin":            {kind: cardKindReceipt, route: "P/plugins/{plugin}", label: "Open plugin"},
	"rename_plugin":            {kind: cardKindReceipt, route: "P/plugins/{plugin}", label: "Open plugin"},
	"set_plugin_auto_sync":     {kind: cardKindReceipt, route: "P/plugins/{plugin}/settings", label: "Open plugin settings"},
	"set_plugin_assignments":   {kind: cardKindReceipt, route: "P/plugins/{plugin}/assignments", label: "Open assignments"},
	"republish_plugin":         {kind: cardKindReceipt, route: "P/plugins/{plugin}", label: "Open plugin"},
	"distribute_mcp_to_plugin": {kind: cardKindReceipt, route: "P/plugins/{plugin}", label: "Open plugin"},
	"remove_mcp_from_plugin":   {kind: cardKindReceipt, route: "P/plugins/{plugin}", label: "Open plugin"},
	"remove_plugin_server":     {kind: cardKindReceipt, route: "P/plugins/{plugin}", label: "Open plugin"},

	// Skills.
	"list_skills":              {kind: cardKindList, route: "P/skills", label: "Open skills"},
	"get_skill":                {kind: cardKindDetail, route: "P/skills/{skill}", label: "Open skill"},
	"list_skill_suggestions":   {kind: cardKindList, route: "P/skills", label: "Open skills"},
	"list_skill_distributions": {kind: cardKindList, route: "P/skills/{skill}", label: "Open skill"},
	"create_skill":             {kind: cardKindReceipt, route: "P/skills/{skill}", label: "Open skill"},
	"add_skill_version":        {kind: cardKindReceipt, route: "P/skills/{skill}", label: "Open skill"},
	"update_skill_metadata":    {kind: cardKindReceipt, route: "P/skills/{skill}/settings", label: "Open skill settings"},
	"distribute_skill":         {kind: cardKindReceipt, route: "P/skills/{skill}", label: "Open skill"},
	"undistribute_skill":       {kind: cardKindReceipt, route: "P/skills/{skill}", label: "Open skill"},
	"link_skill_to_mcp":        {kind: cardKindReceipt, route: "P/skills/{skill}", label: "Open skill"},
	"unlink_skill_from_mcp":    {kind: cardKindReceipt, route: "P/skills/{skill}", label: "Open skill"},
	"approve_skill_suggestion": {kind: cardKindReceipt, route: "P/skills", label: "Open skills"},
	"dismiss_skill_suggestion": {kind: cardKindReceipt, route: "P/skills", label: "Open skills"},

	// Risk policies and findings.
	"list_risk_policies":                  {kind: cardKindList, route: "P/risk-policies", label: "Open risk policies"},
	"get_risk_policy":                     {kind: cardKindDetail, route: "P/risk-policies/{policy}", label: "Open policy"},
	"create_risk_policy":                  {kind: cardKindReceipt, route: "P/risk-policies/{policy}", label: "Open policy"},
	"update_risk_policy":                  {kind: cardKindReceipt, route: "P/risk-policies/{policy}", label: "Open policy"},
	"change_risk_policy_audience":         {kind: cardKindReceipt, route: "P/risk-policies/{policy}", label: "Open policy"},
	"remove_self_from_risk_policy":        {kind: cardKindReceipt, route: "P/risk-policies/{policy}", label: "Open policy"},
	"list_risk_exclusions":                {kind: cardKindList, route: "P/risk-policies", label: "Open risk policies"},
	"create_risk_exclusion":               {kind: cardKindReceipt, route: "P/risk-policies", label: "Open risk policies"},
	"update_risk_exclusion":               {kind: cardKindReceipt, route: "P/risk-policies", label: "Open risk policies"},
	"list_risk_findings":                  {kind: cardKindList, route: "P/risk-events", label: "Open risk events"},
	"list_risk_findings_by_chat":          {kind: cardKindList, route: "P/risk-events", label: "Open risk events"},
	"list_watchdog_findings":              {kind: cardKindList, route: "P/watchdog", label: "Open Watchdog"},
	"mark_risk_findings_false_positive":   {kind: cardKindReceipt, route: "P/risk-events", label: "Open risk events"},
	"unmark_risk_findings_false_positive": {kind: cardKindReceipt, route: "P/risk-events", label: "Open risk events"},

	// Shadow MCP, shadow AI and devices.
	"list_shadow_mcp_inventory": {kind: cardKindList, route: "P/shadow-ai", label: "Open shadow AI"},
	"get_shadow_mcp_review":     {kind: cardKindDetail, route: "P/shadow-ai", label: "Open shadow AI"},
	"decide_shadow_mcp_access":  {kind: cardKindReceipt, route: "P/shadow-ai", label: "Open shadow AI"},
	"list_shadow_ai_inventory":  {kind: cardKindList, route: "P/shadow-ai", label: "Open shadow AI"},
	"get_device_coverage":       {kind: cardKindDetail, route: "O/device-agent/devices", label: "Open devices"},
	"list_managed_devices":      {kind: cardKindList, route: "O/device-agent/devices", label: "Open devices"},

	// Access.
	"list_access_roles":               {kind: cardKindList, route: "O/access/roles", label: "Open roles"},
	"get_member_mcp_access":           {kind: cardKindDetail, route: "O/access/members", label: "Open members"},
	"create_mcp_access_role":          {kind: cardKindReceipt, route: "O/access/roles", label: "Open roles"},
	"update_mcp_access_role":          {kind: cardKindReceipt, route: "O/access/roles", label: "Open roles"},
	"configure_inference_access_role": {kind: cardKindReceipt, route: "O/access/roles", label: "Open roles"},
	"configure_memory_access_role":    {kind: cardKindReceipt, route: "O/access/roles", label: "Open roles"},
	"assign_mcp_access_role":          {kind: cardKindReceipt, route: "O/access/assignments", label: "Open assignments"},
	"list_directory_review_items":     {kind: cardKindList, route: "O/access/members", label: "Open members"},
	"resolve_directory_review_item":   {kind: cardKindReceipt, route: "O/access/members", label: "Open members"},

	// Sign-in and identity providers.
	"trust_inbound_ema_identity_provider":   {kind: cardKindReceipt, route: "O/identity", label: "Open identity settings"},
	"untrust_inbound_ema_identity_provider": {kind: cardKindReceipt, route: "O/identity", label: "Open identity settings"},
	"set_workload_assertion_reuse":          {kind: cardKindReceipt, route: "O/identity", label: "Open identity settings"},

	// Gateways.
	"create_gateway":              {kind: cardKindReceipt, route: "P/mcp/gateway/{gateway}", label: "Open gateway"},
	"set_gateway_members":         {kind: cardKindReceipt, route: "P/mcp/gateway/{gateway}", label: "Open gateway"},
	"get_my_project_gateway":      {kind: cardKindDetail, route: "P/mcp/organization-gateway", label: "Open gateway"},
	"get_my_organization_gateway": {kind: cardKindDetail, route: "O/gateway", label: "Open gateway"},

	// Data exports.
	"list_data_exports":  {kind: cardKindList, route: "O/data/exports", label: "Open data exports"},
	"create_data_export": {kind: cardKindReceipt, route: "O/data/exports", label: "Open data exports"},
	"pause_data_export":  {kind: cardKindReceipt, route: "O/data/exports", label: "Open data exports"},
	"resume_data_export": {kind: cardKindReceipt, route: "O/data/exports", label: "Open data exports"},

	// Dashboards and Explore widgets.
	"list_dashboards":        {kind: cardKindList, route: "P/dashboards", label: "Open dashboards"},
	"get_dashboard":          {kind: cardKindDetail, route: "{dashboard_path}", label: "Open dashboard"},
	"save_dashboard":         {kind: cardKindReceipt, route: "{dashboard_path}", label: "Open dashboard"},
	"save_dashboard_filters": {kind: cardKindReceipt, route: "{dashboard_path}", label: "Open dashboard"},
	"duplicate_dashboard":    {kind: cardKindReceipt, route: "{dashboard_path}", label: "Open dashboard"},
	"edit_dashboard_layout":  {kind: cardKindReceipt, route: "{dashboard_path}", label: "Open dashboard"},
	"delete_dashboard":       {kind: cardKindReceipt, route: "P/dashboards", label: "Open dashboards"},
	"list_widgets":           {kind: cardKindList, route: "P/insights", label: "Open insights"},
	"get_widget":             {kind: cardKindDetail, route: "P/insights", label: "Open insights"},
	"save_widget":            {kind: cardKindReceipt, route: "P/insights", label: "Open insights"},
	"duplicate_widget":       {kind: cardKindReceipt, route: "P/insights", label: "Open insights"},
	"delete_widget":          {kind: cardKindReceipt, route: "P/insights", label: "Open insights"},

	// Signals and sensors.
	"find_signals":  {kind: cardKindList, route: "P/signals-intelligence", label: "Open signals"},
	"find_sensors":  {kind: cardKindList, route: "P/signals-intelligence", label: "Open signals"},
	"get_sensor":    {kind: cardKindDetail, route: "P/signals-intelligence", label: "Open signals"},
	"create_sensor": {kind: cardKindReceipt, route: "P/signals-intelligence", label: "Open signals"},
	"update_sensor": {kind: cardKindReceipt, route: "P/signals-intelligence", label: "Open signals"},
	"create_signal": {kind: cardKindReceipt, route: "P/signals-intelligence", label: "Open signals"},
	"update_signal": {kind: cardKindReceipt, route: "P/signals-intelligence", label: "Open signals"},

	// Projects and chats.
	"list_projects":               {kind: cardKindList, route: "O", label: "Open Speakeasy"},
	"create_project":              {kind: cardKindReceipt, route: "P", label: "Open project"},
	"rename_project":              {kind: cardKindReceipt, route: "P", label: "Open project"},
	"list_chats":                  {kind: cardKindList, route: "P/chat", label: "Open chats"},
	"create_session_handoff_link": {kind: cardKindReceipt, route: "", label: ""},
}

// Reasons a tool stays text-only.
const (
	textOnlyForModel  = "returns detail for the model to reason over or explain; there is no page a card could add"
	textOnlyAnalytics = "returns analytics rows or rollups for the model to summarise; a card cannot show them"
	textOnlyLog       = "returns log entries or search hits for the model to read; there is no single page to open"
	textOnlyPicker    = "returns choices the model presents so the user can pick one in the conversation"
	textOnlyAssistant = "configures a managed assistant, which has no dashboard page to link to"
	textOnlyMember    = "answers a member about their own access; members may not reach the matching dashboard page"
	textOnlyNothing   = "has nothing for the user to look at"
)

// textOnlyTools keep their plain result, each with the reason.
var textOnlyTools = map[string]string{
	"get_my_channels_access":               "Channels connection admission and endpoint instructions have no standalone dashboard resource.",
	"run_analytics_query":                  textOnlyAnalytics,
	"query_mcp_events":                     textOnlyAnalytics,
	"query_mcp_metrics":                    textOnlyAnalytics,
	"query_mcp_traces":                     textOnlyAnalytics,
	"query_skill_usage":                    textOnlyAnalytics,
	"describe_analytics_catalog":           textOnlyForModel,
	"list_analytics_dimension_values":      textOnlyForModel,
	"list_attribute_keys":                  textOnlyForModel,
	"get_project_overview":                 textOnlyAnalytics,
	"get_tool_usage_summary":               textOnlyAnalytics,
	"get_user_mcp_status":                  textOnlyAnalytics,
	"get_user_metrics_summary":             textOnlyAnalytics,
	"get_user_skill_status":                textOnlyAnalytics,
	"list_mcp_usage_users":                 textOnlyAnalytics,
	"list_skill_usage_users":               textOnlyAnalytics,
	"list_skill_insights":                  textOnlyAnalytics,
	"get_organization_gateway_activity":    textOnlyAnalytics,
	"get_mcp_network_traffic":              textOnlyAnalytics,
	"get_risk_rule_breakdown":              textOnlyAnalytics,
	"list_organization_events":             textOnlyLog,
	"list_recent_tool_calls":               textOnlyLog,
	"search_tool_calls":                    textOnlyLog,
	"list_gateway_sessions":                textOnlyLog,
	"search_chats":                         textOnlyLog,
	"read_chat":                            textOnlyLog,
	"search_team_memory":                   textOnlyLog,
	"find_signal_sessions":                 textOnlyLog,
	"list_skill_feedback":                  textOnlyLog,
	"list_skill_suggestion_feedback":       textOnlyLog,
	"list_skill_versions":                  textOnlyForModel,
	"compare_skill_versions":               textOnlyForModel,
	"get_platform_context":                 textOnlyForModel,
	"describe_access_permissions":          textOnlyForModel,
	"list_agent_authorization_checks":      textOnlyForModel,
	"get_agent_mcp_access":                 textOnlyForModel,
	"get_mcp_diagnostics":                  textOnlyForModel,
	"get_mcp_repair_plan":                  textOnlyForModel,
	"get_risk_analysis_status":             textOnlyForModel,
	"get_signal_definition":                textOnlyForModel,
	"preview_sensor_match":                 textOnlyForModel,
	"suggest_risk_policy":                  textOnlyForModel,
	"list_risk_presets":                    textOnlyForModel,
	"list_ai_scan_library":                 textOnlyForModel,
	"get_xaa_readiness":                    textOnlyForModel,
	"get_internal_session_issuer":          textOnlyForModel,
	"get_workload_assertion_reuse":         textOnlyForModel,
	"get_inbound_ema_trust":                textOnlyForModel,
	"get_network_ingress":                  textOnlyForModel,
	"get_custom_domain_status":             textOnlyForModel,
	"describe_tools":                       textOnlyForModel,
	"execute_tool":                         "returns another MCP server's own result, which that server presents",
	"search_speakeasy_docs":                "returns doc passages for the model to read and cite; it is also a first-run tool, and its card link would push tools/list past the host budget",
	"list_access_members":                  textOnlyPicker,
	"list_project_tools":                   textOnlyPicker,
	"list_my_sessions":                     textOnlyPicker,
	"list_my_servers":                      textOnlyPicker,
	"inspect_mcp_candidate":                textOnlyPicker,
	"search_my_tools":                      textOnlyPicker,
	"search_users":                         textOnlyPicker,
	"continue_session":                     textOnlyForModel,
	"select_assistant_runtime":             textOnlyAssistant,
	"choose_assistant_upstream_credential": textOnlyAssistant,
	"get_my_install_instructions":          textOnlyMember,
	"get_my_mcp_access":                    textOnlyMember,
	"get_my_mcp_connection_status":         textOnlyMember,
	"list_my_mcp_access":                   textOnlyMember,
	"send_platform_mcp_feedback":           textOnlyNothing,
	"record_workflow_run":                  textOnlyNothing,
	"remember":                             textOnlyForModel,
}

// cardRouteParam fills one {name} route segment. Exact keys are read from
// the result and then the call's arguments; subject keys only from the object
// the result is about, where a bare "id" or "slug" is unambiguous.
type cardRouteParam struct {
	exact   []string
	subject []string
	// nested is a result key holding the resource itself, such as "policy",
	// whose "id" fills the segment.
	nested string
}

var cardRouteParams = map[string]cardRouteParam{
	"mcp":        {exact: []string{"mcp_slug", "mcp_id"}, subject: []string{"slug"}, nested: "mcp"},
	"plugin":     {exact: []string{"plugin_id"}, subject: []string{"id"}, nested: "plugin"},
	"skill":      {exact: []string{"skill_id"}, subject: []string{"id"}, nested: "skill"},
	"policy":     {exact: []string{"policy_id"}, subject: []string{"id"}, nested: "policy"},
	"gateway":    {exact: []string{"gateway_id"}, subject: []string{"id"}, nested: "gateway"},
	"deployment": {exact: []string{"deployment_id"}, subject: nil, nested: ""},
}

// cardLinks builds dashboard links for cards.
type cardLinks struct {
	dashboardURL *url.URL
	serverURL    *url.URL
	// projectSlug resolves a project ID in the caller's organization to its
	// slug, for results that name the project only by ID. It returns "" when
	// the project is not found. It may be nil.
	projectSlug func(ctx context.Context, projectID string) string
}

// newCardLinks resolves project slugs with the org-scoped project lookup the
// tools already use, so a card can only link to a project in the caller's
// own organization.
func newCardLinks(dashboardURL, serverURL *url.URL, db platformrepo.DBTX) cardLinks {
	links := cardLinks{dashboardURL: dashboardURL, serverURL: serverURL, projectSlug: nil}
	if db == nil {
		return links
	}
	links.projectSlug = func(ctx context.Context, projectID string) string {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return ""
		}
		parsed, err := uuid.Parse(strings.TrimSpace(projectID))
		if err != nil {
			return ""
		}
		row, err := platformrepo.New(db).ResolvePlatformMCPProjectByID(ctx, platformrepo.ResolvePlatformMCPProjectByIDParams{OrganizationID: principal.OrganizationID, ProjectID: parsed})
		if err != nil {
			return ""
		}
		return row.Slug
	}
	return links
}

// link is the card's dashboard button, or nil when the route cannot be filled
// exactly. It never guesses: a missing project, organization or id leaves the
// card without a button rather than pointing somewhere else.
//
// When a page's own id cannot be filled, the card links to that section's
// index instead, such as the MCP servers list for an MCP server page.
func (l cardLinks) link(ctx context.Context, spec cardSpec, object, input jsonObject) *cardAction {
	if action := l.exactLink(ctx, spec, object, input); action != nil {
		return action
	}
	if fallback, ok := cardSectionRoute(spec.route); ok {
		return l.exactLink(ctx, fallback, object, input)
	}
	return nil
}

// cardSectionLabels name the section index pages a card falls back to.
var cardSectionLabels = map[string]string{
	"mcp":           "Open MCP servers",
	"plugins":       "Open plugins",
	"skills":        "Open skills",
	"risk-policies": "Open risk policies",
	"deployments":   "Open deployments",
	"dashboards":    "Open dashboards",
}

// noCardSpec is returned with false when a route has no section index.
var noCardSpec cardSpec

// cardSectionRoute is the index page above a route's first id segment.
func cardSectionRoute(route string) (cardSpec, bool) {
	if route == "{dashboard_path}" {
		return cardSpec{kind: "", route: "P/dashboards", label: cardSectionLabels["dashboards"]}, true
	}
	head, _, found := strings.Cut(route, "/{")
	if !found {
		return noCardSpec, false
	}
	section := head[strings.LastIndex(head, "/")+1:]
	label, ok := cardSectionLabels[section]
	if !ok {
		return noCardSpec, false
	}
	return cardSpec{kind: "", route: head, label: label}, true
}

func (l cardLinks) exactLink(ctx context.Context, spec cardSpec, object, input jsonObject) *cardAction {
	if spec.route == "" || l.dashboardURL == nil {
		return nil
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || strings.TrimSpace(authCtx.OrganizationSlug) == "" {
		return nil
	}
	org := strings.TrimSpace(authCtx.OrganizationSlug)
	base := requestDashboardURL(ctx, l.dashboardURL, l.serverURL)
	if base == nil {
		return nil
	}
	subject := cardSubject(object)

	if spec.route == "{dashboard_path}" {
		path := object.firstString([]string{"dashboard_path"})
		if path == "" {
			if current, ok := object.object("current"); ok {
				path = current.firstString([]string{"dashboard_path"})
			}
		}
		prefix := "/" + org + "/projects/"
		if !strings.HasPrefix(path, prefix) || strings.Contains(path, "..") {
			return nil
		}
		return &cardAction{Label: spec.label, URL: base.JoinPath(strings.Split(strings.TrimPrefix(path, "/"), "/")...).String()}
	}

	scope, rest, _ := strings.Cut(spec.route, "/")
	segments := []string{org}
	if scope == "P" {
		project := cardProjectSlug(object, subject, input)
		if project == "" && l.projectSlug != nil {
			if id := cardProjectID(object, subject, input); id != "" {
				project = l.projectSlug(ctx, id)
			}
		}
		if project == "" {
			return nil
		}
		segments = append(segments, "projects", project)
	}
	if rest != "" {
		for segment := range strings.SplitSeq(rest, "/") {
			name, isParam := strings.CutPrefix(segment, "{")
			if !isParam {
				segments = append(segments, segment)
				continue
			}
			value := cardRouteValue(cardRouteParams[strings.TrimSuffix(name, "}")], object, subject, input)
			if value == "" {
				return nil
			}
			segments = append(segments, value)
		}
	}
	return &cardAction{Label: spec.label, URL: base.JoinPath(segments...).String()}
}

func cardRouteValue(param cardRouteParam, object, subject, input jsonObject) string {
	for _, candidate := range []jsonObject{object, subject, input} {
		if value := candidate.firstString(param.exact); value != "" {
			return value
		}
	}
	if param.nested != "" {
		if nested, ok := object.object(param.nested); ok {
			if id := nested.firstString([]string{"id"}); id != "" {
				return id
			}
		}
	}
	return subject.firstString(param.subject)
}

// cardProjectID is the project's ID when the result or the call's arguments
// name it only by ID.
func cardProjectID(object, subject, input jsonObject) string {
	for _, candidate := range []jsonObject{object, subject, input} {
		if id := candidate.firstString([]string{"project_id"}); id != "" {
			return id
		}
		if project, ok := candidate.object("project"); ok {
			if id := project.firstString([]string{"id"}); id != "" {
				return id
			}
		}
	}
	return ""
}

// cardProjectSlug is the project a project page belongs to, from the result
// or the call's arguments.
func cardProjectSlug(object, subject, input jsonObject) string {
	if field := cardProjectField(object, subject); field != nil {
		return field.Value
	}
	return input.firstString([]string{"project_slug"})
}
