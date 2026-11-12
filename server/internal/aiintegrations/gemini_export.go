package aiintegrations

import (
	"archive/zip"
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Google Vault Gemini app XML observed in a locally validated export:
// GeminiUserConversationHistory/User/Email and Conversations/Conversation with
// ConversationId, ConversationTopic, and ConversationTurns/ConversationTurn.
// Turns contain RequestId, ModelVersion, Timestamp, Prompt/Text,
// PrimaryResponse/{ResponseId,Text}, and optional ParentRequestId.
// See https://knowledge.workspace.google.com/vault/exports/vault-export-contents#gemini_conversation_file.
// Legacy flat field variants are retained. Names ignore XML namespaces, case,
// underscores, and hyphens. Unrecognized rich content fails closed. Workspace
// JSON exports are a different format. Fixtures contain synthetic values only.
type geminiTurn struct {
	OwnerEmail      string
	ConversationID  string
	Title           string
	Prompt          string
	Response        string
	Model           string
	ParentRequestID string
	RequestID       string
	Timestamp       time.Time
	// ID uses owner+conversation+request ID, or full text/metadata+timestamp.
	// Identical turns at the same timestamp without request IDs cannot be
	// distinguished; they are deduplicated just like overlapping exports.
	ID string
}

// errGeminiPartialExport marks proven Vault-side failures, not parser errors.
var errGeminiPartialExport = errors.New("gemini export contains partial account or conversation failures")

type geminiExportFile struct {
	Name    string
	Content []byte
}

const (
	geminiMaxArchiveBytes = 64 << 20
	geminiMaxFileBytes    = 16 << 20
	geminiMaxTotalBytes   = 128 << 20
	geminiMaxFiles        = 10000
	geminiMaxTurns        = 100000
	geminiMaxXMLNodes     = 100000
	geminiMaxXMLDepth     = 64
)

// parseGeminiExport accepts content ZIPs and required count/error sidecars,
// either adjacent or inside ZIPs. All files from one Vault export must be passed.
// An empty window succeeds only with successful zero counts and an error report.
// All validation precedes callbacks; callback failures are not transactional.
func parseGeminiExport(ctx context.Context, files []geminiExportFile, visit func(geminiTurn) error) error {
	if visit == nil || len(files) == 0 || len(files) > geminiMaxFiles {
		return errors.New("parse error: Gemini export requires files and a visitor")
	}
	var turns []geminiTurn
	var summaries, reports [][]byte
	seen := make(map[string]geminiTurn)
	counts := make(map[string]int)
	fileCount, total, compressed := 0, 0, 0
	consume := func(name string, data []byte) error {
		if len(data) > geminiMaxFileBytes {
			return errors.New("parse error: Gemini export file exceeds limit")
		}
		total += len(data)
		if total > geminiMaxTotalBytes {
			return errors.New("parse error: Gemini export expanded size exceeds limit")
		}
		name = strings.ToLower(path.Base(name))
		switch {
		case strings.HasSuffix(name, "-result-counts.csv"):
			summaries = append(summaries, data)
		case strings.HasSuffix(name, "-error.xml"), strings.HasSuffix(name, "-errors.xml"):
			reports = append(reports, data)
		case strings.HasSuffix(name, ".xml"):
			parsed, err := parseGeminiConversationXML(ctx, data)
			if err != nil {
				return err
			}
			for _, turn := range parsed {
				if old, ok := seen[turn.ID]; ok {
					if old.Prompt != turn.Prompt || old.Response != turn.Response || !old.Timestamp.Equal(turn.Timestamp) || old.Model != turn.Model || old.ParentRequestID != turn.ParentRequestID {
						return errors.New("parse error: Gemini export has conflicting request identities")
					}
					continue
				}
				seen[turn.ID] = turn
				turns = append(turns, turn)
				counts[turn.OwnerEmail]++
				if len(turns) > geminiMaxTurns {
					return errors.New("parse error: Gemini export has too many turns")
				}
			}
		case strings.HasSuffix(name, ".md5"):
			// ZIP CRC is checked on reads. Manifests do not authenticate provenance.
		default:
			return errors.New("unsupported file in Gemini export")
		}
		return nil
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("parse Gemini export: %w", err)
		}
		fileCount++
		if fileCount > geminiMaxFiles {
			return errors.New("parse error: Gemini export has too many files")
		}
		if !strings.HasSuffix(strings.ToLower(file.Name), ".zip") {
			if err := consume(file.Name, file.Content); err != nil {
				return err
			}
			continue
		}
		compressed += len(file.Content)
		if compressed > geminiMaxArchiveBytes {
			return errors.New("parse error: Gemini export compressed size exceeds limit")
		}
		zr, err := zip.NewReader(bytes.NewReader(file.Content), int64(len(file.Content)))
		if err != nil {
			return fmt.Errorf("read Gemini export ZIP: %w", err)
		}
		for _, entry := range zr.File {
			fileCount++
			if fileCount > geminiMaxFiles {
				return errors.New("parse error: Gemini export has too many files")
			}
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("parse Gemini export: %w", err)
			}
			if !fs.ValidPath(strings.TrimSuffix(entry.Name, "/")) || strings.Contains(entry.Name, "\\") || entry.Mode()&fs.ModeSymlink != 0 {
				return errors.New("parse error: Gemini export has unsafe entry")
			}
			if entry.FileInfo().IsDir() {
				continue
			}
			if entry.UncompressedSize64 > geminiMaxFileBytes || entry.UncompressedSize64 > uint64(geminiMaxTotalBytes-total) {
				return errors.New("parse error: Gemini export expanded size exceeds limit")
			}
			if entry.UncompressedSize64 > 1<<20 && (entry.CompressedSize64 == 0 || entry.UncompressedSize64/entry.CompressedSize64 > 1000) {
				return errors.New("parse error: Gemini export suspicious compression ratio")
			}
			r, err := entry.Open()
			if err != nil {
				return fmt.Errorf("open Gemini export entry: %w", err)
			}
			data, readErr := io.ReadAll(io.LimitReader(r, geminiMaxFileBytes+1))
			closeErr := r.Close()
			if readErr != nil {
				return fmt.Errorf("read Gemini export entry: %w", readErr)
			}
			if closeErr != nil {
				return fmt.Errorf("close Gemini export entry: %w", closeErr)
			}
			if err := consume(entry.Name, data); err != nil {
				return err
			}
		}
	}
	if len(summaries) == 0 || len(reports) == 0 {
		return errors.New("parse error: Gemini export requires result counts and an error report")
	}
	for _, report := range reports {
		if err := validateGeminiErrorReport(ctx, report); err != nil {
			return err
		}
	}
	for _, summary := range summaries {
		if err := validateGeminiResultCounts(summary, counts); err != nil {
			return err
		}
	}
	// Vault may split conversations across archives and does not promise file
	// order. Visit chronologically so replay establishes the same creation
	// time, title progression, and equal-timestamp message order each time.
	slices.SortFunc(turns, func(a, b geminiTurn) int {
		if order := cmp.Compare(a.OwnerEmail, b.OwnerEmail); order != 0 {
			return order
		}
		if order := cmp.Compare(a.ConversationID, b.ConversationID); order != 0 {
			return order
		}
		if order := a.Timestamp.Compare(b.Timestamp); order != 0 {
			return order
		}
		return cmp.Compare(a.ID, b.ID)
	})
	for _, turn := range turns {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("parse Gemini export: %w", err)
		}
		if err := visit(turn); err != nil {
			return err
		}
	}
	return nil
}

