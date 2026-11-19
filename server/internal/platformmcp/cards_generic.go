package platformmcp

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"maps"
	"net/url"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Generic result cards. Every tool in cardTools links the card view, and after
// a call addTool builds one of four shared cards from the result the client
// already receives: a detail card for one resource, a list card, a receipt for
// a change, and a refusal card for a call that did not complete. Each card
// links to its dashboard page. A tool that builds its own card keeps it.
//
// A generic card shows only values under the named keys below. It never
// renders an arbitrary field, so a credential or hidden-resource signal in a
// result cannot reach the card even if a future output adds one.

// cardKind picks which shared card a tool's results are drawn as.
type cardKind string

const (
	cardKindDetail  cardKind = "detail"
	cardKindList    cardKind = "list"
	cardKindReceipt cardKind = "receipt"
)

// toolCardSpec is a tool's card spec, and false for a text-only tool.
func toolCardSpec(name string) (cardSpec, bool) {
	spec, ok := cardTools[name]
	return spec, ok
}

// withCardToolMeta links a tool to the card view without dropping any _meta
// the tool already declares.
func withCardToolMeta(meta mcp.Meta) mcp.Meta {
	if _, ok := meta["ui"]; ok {
		return meta
	}
	linked := maps.Clone(meta)
	if linked == nil {
		linked = mcp.Meta{}
	}
	maps.Copy(linked, cardToolMeta())
	return linked
}

// withResultCard adds a generic card to a result that has none. It never
// changes the result's content, structured content or error flag, and it
// returns a copy rather than editing a result the handler may share.
func withResultCard(ctx context.Context, links cardLinks, title string, spec cardSpec, result *mcp.CallToolResult, output, input any) *mcp.CallToolResult {
	if result != nil {
		if _, ok := result.Meta[cardMetaKey]; ok {
			return result
		}
	}
	card, ok := genericCard(ctx, links, title, spec, result, output, input)
	if !ok {
		return result
	}
	rendered, ok := renderResultCard(card)
	if !ok {
		return result
	}
	if result == nil {
		// The SDK fills Content and StructuredContent from the handler's output.
		return &mcp.CallToolResult{Meta: mcp.Meta{cardMetaKey: rendered}, Content: nil, StructuredContent: nil, IsError: false, InputRequests: nil, RequestState: ""}
	}
	carded := *result
	carded.Meta = maps.Clone(result.Meta)
	if carded.Meta == nil {
		carded.Meta = mcp.Meta{}
	}
	carded.Meta[cardMetaKey] = rendered
	return &carded
}

// genericCard reads the same JSON the client receives: the structured content
// when the handler set it, else its text, else the typed output.
func genericCard(ctx context.Context, links cardLinks, title string, spec cardSpec, result *mcp.CallToolResult, output, input any) (resultCard, bool) {
	source := output
	switch {
	case result != nil && result.StructuredContent != nil:
		source = result.StructuredContent
	case result != nil && len(result.Content) > 0:
		if text, ok := result.Content[0].(*mcp.TextContent); ok {
			source = json.RawMessage(text.Text)
		}
	}
	var data []byte
	if raw, ok := source.(json.RawMessage); ok {
		data = raw
	} else {
		encoded, err := json.Marshal(source)
		if err != nil {
			return noResultCard, false
		}
		data = encoded
	}
	decoded, err := decodeOrderedJSON(data)
	if err != nil {
		if result != nil && result.IsError {
			// A plain-text refusal is its own message.
			return refusalCard(title, jsonObject{{Key: "message", Value: strings.TrimSpace(string(data))}}), true
		}
		return noResultCard, false
	}
	object, _ := decoded.(jsonObject)
	if result != nil && result.IsError {
		return refusalCard(title, object), true
	}
	if isUnavailableResult(object) {
		return unavailableCard(title, object), true
	}
	var card resultCard
	switch spec.kind {
	case cardKindList:
		listed, ok := listCard(title, object)
		if !ok {
			listed = detailCard(title, object)
		}
		card = listed
	case cardKindReceipt:
		card = receiptCard(title, object)
	default:
		card = detailCard(title, object)
		// A read that names no single resource but returns a list, such as
		// a member's MCP servers, reads better as that list.
		if cardSubject(object).firstString(cardNameKeys) == "" {
			if listed, ok := listCard(title, object); ok {
				card = listed
			}
		}
	}
	// A link the result itself returns, such as a setup or sign-in URL, is
	// the next step and wins over the page link.
	if card.Action == nil {
		card.Action = links.link(ctx, spec, object, decodeCardInput(input))
	}
	return card, true
}

