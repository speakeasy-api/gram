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
	Attachment transcriptAttachment `json:"attachment"`
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
	f, err := os.Open(path)
	if err != nil {
		return nil, highWater, err
	}
	defer func() { _ = f.Close() }()
	return parsePromptAttachments(f, highWater)
}

func parsePromptAttachments(r io.Reader, highWater int64) ([]transcriptPromptAttachment, int64, error) {
	reader := bufio.NewReaderSize(r, 64*1024)

	entries := map[string]transcriptEntry{}
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

		var entry transcriptEntry
		if err := json.Unmarshal(line, &entry); err != nil || entry.UUID == "" {
			continue
		}
		entries[entry.UUID] = entry
		if entry.Type != "attachment" || start < highWater {
			continue
		}
		attachment, ok := promptAttachmentFromTranscriptEntry(entry)
		if !ok {
			continue
		}
		parsed = append(parsed, transcriptPromptAttachment{entry: attachment, offset: start})
	}
	for i := range parsed {
		if promptID := resolvePromptID(entries, parsed[i].entry.EntryUUID); promptID != "" {
			parsed[i].entry.PromptID = optStr(promptID)
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
			StartLine:      nil,
			Timestamp:      optStr(strings.TrimSpace(entry.Timestamp)),
			TotalLines:     nil,
		}, true
	default:
		return promptAttachmentEntry{}, false
	}
}

func resolvePromptID(entries map[string]transcriptEntry, attachmentUUID string) string {
	current, ok := entries[attachmentUUID]
	for range promptAttachmentParentHopLimit {
		if !ok || current.ParentUUID == "" {
			return ""
		}
		parent, found := entries[current.ParentUUID]
		if !found {
			return ""
		}
		if parent.Type == "user" {
			return strings.TrimSpace(parent.PromptID)
		}
		current, ok = parent, true
	}
	return ""
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