type geminiXMLNode struct {
	name     string
	value    []byte
	children []*geminiXMLNode
}

func geminiFieldName(s string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(s))
}

func readGeminiXML(ctx context.Context, data []byte) (*geminiXMLNode, error) {
	if len(data) == 0 || len(data) > geminiMaxFileBytes {
		return nil, errors.New("invalid Gemini XML size")
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var root *geminiXMLNode
	var stack []*geminiXMLNode
	nodes := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("parse Gemini XML: %w", err)
		}
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid Gemini XML: %w", err)
		}
		switch t := token.(type) {
		case xml.StartElement:
			nodes++
			if nodes > geminiMaxXMLNodes || len(stack) >= geminiMaxXMLDepth {
				return nil, errors.New("parse error: Gemini XML complexity exceeds limit")
			}
			n := &geminiXMLNode{name: geminiFieldName(t.Name.Local), value: nil, children: nil}
			if len(stack) == 0 {
				if root != nil {
					return nil, errors.New("parse error: Gemini XML has multiple roots")
				}
				root = n
			} else {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].value = append(stack[len(stack)-1].value, t...)
			} else if strings.TrimSpace(string(t)) != "" {
				return nil, errors.New("parse error: Gemini XML has text outside root")
			}
		case xml.Directive:
			return nil, errors.New("parse error: Gemini XML directives are unsupported")
		}
	}
	if root == nil {
		return nil, errors.New("parse error: Gemini XML is empty")
	}
	return root, nil
}

