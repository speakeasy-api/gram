package relay

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/speakeasy-api/agenthooks"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

const promptAttachmentParentHopLimit = 10

type promptAttachmentEntry = components.HookPromptAttachmentEntry

type transcriptPromptAttachment struct {
	entry  promptAttachmentEntry
	offset int64
}

type promptAttachmentHighWaterAdvance struct {
	transcriptPath string
	offset         int64
}

type transcriptEntry struct {
	UUID       string               `json:"uuid"`
	ParentUUID string               `json:"parentUuid"`
	Type       string               `json:"type"`
	PromptID   string               `json:"promptId"`
	Timestamp  string               `json:"timestamp"`
	Message    transcriptMessage    `json:"message"`
	Attachment transcriptAttachment `json:"attachment"`
}

type transcriptIndexEntry struct {
	parentUUID   string
	entryType    string
	promptID     string
	promptSHA256 string
}

type transcriptIndexLine struct {
	UUID       string          `json:"uuid"`
	ParentUUID string          `json:"parentUuid"`
	Type       string          `json:"type"`
	PromptID   string          `json:"promptId"`
	Message    json.RawMessage `json:"message"`
}

type transcriptMessage struct {
	Content transcriptMessageContent `json:"content"`
}

type transcriptMessageContent struct {
	Text string
}

func (c *transcriptMessageContent) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		c.Text = s
		return nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &parts); err != nil {
		return err
	}
	var texts []string
	for _, part := range parts {
		if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
			texts = append(texts, part.Text)
		}
	}
	c.Text = strings.Join(texts, "\n")
	return nil
}

type transcriptAttachment struct {
	Type        string                      `json:"type"`
	Filename    string                      `json:"filename"`
	Path        string                      `json:"path"`
	DisplayPath string                      `json:"displayPath"`
	Content     transcriptAttachmentContent `json:"content"`
}

type transcriptAttachmentContent struct {
	Type string                          `json:"type"`
	File transcriptAttachmentContentFile `json:"file"`
	Text string                          `json:"-"`
}

type transcriptAttachmentContentFile struct {
	FilePath   string `json:"filePath"`
	Content    string `json:"content"`
	NumLines   *int   `json:"numLines"`
	StartLine  *int   `json:"startLine"`
	TotalLines *int   `json:"totalLines"`
}

