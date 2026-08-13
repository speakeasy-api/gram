package agentcapture

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

const saltSize = 32

// loadOrCreateSalt returns the hex-encoded salt stored at path, creating a
// fresh random one on first use. Reusing the salt keeps pseudonyms stable
// across repeated captures into the same directory, so identities keep their
// join structure between dump files.
func loadOrCreateSalt(path string) ([]byte, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- the salt lives inside the operator-chosen output directory
	if err == nil {
		salt, decErr := hex.DecodeString(strings.TrimSpace(string(raw)))
		if decErr != nil {
			return nil, fmt.Errorf("decode anonymization salt %s: %w", path, decErr)
		}
		return salt, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read anonymization salt: %w", err)
	}

	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate anonymization salt: %w", err)
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(salt)+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("write anonymization salt: %w", err)
	}
	return salt, nil
}

// Anonymizer deterministically pseudonymizes provider-side identities and
// scrubs free-text content in dumped telemetry rows. The same salt maps the
// same input to the same output, preserving per-user grouping and natural
// keys without exposing the original values.
//
// Gram-internal identifiers (local user, project, session, chat IDs) are
// deliberately left intact: they are opaque UUIDs minted by the local dev
// stack and are needed to join the dump against local Postgres fixtures.
type Anonymizer struct {
	salt []byte
}

func NewAnonymizer(salt []byte) *Anonymizer {
	return &Anonymizer{salt: salt}
}

// identityPaths are dotted attribute paths whose string values name a person
// or organization on the provider's side. Values are replaced with a
// deterministic pseudonym; UUID-shaped values stay UUID-shaped so downstream
// parsers keep working. Email-bearing keys are matched by shape instead (see
// isEmailKey).
var identityPaths = map[string]struct{}{
	"user.account_uuid":     {},
	"organization.id":       {},
	"gram.external_user.id": {},
	"gram.external_org_id":  {},
}

// contentPaths are dotted attribute paths that carry free text: prompts,
// completions, and tool IO. Values are replaced with a length-stamped
// placeholder so row shape and rough payload size survive.
var contentPaths = map[string]struct{}{
	"prompt":                     {},
	"gen_ai.prompt":              {},
	"gen_ai.completion":          {},
	"gen_ai.tool.call.arguments": {},
	"gen_ai.tool.call.result":    {},
}

// maxClearBodyLen bounds how long a body may be before it is scrubbed. Real
// telemetry bodies are short event descriptors ("Claude Chat cost metrics",
// "user_prompt"); anything longer is almost certainly free text that leaked
// into the body.
const maxClearBodyLen = 200

// ScrubRow anonymizes one dumped row in place: both JSON attribute blobs are
// walked with the path rules and an over-long body is replaced with a
// placeholder.
func (a *Anonymizer) ScrubRow(row *logRow) error {
	attrs, err := a.ScrubJSON(string(row.Attributes))
	if err != nil {
		return fmt.Errorf("scrub attributes: %w", err)
	}
	row.Attributes = json.RawMessage(attrs)

	res, err := a.ScrubJSON(string(row.ResourceAttributes))
	if err != nil {
		return fmt.Errorf("scrub resource attributes: %w", err)
	}
	row.ResourceAttributes = json.RawMessage(res)

	if len(row.Body) > maxClearBodyLen {
		row.Body = scrubPlaceholder(row.Body)
	}
	return nil
}

// ScrubChat anonymizes one exported external chat in place: the title is
// user-authored free text and the external user ID is a provider-side
// identity. Gram-internal IDs (chat, project, connected user) stay intact
// for local joins, and the external chat ID is a content natural key, not a
// person, so it survives too.
func (a *Anonymizer) ScrubChat(row *chatExportRow) {
	if row.Title != nil && *row.Title != "" {
		row.Title = conv.PtrEmpty(scrubPlaceholder(*row.Title))
	}
	if row.ExternalUserID != nil && *row.ExternalUserID != "" {
		row.ExternalUserID = conv.PtrEmpty(a.pseudonymID(*row.ExternalUserID))
	}
}