func geminiScalar(n *geminiXMLNode) (string, error) {
	if len(n.children) != 0 {
		return "", errors.New("parse error: Gemini text field contains unsupported nested content")
	}
	return string(n.value), nil
}

func parseGeminiConversationXML(ctx context.Context, data []byte) ([]geminiTurn, error) {
	root, err := readGeminiXML(ctx, data)
	if err != nil {
		return nil, err
	}
	var turns []geminiTurn
	var walk func(*geminiXMLNode, geminiTurn) error
	walk = func(n *geminiXMLNode, scope geminiTurn) error {
		// Sibling metadata is read first, regardless of element ordering.
		fields := make(map[string]bool)
		for _, child := range n.children {
			switch child.name {
			case "email", "conversationid", "conversationtopic", "timestamp":
				if fields[child.name] {
					return errors.New("parse error: Gemini XML has duplicate metadata fields")
				}
				fields[child.name] = true
				value, err := geminiScalar(child)
				if err != nil {
					return err
				}
				switch child.name {
				case "email":
					scope.OwnerEmail = strings.ToLower(strings.TrimSpace(value))
				case "conversationid":
					scope.ConversationID = strings.TrimSpace(value)
				case "conversationtopic":
					scope.Title = value
				case "timestamp":
					scope.Timestamp, err = parseGeminiTimestamp(value)
					if err != nil {
						return err
					}
				}
			}
		}
		if n.name == "conversationturn" {
			turn, err := decodeGeminiTurn(n, scope)
			if err != nil {
				return err
			}
			turns = append(turns, turn)
			if len(turns) > geminiMaxTurns {
				return errors.New("parse error: Gemini XML has too many turns")
			}
			return nil
		}
		before := len(turns)
		for _, child := range n.children {
			if err := walk(child, scope); err != nil {
				return err
			}
		}
		if (n.name == "conversation" || fields["conversationid"]) && len(turns) == before {
			return errors.New("parse error: Gemini conversation has no recognized turns")
		}
		return nil
	}
	var initial geminiTurn
	if root.name == "geminiuserconversationhistory" {
		// The owner is in a User sibling of Conversations, not an ancestor
		// of each turn. Resolve it before walking either sibling.
		userCount, emailCount := 0, 0
		for _, child := range root.children {
			if child.name != "user" {
				continue
			}
			userCount++
			for _, field := range child.children {
				if field.name != "email" {
					continue
				}
				emailCount++
				email, err := geminiScalar(field)
				if err != nil {
					return nil, err
				}
				initial.OwnerEmail = strings.ToLower(strings.TrimSpace(email))
			}
		}
		if userCount != 1 || emailCount != 1 || !strings.Contains(initial.OwnerEmail, "@") {
			return nil, errors.New("invalid Gemini export owner metadata")
		}
	}
	if err := walk(root, initial); err != nil {
		return nil, err
	}
	if len(turns) == 0 {
		foundOwner := false
		var owner func(*geminiXMLNode)
		owner = func(n *geminiXMLNode) {
			if n.name == "email" && strings.Contains(string(n.value), "@") {
				foundOwner = true
			}
			for _, c := range n.children {
				owner(c)
			}
		}
		owner(root)
		if !foundOwner {
			return nil, errors.New("unsupported or empty Gemini conversation XML")
		}
	}
	return turns, nil
}