// decodeCardInput is the call's arguments as an ordered object, for filling
// route segments the result does not repeat.
func decodeCardInput(input any) jsonObject {
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil
	}
	decoded, err := decodeOrderedJSON(encoded)
	if err != nil {
		return nil
	}
	object, _ := decoded.(jsonObject)
	return object
}

// noResultCard is returned with false when no card can be drawn.
var noResultCard resultCard

// The keys a generic card reads, in priority order.
var (
	cardNameKeys = []string{"display_name", "name", "title", "mcp_name", "plugin_name", "skill_name", "widget_name", "slug", "email", "masked_identity"}
	// cardRowLabelKeys label a list row that has no name.
	cardRowLabelKeys    = append(slices.Clone(cardNameKeys), "tool_name", "label", "event_type", "type", "kind", "key")
	cardStatusKeys      = []string{"outcome", "status", "state", "enabled_state", "visibility", "decision", "result_category", "lifecycle", "severity", "mode"}
	cardDescriptionKeys = []string{"description", "summary", "next_step", "caveat"}
	cardActionKeys      = []cardActionKey{
		{key: "setup_url", label: "Finish setup"},
		{key: "management_url", label: "Manage"},
		{key: "request_access_url", label: "Request access"},
		{key: "open_authorization_url", label: "Sign in"},
		{key: "authorization_url", label: "Sign in"},
		{key: "url", label: "Open"},
	}
)

// cardActionKey is a result key whose URL may become the card's one link.
type cardActionKey struct {
	key   string
	label string
}

const (
	maxCardTags        = 3
	maxCardRows        = 5
	maxCardTextRunes   = 180
	maxCardRowRunes    = 64
	maxCardSearchDepth = 2
)

func detailCard(title string, object jsonObject) resultCard {
	subject := cardSubject(object)
	name := subject.firstString(cardNameKeys)
	card := resultCard{
		Eyebrow:     cardStatus{Label: title, Tone: cardToneNeutral},
		Title:       cmp.Or(name, title),
		Logo:        "",
		Initials:    cardInitials(name),
		Icon:        "",
		Field:       cardProjectField(object, subject),
		Description: cardTruncate(cmp.Or(subject.firstString(cardDescriptionKeys), object.firstString(cardDescriptionKeys)), maxCardTextRunes),
		Tags:        cardTags(subject, object),
		Rows:        nil,
		Action:      cardActionFrom(object, subject),
	}
	if name == "" {
		// The title already names the tool; the eyebrow says what this is.
		card.Eyebrow = cardStatus{Label: "Details", Tone: cardToneNeutral}
		card.Icon = cardDetailIcon
	}
	return card
}

func listCard(title string, object jsonObject) (resultCard, bool) {
	key, items, ok := object.firstArray()
	if !ok {
		return noResultCard, false
	}
	card := resultCard{
		Eyebrow:     cardStatus{Label: title, Tone: cardToneNeutral},
		Title:       cardCount(len(items), key),
		Logo:        "",
		Initials:    "",
		Icon:        cardListIcon,
		Field:       cardProjectField(object, object),
		Description: "",
		Tags:        nil,
		Rows:        nil,
		Action:      cardActionFrom(object, object),
	}
	for _, item := range items {
		if len(card.Rows) == maxCardRows {
			break
		}
		switch value := item.(type) {
		case string:
			card.Rows = append(card.Rows, cardRow{Label: cardTruncate(value, maxCardRowRunes), Meta: ""})
		case jsonObject:
			label := value.firstString(cardRowLabelKeys)
			if label == "" {
				continue
			}
			card.Rows = append(card.Rows, cardRow{Label: cardTruncate(label, maxCardRowRunes), Meta: cardHumanize(value.firstString(cardStatusKeys))})
		}
	}
	more := len(items) - len(card.Rows)
	truncated, _ := object.get("truncated")
	cursor := object.firstString([]string{"next_cursor", "next_page_token"})
	switch {
	case len(items) == 0:
		card.Description = "Nothing matched."
	case truncated == true || cursor != "":
		card.Description = "More results are available."
	case more > 0 && len(card.Rows) > 0:
		card.Description = fmt.Sprintf("And %d more.", more)
	}
	return card, true
}