// ScrubChatMessage anonymizes one exported external chat message in place:
// content is the transcript text, the user agent and IP address describe a
// person's device, and the external user ID is a provider-side identity. The
// external message ID is the row's natural key and stays.
func (a *Anonymizer) ScrubChatMessage(row *chatMessageExportRow) {
	if row.Content != "" {
		row.Content = scrubPlaceholder(row.Content)
	}
	if row.ExternalUserID != nil && *row.ExternalUserID != "" {
		row.ExternalUserID = conv.PtrEmpty(a.pseudonymID(*row.ExternalUserID))
	}
	if row.UserAgent != nil && *row.UserAgent != "" {
		row.UserAgent = conv.PtrEmpty(scrubPlaceholder(*row.UserAgent))
	}
	if row.IPAddress != nil && *row.IPAddress != "" {
		row.IPAddress = conv.PtrEmpty(a.pseudonymID(*row.IPAddress))
	}
}

// ScrubJSON walks a JSON object and applies the anonymization rules to every
// string leaf, keyed by the leaf's dotted path. ClickHouse's JSON type stores
// dotted attribute keys as nested objects, so "user.email" arrives as
// {"user":{"email":...}} and the walk reassembles the dotted path.
func (a *Anonymizer) ScrubJSON(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return raw, nil
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return "", fmt.Errorf("parse attribute json: %w", err)
	}
	a.walkMap(root, "")
	out, err := json.Marshal(root)
	if err != nil {
		return "", fmt.Errorf("serialize scrubbed json: %w", err)
	}
	return string(out), nil
}

func (a *Anonymizer) walkMap(node map[string]any, prefix string) {
	for key, value := range node {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		node[key] = a.walkValue(path, key, value)
	}
}

func (a *Anonymizer) walkValue(path, key string, value any) any {
	switch val := value.(type) {
	case map[string]any:
		a.walkMap(val, path)
		return val
	case []any:
		for i, item := range val {
			val[i] = a.walkValue(path, key, item)
		}
		return val
	case string:
		return a.scrubString(path, key, val)
	default:
		return value
	}
}

func (a *Anonymizer) scrubString(path, key, value string) string {
	if value == "" {
		return value
	}
	if _, ok := contentPaths[path]; ok {
		return scrubPlaceholder(value)
	}
	if isEmailKey(key) {
		return a.pseudonymEmail(value)
	}
	if _, ok := identityPaths[path]; ok {
		return a.pseudonymID(value)
	}
	return value
}

// isEmailKey reports whether a leaf key names an email address, whatever its
// path — every email in a telemetry row is a person and must be
// pseudonymized.
func isEmailKey(key string) bool {
	return key == "email" || strings.HasSuffix(key, "_email") || strings.HasSuffix(key, ".email")
}

func scrubPlaceholder(value string) string {
	return fmt.Sprintf("[scrubbed %d bytes]", len(value))
}

func (a *Anonymizer) pseudonymEmail(value string) string {
	return fmt.Sprintf("user-%s@anon.invalid", hex.EncodeToString(a.mac("email", value)[:6]))
}

// pseudonymID maps an identifier to a stable pseudonym. UUID-shaped inputs
// produce a derived (version 4, RFC 4122 variant) UUID so consumers that
// parse the field keep working; everything else gets an anon-<hex> handle.
func (a *Anonymizer) pseudonymID(value string) string {
	sum := a.mac("id", value)
	if _, err := uuid.Parse(value); err == nil {
		var out uuid.UUID
		copy(out[:], sum[:16])
		out[6] = (out[6] & 0x0f) | 0x40
		out[8] = (out[8] & 0x3f) | 0x80
		return out.String()
	}
	return "anon-" + hex.EncodeToString(sum[:8])
}

// mac derives the pseudonym bytes for one value. The kind prefix domain-
// separates rule families so an email and an ID with the same raw value do
// not share a pseudonym.
func (a *Anonymizer) mac(kind, value string) []byte {
	h := hmac.New(sha256.New, a.salt)
	h.Write([]byte(kind))
	h.Write([]byte{0})
	h.Write([]byte(value))
	return h.Sum(nil)
}