func decodeGeminiTurn(n *geminiXMLNode, turn geminiTurn) (geminiTurn, error) {
	fields := make(map[string]string)
	for _, child := range n.children {
		key := child.name
		switch key {
		case "userprompt":
			key = "prompt"
		case "geminiappresponse", "geminiresponse", "primaryresponse":
			key = "response"
		case "geminimodelversion", "modelversion":
			key = "model"
		}
		switch key {
		case "prompt", "response", "model", "parentrequestid", "requestid", "timestamp":
			if _, exists := fields[key]; exists {
				return turn, errors.New("parse error: Gemini turn has duplicate fields")
			}
			var value string
			var err error
			if child.name == "prompt" || child.name == "primaryresponse" {
				value, err = geminiTurnText(child)
			} else {
				value, err = geminiScalar(child)
			}
			if err != nil {
				return turn, err
			}
			fields[key] = value
		default:
			// Metadata can grow, but rich content must not silently disappear.
			if len(child.children) != 0 || strings.Contains(key, "attachment") || strings.Contains(key, "image") || strings.Contains(key, "audio") || strings.Contains(key, "content") || strings.Contains(key, "prompt") || strings.Contains(key, "response") || strings.Contains(key, "text") {
				return turn, errors.New("parse error: Gemini turn contains unsupported content")
			}
		}
	}
	prompt, hasPrompt := fields["prompt"]
	response, hasResponse := fields["response"]
	if !hasPrompt || !hasResponse || strings.TrimSpace(prompt+response) == "" {
		return turn, errors.New("parse error: Gemini turn requires recognized prompt and response fields")
	}
	if turn.OwnerEmail == "" || !strings.Contains(turn.OwnerEmail, "@") || turn.ConversationID == "" {
		return turn, errors.New("parse error: Gemini turn lacks owner or conversation identity")
	}
	var err error
	if value, ok := fields["timestamp"]; ok {
		turn.Timestamp, err = parseGeminiTimestamp(value)
		if err != nil {
			return turn, err
		}
	}
	if turn.Timestamp.IsZero() {
		return turn, errors.New("parse error: Gemini turn lacks timestamp")
	}
	turn.Prompt, turn.Response, turn.Model = prompt, response, fields["model"]
	turn.RequestID, turn.ParentRequestID = strings.TrimSpace(fields["requestid"]), strings.TrimSpace(fields["parentrequestid"])
	identity := []string{turn.OwnerEmail, turn.ConversationID, "request", turn.RequestID}
	if turn.RequestID == "" {
		identity = []string{turn.OwnerEmail, turn.ConversationID, "text", turn.Timestamp.Format(time.RFC3339Nano), turn.Prompt, turn.Response, turn.Model, turn.ParentRequestID}
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return turn, fmt.Errorf("encode Gemini turn identity: %w", err)
	}
	sum := sha256.Sum256(encoded)
	turn.ID = hex.EncodeToString(sum[:])
	return turn, nil
}

// geminiTurnText accepts the observed text wrapper without flattening arbitrary
// XML: silently flattening unknown rich content would conceal missing data.
func geminiTurnText(n *geminiXMLNode) (string, error) {
	if len(n.children) == 0 {
		return geminiScalar(n)
	}
	if strings.TrimSpace(string(n.value)) != "" {
		return "", errors.New("unsupported mixed Gemini text content")
	}
	seen := map[string]bool{}
	value := ""
	for _, field := range n.children {
		if seen[field.name] {
			return "", errors.New("duplicate Gemini text wrapper field")
		}
		seen[field.name] = true
		switch field.name {
		case "text":
			text, err := geminiScalar(field)
			if err != nil {
				return "", err
			}
			value = text
		case "responseid":
			if n.name != "primaryresponse" {
				return "", errors.New("unexpected Gemini response metadata")
			}
			if _, err := geminiScalar(field); err != nil {
				return "", err
			}
		default:
			return "", errors.New("unsupported Gemini text wrapper content")
		}
	}
	if !seen["text"] {
		return "", errors.New("missing Gemini text wrapper content")
	}
	return value, nil
}