func (c *transcriptAttachmentContent) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		c.Text = s
		return nil
	}
	var obj struct {
		Type string                          `json:"type"`
		File transcriptAttachmentContentFile `json:"file"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	c.Type = obj.Type
	c.File = obj.File
	return nil
}

func attachPromptAttachments(payload *components.IngestRequestBody, entries []promptAttachmentEntry) {
	if len(entries) == 0 {
		return
	}
	if payload.Data == nil {
		payload.Data = &components.HookIngestData{
			Mcp:               nil,
			McpAttribution:    nil,
			McpInventory:      nil,
			Message:           nil,
			Notification:      nil,
			Prompt:            nil,
			PromptAttachments: nil,
			Skill:             nil,
			ToolCall:          nil,
			Usage:             nil,
		}
	}
	payload.Data.PromptAttachments = append(payload.Data.PromptAttachments, entries...)
}

func collectClaudePromptAttachments(base *agenthooks.Event) ([]promptAttachmentEntry, promptAttachmentHighWaterAdvance, error) {
	if base.Provider != agenthooks.ProviderClaudeCode {
		return nil, promptAttachmentHighWaterAdvance{}, nil
	}
	switch base.Kind {
	case agenthooks.KindStop, agenthooks.KindSubagentStop, agenthooks.KindSessionEnd:
	default:
		return nil, promptAttachmentHighWaterAdvance{}, nil
	}
	transcriptPath := strings.TrimSpace(base.Session.TranscriptPath)
	if transcriptPath == "" {
		return nil, promptAttachmentHighWaterAdvance{}, nil
	}
	transcriptPath, err := validateClaudeTranscriptPath(transcriptPath)
	if err != nil {
		return nil, promptAttachmentHighWaterAdvance{}, err
	}
	highWater := readPromptAttachmentHighWater(transcriptPath)
	attachments, nextOffset, err := parsePromptAttachmentsFile(transcriptPath, highWater)
	if err != nil {
		return nil, promptAttachmentHighWaterAdvance{}, err
	}
	entries := make([]promptAttachmentEntry, 0, len(attachments))
	for _, attachment := range attachments {
		entries = append(entries, attachment.entry)
	}
	advance := promptAttachmentHighWaterAdvance{}
	if nextOffset > highWater {
		advance = promptAttachmentHighWaterAdvance{transcriptPath: transcriptPath, offset: nextOffset}
	}
	return entries, advance, nil
}

func parsePromptAttachmentsFile(path string, highWater int64) ([]transcriptPromptAttachment, int64, error) {
	transcriptPath, err := validateClaudeTranscriptPath(path)
	if err != nil {
		return nil, highWater, err
	}
	f, err := os.Open(transcriptPath)
	if err != nil {
		return nil, highWater, err
	}
	defer func() { _ = f.Close() }()
	return parsePromptAttachments(f, highWater)
}

func parsePromptAttachments(r io.Reader, highWater int64) ([]transcriptPromptAttachment, int64, error) {
	reader := bufio.NewReaderSize(r, 64*1024)

	index := map[string]transcriptIndexEntry{}
	var parsed []transcriptPromptAttachment
	var offset int64
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, highWater, err
		}
		start := offset
		offset += int64(len(line))

		var header transcriptIndexLine
		if err := json.Unmarshal(line, &header); err != nil || header.UUID == "" {
			continue
		}
		index[header.UUID] = compactTranscriptIndexEntry(header)
		if header.Type != "attachment" || start < highWater {
			continue
		}
		var entry transcriptEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		attachment, ok := promptAttachmentFromTranscriptEntry(entry)
		if !ok {
			continue
		}
		parsed = append(parsed, transcriptPromptAttachment{entry: attachment, offset: start})
	}
	for i := range parsed {
		if promptID, promptSHA256 := resolvePromptParent(index, parsed[i].entry.EntryUUID); promptID != "" {
			parsed[i].entry.PromptID = optStr(promptID)
			parsed[i].entry.PromptSha256 = optStr(promptSHA256)
		}
	}
	return parsed, offset, nil
}

func promptAttachmentFromTranscriptEntry(entry transcriptEntry) (promptAttachmentEntry, bool) {
	attachmentType := strings.TrimSpace(entry.Attachment.Type)
	switch attachmentType {
	case "file":
		content := entry.Attachment.Content.File.Content
		if content == "" {
			return promptAttachmentEntry{}, false
		}
		filePath := strings.TrimSpace(entry.Attachment.Content.File.FilePath)
		if filePath == "" {
			filePath = strings.TrimSpace(entry.Attachment.Filename)
		}
		return promptAttachmentEntry{
			EntryUUID:      entry.UUID,
			AttachmentKind: attachmentType,
			Content:        content,
			DisplayPath:    optStr(strings.TrimSpace(entry.Attachment.DisplayPath)),
			FilePath:       optStr(filePath),
			NumLines:       intPtrToInt64(entry.Attachment.Content.File.NumLines),
			PromptID:       nil,
			PromptSha256:   nil,
			StartLine:      intPtrToInt64(entry.Attachment.Content.File.StartLine),
			Timestamp:      optStr(strings.TrimSpace(entry.Timestamp)),
			TotalLines:     intPtrToInt64(entry.Attachment.Content.File.TotalLines),
		}, true
	case "directory":
		content := entry.Attachment.Content.Text
		if content == "" {
			return promptAttachmentEntry{}, false
		}
		return promptAttachmentEntry{
			EntryUUID:      entry.UUID,
			AttachmentKind: attachmentType,
			Content:        content,
			DisplayPath:    optStr(strings.TrimSpace(entry.Attachment.DisplayPath)),
			FilePath:       optStr(strings.TrimSpace(entry.Attachment.Path)),
			NumLines:       nil,
			PromptID:       nil,
			PromptSha256:   nil,
			StartLine:      nil,
			Timestamp:      optStr(strings.TrimSpace(entry.Timestamp)),
			TotalLines:     nil,
		}, true
	default:
		return promptAttachmentEntry{}, false
	}
}

func compactTranscriptIndexEntry(line transcriptIndexLine) transcriptIndexEntry {
	entry := transcriptIndexEntry{
		parentUUID:   strings.TrimSpace(line.ParentUUID),
		entryType:    strings.TrimSpace(line.Type),
		promptID:     "",
		promptSHA256: "",
	}
	if entry.entryType != "user" {
		return entry
	}

	entry.promptID = strings.TrimSpace(line.PromptID)
	if len(line.Message) == 0 {
		return entry
	}

	var message transcriptMessage
	if err := json.Unmarshal(line.Message, &message); err != nil {
		return entry
	}
	promptText := strings.TrimSpace(message.Content.Text)
	if promptText != "" {
		entry.promptSHA256 = promptTextSHA256(promptText)
	}
	return entry
}

func resolvePromptParent(entries map[string]transcriptIndexEntry, attachmentUUID string) (string, string) {
	current, ok := entries[attachmentUUID]
	for range promptAttachmentParentHopLimit {
		if !ok || current.parentUUID == "" {
			return "", ""
		}
		parent, found := entries[current.parentUUID]
		if !found {
			return "", ""
		}
		if parent.entryType == "user" {
			return parent.promptID, parent.promptSHA256
		}
		current, ok = parent, true
	}
	return "", ""
}

func promptTextSHA256(text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return hex.EncodeToString(sum[:])
}

func validateClaudeTranscriptPath(path string) (string, error) {
	raw := strings.TrimSpace(path)
	if raw == "" {
		return "", fmt.Errorf("empty transcript path")
	}
	if !filepath.IsAbs(raw) {
		return "", fmt.Errorf("transcript path must be absolute")
	}
	if pathHasTraversal(raw) {
		return "", fmt.Errorf("transcript path must not contain traversal")
	}

	root, err := claudeTranscriptProjectsRoot()
	if err != nil {
		return "", err
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve claude transcript root: %w", err)
	}

	candidate := filepath.Clean(raw)
	info, err := os.Lstat(candidate)
	if err != nil {
		return "", fmt.Errorf("stat transcript path: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("transcript path must not be a symlink")
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("transcript path must be a regular file")
	}

	resolvedCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve transcript path: %w", err)
	}
	if !pathWithin(resolvedCandidate, resolvedRoot) {
		return "", fmt.Errorf("transcript path outside claude transcript root")
	}
	return resolvedCandidate, nil
}

func claudeTranscriptProjectsRoot() (string, error) {
	configRoot := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR"))
	if configRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		configRoot = filepath.Join(home, ".claude")
	}
	if !filepath.IsAbs(configRoot) {
		abs, err := filepath.Abs(configRoot)
		if err != nil {
			return "", fmt.Errorf("resolve claude config directory: %w", err)
		}
		configRoot = abs
	}
	return filepath.Join(filepath.Clean(configRoot), "projects"), nil
}

func pathHasTraversal(path string) bool {
	return slices.Contains(strings.Split(filepath.ToSlash(path), "/"), "..")
}

func promptAttachmentHighWaterPath(transcriptPath string) string {
	root := hooksStateDir()
	if root == "" || strings.TrimSpace(transcriptPath) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(transcriptPath))
	return filepath.Join(root, "prompt-attachments", hex.EncodeToString(sum[:])+".offset")
}

func readPromptAttachmentHighWater(transcriptPath string) int64 {
	path := promptAttachmentHighWaterPath(transcriptPath)
	if path == "" {
		return 0
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var offset int64
	if _, err := fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &offset); err != nil || offset < 0 {
		return 0
	}
	return offset
}

func writePromptAttachmentHighWater(transcriptPath string, offset int64) {
	path := promptAttachmentHighWaterPath(transcriptPath)
	if path == "" || offset < 0 {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, fmt.Appendf(nil, "%d\n", offset), 0o600)
}

func commitPromptAttachmentHighWater(advance promptAttachmentHighWaterAdvance) {
	if advance.transcriptPath == "" || advance.offset <= 0 {
		return
	}
	writePromptAttachmentHighWater(advance.transcriptPath, advance.offset)
}

func intPtrToInt64(v *int) *int64 {
	if v == nil {
		return nil
	}
	out := int64(*v)
	return &out
}
