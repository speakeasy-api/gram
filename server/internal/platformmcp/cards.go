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
// The one exception is Logo: a data: image URI that cardLogoDataURI builds
// only from stored bytes it has checked, typed as template.URL so the filter
// lets it through.
//
// Every card is one header row — a leading tile, the title and a secondary
// line, and one trailing action — with optional label and value rows below a
// divider. A card without detail rows is the compact variant.
var cardTemplate = template.Must(template.New("card").Parse(`<article class="card">` +
	`<div class="head">` +
	`<div class="tile{{if .Logo}} tile-logo{{else if .Initials}} tile-initials{{end}}">` +
	`{{if .Logo}}<img class="logo" src="{{.Logo}}" alt="">{{else if .Initials}}<span aria-hidden="true">{{.Initials}}</span>{{else}}` + cardServerIcon + `{{end}}` +
	`{{with .Badge}}<span class="badge badge-{{.Tone}}" role="img" aria-label="{{.Label}}">{{if eq .Tone "done"}}` + cardCheckIcon + `{{else}}!{{end}}</span>{{end}}` +
	`</div>` +
	`<div class="text"><div class="title"><h1>{{.Title}}</h1>{{with .Pill}}<span class="pill pill-{{.Tone}}">{{.Label}}</span>{{end}}</div>` +
	`{{with .Status}}<p class="line"><span class="dot dot-{{.Tone}}" aria-hidden="true"></span>{{.Label}}</p>{{else}}{{with .Meta}}<p class="line">{{.}}</p>{{end}}{{end}}` +
	`</div>` +
	`{{with .Action}}<a class="button{{if .Secondary}} secondary{{end}}" href="{{.URL}}" target="_blank" rel="noopener noreferrer">{{.Label}}</a>{{end}}` +
	`</div>` +
	`{{with .Details}}<dl class="details">{{range .}}<div class="row"><dt>{{.Label}}</dt>` +
	`<dd>{{if .Copy}}<span class="field"><code>{{.Copy}}</code><button type="button" class="copy" data-copy="{{.Copy}}">Copy</button></span>{{else}}{{.Text}}{{end}}</dd></div>{{end}}</dl>{{end}}` +
	`</article>`))

// cardServerIcon is the line icon on an MCP server's tile.
const cardServerIcon = `<svg class="icon" viewBox="0 0 24 24" aria-hidden="true"><rect x="4" y="4" width="16" height="16" rx="2"/><path d="M8 9h8M8 12h8M8 15h8"/></svg>`

// cardCheckIcon is the check inside a done badge.
const cardCheckIcon = `<svg viewBox="0 0 12 12" aria-hidden="true"><path d="M3 6.2 5 8.2 9 4"/></svg>`

// resultCard is what one card shows. Every value except Logo is plain text;
// the template escapes it.
type resultCard struct {
	Title string
	// Logo is the MCP server's logo as a data: URI built by cardLogoDataURI
	// from stored, validated bytes. When it is set it fills the tile in place
	// of the initials or the server icon.
	Logo template.URL
	// Initials fill a dark tile, as on a sign-in card. Without them the tile
	// shows the MCP server icon.
	Initials string
	// Badge sits on the tile's corner and says whether the step is done or
	// waits for the user. Its label is read out by screen readers.
	Badge *cardStatus
	// Pill is the short uppercase tag after the title.
	Pill *cardStatus
	// Status is a coloured dot and a sentence under the title. When it is
	// absent, Meta is shown there instead.
	Status  *cardStatus
	Meta    string
	Action  *cardAction
	Details []cardDetail
}

// cardTone colours a badge, pill or status dot.
type cardTone string

const (
	// cardToneDone marks a step that has finished.
	cardToneDone cardTone = "done"
	// cardToneAction marks a step that waits for the user.
	cardToneAction cardTone = "action"
	// cardTonePending marks a step that has not started.
	cardTonePending cardTone = "pending"
	// cardToneNeutral is a tag that reports a fact, not a state.
	cardToneNeutral cardTone = "neutral"
)

// cardStatus is a short label and the tone it is drawn in.
type cardStatus struct {
	Label string
	Tone  cardTone
}

// cardDetail is one label and value row below the header. Copy, when set,
// replaces Text with a monospaced value and a Copy button.
type cardDetail struct {
	Label string
	Text  string
	Copy  string
}