func parseGeminiTimestamp(value string) (time.Time, error) {
	for _, format := range []string{time.RFC3339Nano, "2006-01-02T15:04:05Z0700"} {
		t, err := time.Parse(format, strings.TrimSpace(value))
		if err == nil && !t.IsZero() {
			return t.UTC(), nil
		}
	}
	return time.Time{}, errors.New("parse error: Gemini export has invalid timestamp")
}

func validateGeminiErrorReport(ctx context.Context, data []byte) error {
	root, err := readGeminiXML(ctx, data)
	if err != nil {
		return err
	}
	recognized := false
	var walk func(*geminiXMLNode) error
	walk = func(n *geminiXMLNode) error {
		switch n.name {
		case "accounterrorscount", "partialaccounterrorscount", "conversationerrorscount", "conversationturnerrorscount":
			recognized = true
			value, err := geminiScalar(n)
			if err != nil {
				return err
			}
			count, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
			if err != nil {
				return errors.New("invalid Gemini export failure count")
			}
			if count != 0 {
				return fmt.Errorf("read Gemini error report: %w", errGeminiPartialExport)
			}
		case "accounterrors", "partialaccounterrors", "conversationerrors", "conversationturnerrors":
			recognized = true
			if len(n.children) > 0 || strings.TrimSpace(string(n.value)) != "" {
				return fmt.Errorf("read Gemini error report: %w", errGeminiPartialExport)
			}
		default:
			if strings.Contains(n.name, "error") && n.name != "errorreport" && n.name != "errors" && (len(n.children) > 0 || strings.TrimSpace(string(n.value)) != "") {
				return errors.New("parse error: Gemini export has an unsupported failure report")
			}
		}
		for _, child := range n.children {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return err
	}
	if !recognized {
		return errors.New("unrecognized Gemini error report")
	}
	return nil
}

func validateGeminiResultCounts(data []byte, actual map[string]int) error {
	if len(data) > geminiMaxFileBytes {
		return errors.New("parse error: Gemini result counts exceed limit")
	}
	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})))
	header, err := reader.Read()
	if err != nil {
		return errors.New("invalid Gemini result counts header")
	}
	columns := make(map[string]int)
	for i, name := range header {
		key := geminiFieldName(strings.TrimSpace(name))
		if _, exists := columns[key]; exists {
			return errors.New("duplicate Gemini result counts column")
		}
		columns[key] = i
	}
	for _, key := range []string{"email", "accountstatus", "successcount"} {
		if _, ok := columns[key]; !ok {
			return errors.New("unsupported Gemini result counts columns")
		}
	}
	seen := make(map[string]bool)
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.New("invalid Gemini result counts row")
		}
		owner := strings.ToLower(strings.TrimSpace(row[columns["email"]]))
		if owner == "" || seen[owner] {
			return errors.New("invalid or duplicate Gemini result counts owner")
		}
		seen[owner] = true
		status := strings.TrimSpace(row[columns["accountstatus"]])
		if status == "" {
			return errors.New("missing Gemini result counts account status")
		}
		if status != "Success" {
			return fmt.Errorf("read Gemini result counts account status %q: %w", status, errGeminiPartialExport)
		}
		count, err := strconv.ParseInt(strings.TrimSpace(row[columns["successcount"]]), 10, 64)
		if err != nil || count < 0 || count != int64(actual[owner]) {
			return errors.New("parse error: Gemini result counts do not match exported turns")
		}
	}
	if len(seen) == 0 {
		return errors.New("parse error: Gemini result counts are empty")
	}
	for owner := range actual {
		if !seen[owner] {
			return errors.New("parse error: Gemini result counts omit an exported owner")
		}
	}
	return nil
}
