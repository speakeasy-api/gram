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
// Every card is a dotted panel holding the resource's tile, and beside it a
// coloured eyebrow, a serif title, one line below it, tags and one action,
// over a Speakeasy footer.
var cardTemplate = template.Must(template.New("card").Parse(`<article class="card">` +
	`<div class="body">` +
	`<div class="panel"><div class="tile{{if .Logo}} tile-logo{{end}}">` +
	`{{if .Logo}}<img class="logo" src="{{.Logo}}" alt="">{{else if .Initials}}<span aria-hidden="true">{{.Initials}}</span>{{else}}` + cardServerIcon + `{{end}}` +
	`</div></div>` +
	`<div class="text">` +
	`<p class="eyebrow eyebrow-{{.Eyebrow.Tone}}">{{.Eyebrow.Label}}</p>` +
	`<h1>{{.Title}}</h1>` +
	`{{with .Field}}<p class="line">{{.Label}} <code>{{.Value}}</code></p>{{else}}{{with .Description}}<p class="line">{{.}}</p>{{end}}{{end}}` +
	`<div class="foot">{{with .Tags}}<ul class="tags">{{range .}}<li>{{.}}</li>{{end}}</ul>{{end}}` +
	`{{with .Action}}<a class="button" href="{{.URL}}" target="_blank" rel="noopener noreferrer">{{.Label}} <span aria-hidden="true">&rarr;</span></a>{{end}}</div>` +
	`</div>` +
	`</div>` +
	`<footer class="brand">` + cardSpeakeasyMark + `<span>speakeasy</span></footer>` +
	`</article>`))

// cardServerIcon is the line icon on an MCP server's tile.
const cardServerIcon = `<svg class="icon" viewBox="0 0 24 24" aria-hidden="true"><rect x="9.5" y="3" width="5" height="5"/><rect x="4" y="16" width="5" height="5"/><rect x="15" y="16" width="5" height="5"/><path d="M12 8v4M6.5 16v-4h11v4"/></svg>`

// cardSpeakeasyMark is the Speakeasy mark in the card footer.
const cardSpeakeasyMark = `<svg class="mark" viewBox="72 75 156 150" aria-hidden="true"><path d="M181.781 215.464L81.7665 201.253L74.0996 207.903L181.781 223.198L225.901 184.921L218.244 183.837L181.781 215.464Z"/><path d="M181.781 192.261L225.901 153.984L218.244 152.899L181.781 184.517L146.095 179.45L97.0809 172.494L81.7665 170.315L74.0996 176.965L89.4237 179.134L74.0996 192.424L181.781 207.719L225.901 169.452L218.244 168.367L181.79 199.995L130.771 192.74L138.428 186.1L181.781 192.261Z"/><path d="M181.781 169.045L81.7665 154.834L74.0996 161.484L181.781 176.779L225.901 138.512L218.244 137.428L181.781 169.045Z"/><path d="M218.244 106.478L202.92 119.767L181.781 138.105L140.443 132.232L81.7665 123.894L74.0996 130.543L132.776 138.872L125.119 145.522L81.7569 139.362L74.0996 146.011L181.781 161.307L225.901 123.04L210.577 120.861L225.901 107.562L218.244 106.478Z"/><path d="M225.901 92.0971L118.22 76.8018L74.0996 115.069L181.781 130.374L225.901 92.0971Z"/></svg>`

// resultCard is what one card shows. Every value except Logo is plain text;
// the template escapes it.
type resultCard struct {
	// Eyebrow is the short coloured label above the title that says what
	// happened.
	Eyebrow cardStatus
	Title   string
	// Logo is the MCP server's logo as a data: URI built by cardLogoDataURI
	// from stored, validated bytes. When it is set it fills the tile in place
	// of the initials or the server icon.
	Logo template.URL
	// Initials fill the tile when there is no logo, as on a sign-in card.
	// Without either the tile shows the MCP server icon.
	Initials string
	// Field is a label and a monospaced value under the title. When it is
	// absent, Description is shown there instead.
	Field       *cardField
	Description string
	// Tags are short uppercase facts about the resource.
	Tags   []string
	Action *cardAction
}

// cardTone colours an eyebrow.
type cardTone string

const (
	// cardToneDone marks a step that has finished.
	cardToneDone cardTone = "done"
	// cardToneAction marks a step that waits for the user.
	cardToneAction cardTone = "action"
	// cardToneNeutral reports a fact, not a state.
	cardToneNeutral cardTone = "neutral"
)

// cardStatus is a short label and the tone it is drawn in.
type cardStatus struct {
	Label string
	Tone  cardTone
}

// cardField is a label and a monospaced value, such as a project slug.
type cardField struct {
	Label string
	Value string
}

// cardAction is the card's one link. Its URL must come from the tool result
// itself — a dashboard page or the authorization URL — never from elsewhere.
type cardAction struct {
	Label string
	URL   string
}

// cardInitials is up to two letters for a tile: the first letter of each of
// the first two words.
func cardInitials(name string) string {
	var initials []rune
	for word := range strings.FieldsSeq(name) {
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
	eyebrow := cardStatus{Label: "MCP server created", Tone: cardToneDone}
	if alreadyRegistered {
		eyebrow = cardStatus{Label: "MCP server already added", Tone: cardToneNeutral}
	}
	tag := "Catalog"
	if serverURL != "" {
		tag = "Remote"
	}
	card := resultCard{
		Eyebrow:     eyebrow,
		Title:       cardTitle(serverName),
		Logo:        logo,
		Initials:    "",
		Field:       &cardField{Label: "Project", Value: project.Slug},
		Description: "",
		Tags:        []string{tag},
		Action:      nil,
	}
	label := ""
	switch nextAction {
	case nextActionConnectSignIn:
		label = "Connect"
	case nextActionContinueDashboardSetup:
		label = "Finish setup"
	case nextActionSecureDashboardSetup:
		label = "Add credentials"
	}
	switch {
	case label != "" && dashboardSetupURL != "":
		card.Action = &cardAction{Label: label, URL: dashboardSetupURL}
	case label != "":
		// Setup is still needed but there is no link to it: say where to
		// finish instead of showing the project.
		card.Eyebrow = cardStatus{Label: "Setup required", Tone: cardToneAction}
		card.Field = nil
		card.Description = "Finish setup from the MCP server's page in the dashboard."
	}
	return card
}

// signInCard offers the one sign-in step after a provider is connected.
func signInCard(serverName string, logo template.URL, authorizationURL string) resultCard {
	card := resultCard{
		Eyebrow:     cardStatus{Label: "Sign-in required", Tone: cardToneAction},
		Title:       "Sign in",
		Logo:        logo,
		Initials:    cardInitials(serverName),
		Field:       nil,
		Description: "Let this MCP server act on your behalf.",
		Tags:        []string{"OAuth"},
		Action:      &cardAction{Label: "Sign in", URL: authorizationURL},
	}
	if serverName != "" {
		card.Title = "Sign in to " + serverName
		card.Description = "Let this MCP server use " + serverName + " on your behalf."
	}
	return card
}

func cardTitle(serverName string) string {
	if serverName == "" {
		return "MCP server"
	}
	return serverName
}
