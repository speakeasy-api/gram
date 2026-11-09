package platformmcp

import (
	"cmp"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// TestRenderCardGallery draws every tool's card from a sample output and fails
// when one cannot be drawn. It writes the cards to one HTML page for review;
// set PLATFORM_MCP_CARD_GALLERY to keep that page:
//
//	PLATFORM_MCP_CARD_GALLERY=$PWD/cards.html mise run test:server ./internal/platformmcp/ -run TestRenderCardGallery
func TestRenderCardGallery(t *testing.T) {
	t.Parallel()

	path := os.Getenv("PLATFORM_MCP_CARD_GALLERY")
	if path == "" {
		path = filepath.Join(t.TempDir(), "cards.html")
	}

	descriptors := galleryDescriptors()
	unavailable := reflect.TypeFor[featureUnavailableResult]()
	type entry struct {
		Name   string
		Note   string
		Card   template.HTML
		Reason string
	}
	sections := map[cardKind][]entry{cardKindDetail: nil, cardKindList: nil, cardKindReceipt: nil}
	var textOnly []entry
	for _, descriptor := range descriptors {
		if !descriptor.Meta.servesAudience(AudienceExternal) {
			continue
		}
		kind, carded := toolCardKind(descriptor.Name, descriptor.Annotations)
		if !carded {
			textOnly = append(textOnly, entry{Name: descriptor.Name, Note: "", Card: "", Reason: textOnlyTools[descriptor.Name]})
			continue
		}
		note := ""
		switch descriptor.Name {
		case "whoami", "register_catalog_mcp", "register_remote_mcp", "attach_platform_mcp_identity_provider":
			note = "builds its own card on success; this is the generic fallback"
		}
		if descriptor.output == unavailable {
			note = "sample needs a live service; shows the unavailable card"
		}
		sample := gallerySampleOutput(descriptor.output)
		if descriptor.output == unavailable {
			sample = featureUnavailableResult{Code: unavailableCode, Feature: descriptor.Name, Message: "This is not switched on for your organization yet."}
		}
		result := withResultCard(cmp.Or(descriptor.Title, cardHumanize(descriptor.Name)), kind, nil, sample)
		rendered, _ := result.Meta[cardMetaKey].(string)
		require.NotEmpty(t, rendered, "tool %q draws no card for its sample output", descriptor.Name)
		sections[kind] = append(sections[kind], entry{Name: descriptor.Name, Note: note, Card: template.HTML(rendered), Reason: ""})
	}

	refusal := withResultCard("Rename Plugin", cardKindReceipt, &mcp.CallToolResult{
		Meta:              nil,
		Content:           []mcp.Content{&mcp.TextContent{Text: `{"code":"permission_denied","request_access_url":"https://app.speakeasy.com/example/request-access","message":"You do not have permission to use this Platform MCP tool. This action requires org:admin."}`}},
		StructuredContent: nil,
		IsError:           true,
		InputRequests:     nil,
		RequestState:      "",
	}, nil)
	refusalHTML, _ := refusal.Meta[cardMetaKey].(string)

	style := cardShellHTML[strings.Index(cardShellHTML, "<style>")+len("<style>") : strings.Index(cardShellHTML, "</style>")]
	page := template.Must(template.New("gallery").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Platform MCP cards</title>
<style>{{.Style}}
body { padding: 24px 16px; max-width: 1280px; margin: 0 auto; background: var(--tile); }
.page h2 { font: 400 22px/1.2 var(--serif); margin: 32px 0 12px; }
.page p.intro { color: var(--muted); margin-bottom: 8px; }
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(100%, 520px), 1fr)); gap: 20px; }
.item > code { display: block; margin: 0 0 6px; font-size: 12px; color: var(--muted); }
.item .note { font-size: 12px; color: var(--action); margin: 0 0 6px; }
table { border-collapse: collapse; font-size: 13px; } td { border-top: 1px solid var(--hair); padding: 6px 12px 6px 0; vertical-align: top; }
</style></head><body><div class="page">
<h1>Platform MCP result cards</h1>
<p class="intro">Every external tool's card, drawn from sample outputs. {{.Total}} tools have a card; {{len .TextOnly}} stay text-only.</p>
{{range .Sections}}<h2 id="{{.Title}}">{{.Title}} ({{len .Entries}})</h2><div class="grid">{{range .Entries}}<div class="item"><code>{{.Name}}</code>{{with .Note}}<p class="note">{{.}}</p>{{end}}{{.Card}}</div>{{end}}</div>
{{end}}<h2>Refusal</h2><div class="grid"><div class="item"><code>any tool, permission denied</code>{{.Refusal}}</div></div>
<h2>Text-only ({{len .TextOnly}})</h2><table>{{range .TextOnly}}<tr><td><code>{{.Name}}</code></td><td>{{.Reason}}</td></tr>{{end}}</table>
</div></body></html>`))

	type section struct {
		Title   string
		Entries []entry
	}
	ordered := []section{
		{Title: "Detail", Entries: sections[cardKindDetail]},
		{Title: "List", Entries: sections[cardKindList]},
		{Title: "Receipt", Entries: sections[cardKindReceipt]},
	}
	total := 0
	for _, s := range ordered {
		total += len(s.Entries)
	}
	var out strings.Builder
	require.NoError(t, page.Execute(&out, map[string]any{
		"Style":    template.CSS(style),
		"Sections": ordered,
		"Refusal":  template.HTML(refusalHTML),
		"TextOnly": textOnly,
		"Total":    total,
	}))
	require.NoError(t, os.WriteFile(path, []byte(out.String()), 0o600))
	t.Logf("wrote %d cards to %s", total, path)
}

// galleryDescriptors collects every external tool with its live output type
// where one can be reached. A live registration given empty services still
// declares its real output type; the handlers never run.
func galleryDescriptors() []Descriptor {
	_, base := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	byName := map[string]Descriptor{}
	var order []string
	for _, descriptor := range base.Descriptors() {
		if _, seen := byName[descriptor.Name]; !seen {
			order = append(order, descriptor.Name)
		}
		byName[descriptor.Name] = descriptor
	}
	unavailable := reflect.TypeFor[featureUnavailableResult]()
	registrations := &RegistrationService{}
	diagnostics := &DiagnosticsService{}
	skills := &SkillsService{}
	plugins := &PluginsService{}
	reader := &PostgresReader{}
	access := &AccessReadService{}
	mutations := &AccessRoleMutationService{}
	live := []func(*Registrar){
		func(r *Registrar) { registerReadinessTools(r, &ReadinessService{}) },
		func(r *Registrar) { registerDiagnosticsTools(r, diagnostics) },
		func(r *Registrar) { registerDrilldownTools(r, diagnostics) },
		func(r *Registrar) { registerToolUsageSummaryTool(r, diagnostics) },
		func(r *Registrar) { registerUserSearchTools(r, diagnostics) },
		func(r *Registrar) { registerToolCallSearchTools(r, diagnostics) },
		func(r *Registrar) { registerSkillUsageTools(r, diagnostics) },
		func(r *Registrar) { registerSkillsTools(r, skills) },
		func(r *Registrar) { registerSkillInsightsTools(r, skills) },
		func(r *Registrar) { registerPluginTools(r, plugins) },
		func(r *Registrar) { registerAccessReadTools(r, access) },
		func(r *Registrar) { registerAccessRoleMutationTools(r, mutations) },
		func(r *Registrar) { registerLifecycleMetadataTool(r, registrations) },
		func(r *Registrar) { registerLifecycleVisibilityTools(r, registrations) },
		func(r *Registrar) { registerSetupHandoffTool(r, registrations) },
		func(r *Registrar) { registerClientAdmissionTools(r, registrations) },
		func(r *Registrar) { registerCatalogRegistrationTool(r, registrations, nil) },
		func(r *Registrar) { registerRemoteRegistrationTool(r, registrations, nil) },
		func(r *Registrar) { registerIdentityProviderTool(r, registrations) },
		func(r *Registrar) { registerDistributionTools(r, nil, &DistributionService{}, registrations) },
		func(r *Registrar) { registerCatalogTools(r, nil, OperationBudget{}, nil, nil) },
		func(r *Registrar) { registerCandidateInspectionTool(r, nil, nil, nil, OperationBudget{}) },
		func(r *Registrar) { registerSearchDocsTool(r, nil, OperationBudget{}) },
		func(r *Registrar) {
			registerRiskToolsWithMutations(r, &RiskReadService{}, &RiskAnalysisStatusService{}, nil)
		},
		func(r *Registrar) { registerRiskFindingsTool(r, nil) },
		func(r *Registrar) { registerRiskFindingListTools(r, nil) },
		func(r *Registrar) { registerDataExportTools(r, reader) },
		func(r *Registrar) { registerDataExportMutationTool(r, reader) },
		func(r *Registrar) { registerRecentToolCallTools(r, reader) },
		func(r *Registrar) { registerMCPNetworkTrafficTool(r, reader) },
		func(r *Registrar) { registerOrganizationEventTools(r, reader) },
		func(r *Registrar) { registerNetworkIngressTool(r, &NetworkIngressStatusService{}) },
		func(r *Registrar) { registerReviewRequestTools(r, nil, nil, OperationBudget{}, nil) },
		func(r *Registrar) { registerShadowInventoryTools(r, &ShadowInventoryService{}) },
		func(r *Registrar) { registerShadowAITools(r, &ShadowAIService{}) },
		func(r *Registrar) { registerDeviceCoverageTools(r, &DeviceCoverageService{}) },
		func(r *Registrar) { registerOrganizationGatewayActivityTool(r, &OrganizationGatewayActivityService{}) },
		func(r *Registrar) { registerOrganizationGatewayTool(r, &OrganizationGatewayService{}) },
		func(r *Registrar) { registerGatewayProxyTools(r, &GatewayProxyService{}) },
		func(r *Registrar) { registerSignalTools(r, &SignalAuthoringService{}) },
		func(r *Registrar) { registerChatMetadataTools(r, &ChatMetadataService{}) },
		func(r *Registrar) { registerWorkflowRunTool(r, &WorkflowRunService{}) },
	}
	for _, register := range live {
		for _, descriptor := range liveDescriptors(register) {
			if descriptor.output == unavailable {
				continue
			}
			if _, seen := byName[descriptor.Name]; !seen {
				order = append(order, descriptor.Name)
			}
			byName[descriptor.Name] = descriptor
		}
	}
	slices.Sort(order)
	out := make([]Descriptor, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}
	return out
}

// liveDescriptors runs one registration on its own registrar. A registration
// that cannot run with empty services contributes nothing.
func liveDescriptors(register func(*Registrar)) (descriptors []Descriptor) {
	defer func() {
		if recover() != nil {
			descriptors = nil
		}
	}()
	registrar := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "card-gallery", Version: "0.0.1"}, nil))
	register(registrar)
	return registrar.Descriptors()
}

// gallerySampleOutput fills a value of the output type with plausible data.
func gallerySampleOutput(t reflect.Type) any {
	if t == nil {
		return nil
	}
	return gallerySampleValue(t, "", 0, 0).Interface()
}

func gallerySampleValue(t reflect.Type, key string, depth, index int) reflect.Value {
	value := reflect.New(t).Elem()
	if depth > 5 {
		return value
	}
	switch t.Kind() {
	case reflect.Pointer:
		pointer := reflect.New(t.Elem())
		pointer.Elem().Set(gallerySampleValue(t.Elem(), key, depth+1, index))
		value.Set(pointer)
	case reflect.Struct:
		if t == reflect.TypeFor[time.Time]() {
			value.Set(reflect.ValueOf(time.Date(2026, 11, 9, 15, 0, 0, 0, time.UTC)))
			return value
		}
		for i := range t.NumField() {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if name == "-" {
				continue
			}
			if name == "" {
				name = strings.ToLower(field.Name)
			}
			value.Field(i).Set(gallerySampleValue(field.Type, name, depth+1, index))
		}
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return value
		}
		items := reflect.MakeSlice(t, 3, 3)
		for i := range 3 {
			items.Index(i).Set(gallerySampleValue(t.Elem(), key, depth+1, i))
		}
		value.Set(items)
	case reflect.String:
		value.SetString(gallerySampleString(key, index))
	case reflect.Bool:
		switch key {
		case "preview", "replayed", "truncated", "matched_existing", "deleted":
		default:
			value.SetBool(true)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt(3)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value.SetUint(3)
	case reflect.Float32, reflect.Float64:
		value.SetFloat(3)
	default:
		// Maps, interfaces and the rest stay zero.
	}
	return value
}

func gallerySampleString(key string, index int) string {
	pick := func(values ...string) string { return values[index%len(values)] }
	switch {
	case slices.Contains(cardNameKeys, key) && key != "slug" && key != "email":
		return pick("Support tools", "Sales CRM", "Billing API")
	case key == "slug" || key == "project_slug":
		return pick("support", "sales", "billing")
	case key == "email":
		return pick("ada@example.com", "grace@example.com", "alan@example.com")
	case slices.Contains(cardStatusKeys, key):
		return pick("ready", "pending", "ready")
	case key == "code", key == "next_cursor", key == "next_page_token", key == "preview_token":
		return ""
	case strings.HasSuffix(key, "_url") || key == "url":
		return "https://app.speakeasy.com/example"
	case key == "id" || strings.HasSuffix(key, "_id"):
		return fmt.Sprintf("3f2a9c1e-0000-4000-8000-00000000000%d", index)
	case slices.Contains(cardDescriptionKeys, key):
		return "A short sample description of this resource."
	case strings.HasSuffix(key, "_at"):
		return "2026-11-09T15:00:00Z"
	}
	return cmp.Or(cardHumanize(key), "sample")
}