func receiptCard(title string, object jsonObject) resultCard {
	card := detailCard(title, object)
	subject := cardSubject(object)
	name := subject.firstString(cardNameKeys)
	card.Eyebrow = cardStatus{Label: "Done", Tone: cardToneDone}
	if name != "" {
		// The title names the resource, so the line under it says what was
		// done to it.
		card.Description = title
	}
	if name == "" {
		card.Icon = cardDoneIcon
	}
	preview, _ := object.get("preview")
	replayed := false
	if receipt, ok := object.object("receipt"); ok {
		value, _ := receipt.get("replayed")
		replayed = value == true
	}
	matched, _ := object.get("matched_existing")
	outcome := object.firstString([]string{"outcome"})
	switch {
	case preview == true:
		card.Eyebrow = cardStatus{Label: "Preview, not applied", Tone: cardToneAction}
	case replayed || matched == true || outcome == "unchanged" || outcome == "no_change" || strings.HasPrefix(outcome, "already"):
		card.Eyebrow = cardStatus{Label: "No change", Tone: cardToneNeutral}
	case slices.Contains(notAcceptedOutcomes, outcome):
		card.Eyebrow = cardStatus{Label: "Not accepted", Tone: cardToneAction}
		card.Icon = cardAlertIcon
		card.Description = cardTruncate(cmp.Or(object.firstString([]string{"message"}), card.Description), maxCardTextRunes)
	}
	return card
}

// notAcceptedOutcomes are the outcomes of a test event the destination did not accept.
var notAcceptedOutcomes = []string{"refused", "not_found", "unreachable", "error"}

// refusalCard shows the readable reason a call did not complete. The message
// is the one the tool already returned to the client.
func refusalCard(title string, object jsonObject) resultCard {
	message := object.firstString([]string{"message"})
	if message == "" {
		message = "This request did not complete."
	}
	return resultCard{
		Eyebrow:     cardStatus{Label: "Not completed", Tone: cardToneAction},
		Title:       title,
		Logo:        "",
		Initials:    "",
		Icon:        cardAlertIcon,
		Field:       nil,
		Description: cardTruncate(message, maxCardTextRunes),
		Tags:        nil,
		Rows:        nil,
		Action:      cardActionFrom(object, object),
	}
}

// unavailableCard explains a tool that is registered but not available on
// this deployment or rollout.
func unavailableCard(title string, object jsonObject) resultCard {
	card := refusalCard(title, object)
	card.Eyebrow = cardStatus{Label: "Unavailable", Tone: cardToneAction}
	return card
}

// isUnavailableResult matches featureUnavailableResult and the other
// code-and-message refusals tools return as their output.
func isUnavailableResult(object jsonObject) bool {
	if object.firstString([]string{"code"}) == "" || object.firstString([]string{"message"}) == "" {
		return false
	}
	for _, field := range object {
		switch field.Key {
		case "code", "message", "feature", "required_scope", "request_access_url", "retry_after_seconds":
		default:
			return false
		}
	}
	return true
}

// cardSubject is the object a result is about: the top level when it names
// something, else the first nested object that does.
func cardSubject(object jsonObject) jsonObject {
	if found, ok := findNamedObject(object, 0); ok {
		return found
	}
	return object
}

func findNamedObject(object jsonObject, depth int) (jsonObject, bool) {
	if object.firstString(cardNameKeys) != "" {
		return object, true
	}
	if depth == maxCardSearchDepth {
		return nil, false
	}
	// The owning project is context, so it is the subject only when nothing
	// else in the result names one. A receipt never is.
	var project jsonObject
	for _, field := range object {
		nested, ok := field.Value.(jsonObject)
		switch {
		case !ok || field.Key == "receipt":
			continue
		case field.Key == "project":
			project = nested
			continue
		}
		if found, ok := findNamedObject(nested, depth+1); ok {
			return found, true
		}
	}
	// A project counts only when it has a display name, not just a slug.
	if project != nil && project.firstString([]string{"display_name", "name"}) != "" {
		return project, true
	}
	return nil, false
}

func cardProjectField(object, subject jsonObject) *cardField {
	for _, candidate := range []jsonObject{subject, object} {
		if slug := candidate.firstString([]string{"project_slug"}); slug != "" {
			return &cardField{Label: "Project", Value: slug}
		}
		if project, ok := candidate.object("project"); ok {
			if slug := project.firstString([]string{"slug"}); slug != "" {
				return &cardField{Label: "Project", Value: slug}
			}
		}
	}
	return nil
}

func cardTags(objects ...jsonObject) []string {
	var tags []string
	seen := map[string]bool{}
	for _, object := range objects {
		for _, key := range cardStatusKeys {
			value := cardHumanize(object.firstString([]string{key}))
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			tags = append(tags, cardTruncate(value, maxCardRowRunes))
			if len(tags) == maxCardTags {
				return tags
			}
		}
	}
	return tags
}

// cardActionFrom is the first allowed link in the result. Only absolute https
// URLs qualify, so a card never opens a page over plaintext HTTP.
func cardActionFrom(objects ...jsonObject) *cardAction {
	for _, object := range objects {
		for _, action := range cardActionKeys {
			raw := object.firstString([]string{action.key})
			if raw == "" {
				continue
			}
			parsed, err := url.Parse(raw)
			if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
				continue
			}
			return &cardAction{Label: action.label, URL: raw}
		}
	}
	return nil
}