// cardAction is the card's one link. Its URL must come from the tool result
// itself — a dashboard page or the authorization URL — never from elsewhere.
type cardAction struct {
	Label string
	URL   string

	// Secondary marks a fallback link, drawn as an outlined button instead of
	// a filled one.
	Secondary bool
}

// cardInitials is up to two letters for a tile: the first letter of each of
// the first two words.
func cardInitials(name string) string {
	var initials []rune
	for _, word := range strings.Fields(name) {
		first, _ := utf8.DecodeRuneInString(word)
		if first == utf8.RuneError || !unicode.IsLetter(first) && !unicode.IsDigit(first) {
			continue
		}
		initials = append(initials, unicode.ToUpper(first))
		if len(initials) == 2 {
			break
		}
	}
	return string(initials)
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
		// The card draws its own border and corners.
		Meta: mcp.Meta{"ui": map[string]any{"prefersBorder": false}},
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

// serverCard describes an MCP server just added to a project and its next
// step. serverURL is the address the server was added by, when there is one.
func serverCard(serverName string, logo template.URL, project ResolvedProject, nextAction, dashboardSetupURL string, alreadyRegistered bool, serverURL string) resultCard {
	projectName := project.Name
	if projectName == "" {
		projectName = project.Slug
	}
	pill := &cardStatus{Label: "Created", Tone: cardToneDone}
	if alreadyRegistered {
		pill = &cardStatus{Label: "Already added", Tone: cardToneNeutral}
	}
	card := resultCard{
		Title:    cardTitle(serverName),
		Logo:     logo,
		Initials: "",
		Badge:    &cardStatus{Label: "Added", Tone: cardToneDone},
		Pill:     pill,
		Status:   nil,
		Meta:     "MCP server · " + projectName + " · private until added to a plugin",
		Action:   nil,
		Details:  nil,
	}
	nextStep := "Check that it is ready to use"
	var action *cardAction
	switch nextAction {
	case nextActionConnectSignIn:
		card.Status = &cardStatus{Label: "Needs its sign-in connected before anyone can use it", Tone: cardToneAction}
		nextStep = "Connect its sign-in here in chat"
		action = &cardAction{Label: "Set up in dashboard", URL: dashboardSetupURL, Secondary: true}
	case nextActionContinueDashboardSetup:
		card.Status = &cardStatus{Label: "Needs sign-in before anyone can use it", Tone: cardToneAction}
		nextStep = "Finish sign-in in the dashboard"
		action = &cardAction{Label: "Finish setup", URL: dashboardSetupURL, Secondary: false}
	case nextActionSecureDashboardSetup:
		card.Status = &cardStatus{Label: "Needs credentials before anyone can use it", Tone: cardToneAction}
		nextStep = "Enter its credentials in the dashboard"
		action = &cardAction{Label: "Finish setup", URL: dashboardSetupURL, Secondary: false}
	}
	if card.Status != nil {
		card.Badge = &cardStatus{Label: "Needs action", Tone: cardToneAction}
		if dashboardSetupURL != "" {
			card.Action = action
		} else if nextAction != nextActionConnectSignIn {
			nextStep = "Finish setup from the MCP server's page in the dashboard"
		}
		// The status line replaces the meta line, so the project moves into
		// the rows.
		card.Details = append(card.Details, cardDetail{Label: "Project", Text: projectName, Copy: ""})
	}
	card.Details = append(card.Details, cardDetail{Label: "Next step", Text: nextStep, Copy: ""})
	if serverURL != "" {
		card.Details = append(card.Details, cardDetail{Label: "Server URL", Text: "", Copy: serverURL})
	}
	return card
}

// signInCard offers the one sign-in step after a provider is connected.
func signInCard(serverName string, logo template.URL, authorizationURL string) resultCard {
	card := resultCard{
		Title:    "Sign in",
		Logo:     logo,
		Initials: cardInitials(serverName),
		Badge:    nil,
		Pill:     nil,
		Status:   &cardStatus{Label: "Connect your account to continue", Tone: cardTonePending},
		Meta:     "",
		Action:   &cardAction{Label: "Sign in", URL: authorizationURL, Secondary: false},
		Details:  nil,
	}
	if serverName != "" {
		card.Title = "Sign in with " + serverName
		card.Status.Label = "Connect your " + serverName + " account to continue"
		card.Details = []cardDetail{{Label: "Requested by", Text: serverName, Copy: ""}}
	}
	return card
}

func cardTitle(serverName string) string {
	if serverName == "" {
		return "MCP server"
	}
	return serverName
}
