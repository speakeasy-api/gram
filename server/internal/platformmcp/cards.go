package platformmcp

import (
	"context"
	_ "embed"
	"html/template"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Result cards let a client that renders MCP Apps show an inline card next to
// a tool's result. The card is rendered and escaped here and carried in the
// result's _meta, so the text and structured content every other client reads
// stay exactly as they were.

const (
	// cardResourceURI is the one MCP Apps view every result card renders in.
	cardResourceURI = "ui://speakeasy/platform-card.html"
	// cardMIMEType marks an HTML resource as an MCP Apps view.
	cardMIMEType = "text/html;profile=mcp-app"
	// cardMetaKey is the result _meta key that carries the rendered card.
	cardMetaKey = "com.speakeasy/card"
)

//go:embed card.html
var cardShellHTML string

// cardTemplate is the markup shared by every card. html/template escapes each
// value for its context and replaces any URL that is not http(s) or mailto.
var cardTemplate = template.Must(template.New("card").Parse(`<article class="card">` +
	`<header><span class="mark" aria-hidden="true">{{.Initial}}</span><div class="heading"><h1>{{.Title}}</h1>{{with .Subtitle}}<p class="sub">{{.}}</p>{{end}}</div>` +
	`{{with .Status}}<span class="status status-{{.Tone}}">{{.Label}}</span>{{end}}</header>` +
	`{{with .Note}}<p class="note">{{.}}</p>{{end}}` +
	`{{with .NextStep}}<p class="next">{{.}}</p>{{end}}` +
	`{{with .Action}}<a class="action{{if .Secondary}} secondary{{end}}" href="{{.URL}}" target="_blank" rel="noopener noreferrer">{{.Label}}</a>{{end}}` +
	`</article>`))

// resultCard is what one card shows. Every value is plain text; the template
// escapes it.
type resultCard struct {
	Title    string
	Subtitle string
	Status   *cardStatus
	Note     string
	NextStep string
	Action   *cardAction
}

// cardTone colours a card's status.
type cardTone string

const (
	// cardToneDone marks a step that has finished.
	cardToneDone cardTone = "done"
	// cardToneAction marks a step that waits for the user.
	cardToneAction cardTone = "action"
)

// cardStatus is the short state shown next to a card's title.
type cardStatus struct {
	Label string
	Tone  cardTone
}

// cardAction is the card's one link. Its URL must come from the tool result
// itself — a dashboard page or the authorization URL — never from elsewhere.
type cardAction struct {
	Label string
	URL   string

	// Secondary marks a fallback link, drawn less prominently than a primary
	// action.
	Secondary bool
}

// Initial is the letter shown in place of a logo.
func (c resultCard) Initial() string {
	first, _ := utf8.DecodeRuneInString(strings.TrimSpace(c.Title))
	if first == utf8.RuneError || !unicode.IsGraphic(first) {
		return ""
	}
	return string(unicode.ToUpper(first))
}

// cardToolMeta links a tool to the card view, under the MCP Apps key and the
// ChatGPT compatibility alias for it.
func cardToolMeta() mcp.Meta {
	return mcp.Meta{
		"ui":                    map[string]any{"resourceUri": cardResourceURI},
		"openai/outputTemplate": cardResourceURI,
	}
}

// cardResult is a tool result that carries the card in _meta. The SDK still
// fills in the text and structured content from the handler's output.
func cardResult(card resultCard) *mcp.CallToolResult {
	var rendered strings.Builder
	if err := cardTemplate.Execute(&rendered, card); err != nil {
		// A card is decoration on a result that has already succeeded.
		return nil
	}
	// The SDK fills Content and StructuredContent from the handler's output.
	return &mcp.CallToolResult{Meta: mcp.Meta{cardMetaKey: rendered.String()}, Content: nil, StructuredContent: nil, IsError: false, InputRequests: nil, RequestState: ""}
}

// registerCardResource serves the card view to external clients. The managed
// assistant renders no MCP Apps, so it is not an audience.
func registerCardResource(reg *Registrar) {
	addResource(reg, &mcp.Resource{ //nolint:exhaustruct // MCP SDK annotations and icons are intentionally omitted.
		URI:         cardResourceURI,
		Name:        "platform-card",
		Title:       "Speakeasy result card",
		Description: "Inline card for MCP servers added to a project and for sign-in steps.",
		MIMEType:    cardMIMEType,
		// No csp: the card loads nothing from the network, so the host's
		// restrictive default applies.
		Meta: mcp.Meta{"ui": map[string]any{"prefersBorder": true}},
	}, ResourceMeta{
		Authorization: ExternalAuthorizationMember,
		Audiences:     []Audience{AudienceExternal},
	}, func(context.Context) (string, error) {
		return cardShellHTML, nil
	})
}

const (
	// nextActionStartSetup leaves the user to check the new server's readiness.
	nextActionStartSetup = "start_setup"
	// nextActionContinueDashboardSetup sends the user to the dashboard to sign
	// in to a provider that needs a browser.
	nextActionContinueDashboardSetup = "continue_dashboard_setup"
	// nextActionSecureDashboardSetup sends the user to the dashboard to enter
	// secrets that must never pass through chat.
	nextActionSecureDashboardSetup = "secure_dashboard_setup_required"
	// nextActionConnectSignIn says the server's sign-in can be connected in
	// chat, with the dashboard setup link as the fallback.
	nextActionConnectSignIn = "connect_sign_in"
)

// serverCard describes an MCP server just added to a project and its next step.
func serverCard(serverName string, project ResolvedProject, nextAction, dashboardSetupURL string) resultCard {
	projectName := project.Name
	if projectName == "" {
		projectName = project.Slug
	}
	card := resultCard{
		Title:    cardTitle(serverName),
		Subtitle: "Added to project " + projectName,
		Status:   &cardStatus{Label: "Added", Tone: cardToneDone},
		Note:     "Private until added to a plugin",
		NextStep: "Next: check that it is ready to use.",
		Action:   nil,
	}
	var action *cardAction
	switch nextAction {
	case nextActionConnectSignIn:
		card.Status = &cardStatus{Label: "Connect sign-in", Tone: cardToneAction}
		card.NextStep = "Next: connect its sign-in here in chat."
		action = &cardAction{Label: "Set up in dashboard", URL: dashboardSetupURL, Secondary: true}
	case nextActionContinueDashboardSetup:
		card.Status = &cardStatus{Label: "Sign-in needed", Tone: cardToneAction}
		card.NextStep = "Next: finish sign-in in the dashboard."
		action = &cardAction{Label: "Finish sign-in", URL: dashboardSetupURL, Secondary: false}
	case nextActionSecureDashboardSetup:
		card.Status = &cardStatus{Label: "Setup needed", Tone: cardToneAction}
		card.NextStep = "Next: enter its credentials in the dashboard."
		action = &cardAction{Label: "Open dashboard setup", URL: dashboardSetupURL, Secondary: false}
	default:
		return card
	}
	if dashboardSetupURL != "" {
		card.Action = action
	} else if nextAction != nextActionConnectSignIn {
		card.NextStep = "Next: finish setup from the MCP server's page in the dashboard."
	}
	return card
}

// signInCard offers the one sign-in step after a provider is connected.
func signInCard(serverName, authorizationURL string) resultCard {
	label := "Sign in"
	if serverName != "" {
		label = "Sign in to " + serverName
	}
	return resultCard{
		Title:    cardTitle(serverName),
		Subtitle: "Connected to its sign-in provider",
		Status:   &cardStatus{Label: "Sign-in needed", Tone: cardToneAction},
		Note:     "",
		NextStep: "Sign in to finish connecting it, then check that it is ready to use.",
		Action:   &cardAction{Label: label, URL: authorizationURL, Secondary: false},
	}
}

func cardTitle(serverName string) string {
	if serverName == "" {
		return "MCP server"
	}
	return serverName
}