// cardCount is "3 plugins" from the result's list key.
func cardCount(count int, key string) string {
	noun := cardHumanize(key)
	if count == 1 {
		if stem, ok := strings.CutSuffix(noun, "ies"); ok {
			noun = stem + "y"
		} else {
			noun = strings.TrimSuffix(noun, "s")
		}
	}
	if noun == "" {
		noun = "results"
	}
	return fmt.Sprintf("%d %s", count, noun)
}

// cardHumanize turns a snake_case key or enum value into words.
func cardHumanize(value string) string {
	words := strings.Fields(strings.ReplaceAll(value, "_", " "))
	for i, word := range words {
		if spelled, ok := cardWordSpelling[word]; ok {
			words[i] = spelled
		}
	}
	return strings.Join(words, " ")
}

// cardFallbackTitle is a readable title for a tool that declares none.
func cardFallbackTitle(name string) string {
	words := cardHumanize(name)
	first, size := utf8.DecodeRuneInString(words)
	if size == 0 {
		return words
	}
	return string(unicode.ToUpper(first)) + words[size:]
}

// cardWordSpelling keeps acronyms and product words readable.
var cardWordSpelling = map[string]string{
	"mcp": "MCP", "mcps": "MCPs", "ai": "AI", "ema": "EMA", "xaa": "XAA",
	"oauth": "OAuth", "url": "URL", "urls": "URLs", "id": "ID", "ids": "IDs",
}

func cardTruncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

// Tile icons for cards that have no name to draw initials from.
const (
	cardListIcon   template.HTML = `<svg class="icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M9 6h11M9 12h11M9 18h11"/><rect x="4" y="5" width="2" height="2"/><rect x="4" y="11" width="2" height="2"/><rect x="4" y="17" width="2" height="2"/></svg>`
	cardDetailIcon template.HTML = `<svg class="icon" viewBox="0 0 24 24" aria-hidden="true"><rect x="4" y="4" width="16" height="16"/><path d="M8 9h8M8 13h8M8 17h5"/></svg>`
	cardDoneIcon   template.HTML = `<svg class="icon" viewBox="0 0 24 24" aria-hidden="true"><rect x="4" y="4" width="16" height="16"/><path d="M8 12.5l3 3 5-6"/></svg>`
	cardAlertIcon  template.HTML = `<svg class="icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 4l9 16H3z"/><path d="M12 10v4M12 17v.5"/></svg>`
)

// jsonObject is a decoded JSON object that keeps its key order, so a card
// reads fields in the order the result declares them.
type jsonObject []jsonField

type jsonField struct {
	Key   string
	Value any
}

func (o jsonObject) get(key string) (any, bool) {
	for _, field := range o {
		if field.Key == key {
			return field.Value, true
		}
	}
	return nil, false
}

func (o jsonObject) object(key string) (jsonObject, bool) {
	value, _ := o.get(key)
	object, ok := value.(jsonObject)
	return object, ok
}

func (o jsonObject) firstString(keys []string) string {
	for _, key := range keys {
		if value, ok := o.get(key); ok {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
}

// firstArray is the first top-level list in the result, preferring a list of
// objects over a list of strings.
func (o jsonObject) firstArray() (string, []any, bool) {
	fallbackKey, fallback, found := "", []any(nil), false
	for _, field := range o {
		items, ok := field.Value.([]any)
		if !ok {
			continue
		}
		if len(items) > 0 {
			if _, ok := items[0].(jsonObject); ok {
				return field.Key, items, true
			}
		}
		if !found {
			fallbackKey, fallback, found = field.Key, items, true
		}
	}
	return fallbackKey, fallback, found
}

func decodeOrderedJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decodeOrderedValue(decoder)
}

func decodeOrderedValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("read json token: %w", err)
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	switch delim {
	case '{':
		object := jsonObject{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, fmt.Errorf("read json key: %w", err)
			}
			key, _ := keyToken.(string)
			value, err := decodeOrderedValue(decoder)
			if err != nil {
				return nil, err
			}
			object = append(object, jsonField{Key: key, Value: value})
		}
		if _, err := decoder.Token(); err != nil {
			return nil, fmt.Errorf("close json object: %w", err)
		}
		return object, nil
	case '[':
		items := []any{}
		for decoder.More() {
			value, err := decodeOrderedValue(decoder)
			if err != nil {
				return nil, err
			}
			items = append(items, value)
		}
		if _, err := decoder.Token(); err != nil {
			return nil, fmt.Errorf("close json array: %w", err)
		}
		return items, nil
	default:
		return nil, fmt.Errorf("unexpected json delimiter %q", delim)
	}
}
