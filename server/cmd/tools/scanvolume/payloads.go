package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Tool-response shapes.
//
// Gram stores the agent's tool response verbatim: the hook relay forwards
// Claude Code's `tool_response` object as `data.tool_call.output` and the
// server writes `json.Marshal(output)` into `chat_messages.content` (see
// canonicalToolResultContent in server/internal/hooks/ingest_hooks.go). The
// structs below reconstruct those objects field for field so the harness
// measures the same bytes the scanner sees.
//
// The field sets are reconstructed from Claude Code's observable hook output,
// not read out of a production payload. Confirm them against
// gram.telemetry_logs before treating any single shape's absolute numbers as
// authoritative; the relative cost of `originalFile` is what this harness is
// built to measure and does not depend on the surrounding fields.

// shape names double as the row labels in the report.
const (
	shapeRead      = "Read"
	shapeEdit      = "Edit"
	shapeMultiEdit = "MultiEdit"
	shapeWrite     = "Write"
	shapeGrep      = "Grep"
	shapeBash      = "Bash"
	shapeMCP       = "MCP"
)

// allShapes is the report's row order.
var allShapes = []string{shapeRead, shapeEdit, shapeMultiEdit, shapeWrite, shapeGrep, shapeBash, shapeMCP}

type readFile struct {
	FilePath   string `json:"filePath"`
	Content    string `json:"content"`
	NumLines   int    `json:"numLines"`
	StartLine  int    `json:"startLine"`
	TotalLines int    `json:"totalLines"`
}

type readResponse struct {
	Type string   `json:"type"`
	File readFile `json:"file"`
}

type patchHunk struct {
	OldStart int      `json:"oldStart"`
	OldLines int      `json:"oldLines"`
	NewStart int      `json:"newStart"`
	NewLines int      `json:"newLines"`
	Lines    []string `json:"lines"`
}

type editResponse struct {
	FilePath        string      `json:"filePath"`
	OldString       string      `json:"oldString"`
	NewString       string      `json:"newString"`
	OriginalFile    string      `json:"originalFile"`
	StructuredPatch []patchHunk `json:"structuredPatch"`
	UserModified    bool        `json:"userModified"`
	ReplaceAll      bool        `json:"replaceAll"`
}

type multiEditResponse struct {
	FilePath             string      `json:"filePath"`
	Edits                []editPair  `json:"edits"`
	OriginalFileContents string      `json:"originalFileContents"`
	StructuredPatch      []patchHunk `json:"structuredPatch"`
	UserModified         bool        `json:"userModified"`
}

type editPair struct {
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

type writeResponse struct {
	Type            string      `json:"type"`
	FilePath        string      `json:"filePath"`
	Content         string      `json:"content"`
	StructuredPatch []patchHunk `json:"structuredPatch"`
}

type grepResponse struct {
	Mode      string   `json:"mode"`
	NumFiles  int      `json:"numFiles"`
	Filenames []string `json:"filenames"`
	Content   string   `json:"content"`
	NumLines  int      `json:"numLines"`
}

type bashResponse struct {
	Stdout      string `json:"stdout"`
	Stderr      string `json:"stderr"`
	Interrupted bool   `json:"interrupted"`
	IsImage     bool   `json:"isImage"`
}

type mcpTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mcpResponse struct {
	Content []mcpTextBlock `json:"content"`
	IsError bool           `json:"isError"`
}

// sample is one reconstructed tool response plus the part of it that carries
// content the pipeline has not already scanned on an earlier message.
type sample struct {
	shape string
	path  string

	// content is exactly what lands in chat_messages.content.
	content string

	// novel is the same response with the fields that only restate
	// already-scanned repository state removed. For every shape except the
	// edit family it equals content.
	novel string

	// echoed is the value of the field that restates already-scanned state
	// (originalFile / originalFileContents), empty when the shape has none.
	// Session dedup keys on it.
	echoed string
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Errorf("marshal tool response: %w", err))
	}
	return string(b)
}

// editWindow picks a plausible edit target inside the file: a run of lines
// from the middle, which is what an agent's oldString usually is.
func editWindow(lines []string, editIndex int) (start int, window []string) {
	if len(lines) == 0 {
		return 0, nil
	}
	span := min(6, len(lines))
	// Walk the edit site down the file so successive edits in a session touch
	// different regions, as a real editing pass does.
	start = (editIndex * 17) % max(1, len(lines)-span+1)
	return start, lines[start : start+span]
}

func rewrite(window []string) []string {
	out := make([]string, len(window))
	for i, line := range window {
		out[i] = line + " // edited"
	}
	return out
}

func patchFor(start int, oldWindow, newWindow []string) []patchHunk {
	lines := make([]string, 0, len(oldWindow)+len(newWindow))
	for _, l := range oldWindow {
		lines = append(lines, "-"+l)
	}
	for _, l := range newWindow {
		lines = append(lines, "+"+l)
	}
	return []patchHunk{{
		OldStart: start + 1,
		OldLines: len(oldWindow),
		NewStart: start + 1,
		NewLines: len(newWindow),
		Lines:    lines,
	}}
}

// buildRead reconstructs a Read tool response for the whole file.
func buildRead(path, body string) sample {
	lines := strings.Count(body, "\n") + 1
	content := mustJSON(readResponse{
		Type: "text",
		File: readFile{
			FilePath:   path,
			Content:    body,
			NumLines:   lines,
			StartLine:  1,
			TotalLines: lines,
		},
	})
	return sample{shape: shapeRead, path: path, content: content, novel: content, echoed: ""}
}

// buildEdit reconstructs a single-hunk Edit tool response, including the
// originalFile field that carries the whole pre-edit file.
func buildEdit(path, body string, editIndex int) sample {
	lines := strings.Split(body, "\n")
	start, window := editWindow(lines, editIndex)
	newWindow := rewrite(window)
	oldString := strings.Join(window, "\n")
	newString := strings.Join(newWindow, "\n")
	patch := patchFor(start, window, newWindow)

	content := mustJSON(editResponse{
		FilePath:        path,
		OldString:       oldString,
		NewString:       newString,
		OriginalFile:    body,
		StructuredPatch: patch,
		UserModified:    false,
		ReplaceAll:      false,
	})
	novel := mustJSON(editResponse{
		FilePath:        path,
		OldString:       oldString,
		NewString:       newString,
		OriginalFile:    "",
		StructuredPatch: patch,
		UserModified:    false,
		ReplaceAll:      false,
	})
	return sample{shape: shapeEdit, path: path, content: content, novel: novel, echoed: body}
}

// buildMultiEdit reconstructs a MultiEdit response with three hunks.
func buildMultiEdit(path, body string, editIndex int) sample {
	lines := strings.Split(body, "\n")
	pairs := make([]editPair, 0, 3)
	hunks := make([]patchHunk, 0, 3)
	for i := range 3 {
		start, window := editWindow(lines, editIndex+i)
		newWindow := rewrite(window)
		pairs = append(pairs, editPair{
			OldString:  strings.Join(window, "\n"),
			NewString:  strings.Join(newWindow, "\n"),
			ReplaceAll: false,
		})
		hunks = append(hunks, patchFor(start, window, newWindow)...)
	}

	content := mustJSON(multiEditResponse{
		FilePath:             path,
		Edits:                pairs,
		OriginalFileContents: body,
		StructuredPatch:      hunks,
		UserModified:         false,
	})
	novel := mustJSON(multiEditResponse{
		FilePath:             path,
		Edits:                pairs,
		OriginalFileContents: "",
		StructuredPatch:      hunks,
		UserModified:         false,
	})
	return sample{shape: shapeMultiEdit, path: path, content: content, novel: novel, echoed: body}
}

// buildWrite reconstructs a Write response. Its content field is the file the
// agent just authored, so all of it is novel.
func buildWrite(path, body string) sample {
	lines := strings.Split(body, "\n")
	start, window := editWindow(lines, 0)
	content := mustJSON(writeResponse{
		Type:            "update",
		FilePath:        path,
		Content:         body,
		StructuredPatch: patchFor(start, window, rewrite(window)),
	})
	return sample{shape: shapeWrite, path: path, content: content, novel: content, echoed: ""}
}

// buildGrep reconstructs a content-mode Grep response over the file.
func buildGrep(path, body string) sample {
	lines := strings.Split(body, "\n")
	hits := make([]string, 0, 64)
	for i, line := range lines {
		if len(hits) == 64 {
			break
		}
		if strings.Contains(line, "err") || strings.Contains(line, "error") {
			hits = append(hits, fmt.Sprintf("%s:%d:%s", path, i+1, line))
		}
	}
	content := mustJSON(grepResponse{
		Mode:      "content",
		NumFiles:  1,
		Filenames: []string{path},
		Content:   strings.Join(hits, "\n"),
		NumLines:  len(hits),
	})
	return sample{shape: shapeGrep, path: path, content: content, novel: content, echoed: ""}
}

// buildBash reconstructs a Bash response whose stdout is the file, which is
// what `cat`, `git show`, and test output look like on the wire.
func buildBash(path, body string) sample {
	content := mustJSON(bashResponse{
		Stdout:      body,
		Stderr:      "",
		Interrupted: false,
		IsImage:     false,
	})
	return sample{shape: shapeBash, path: path, content: content, novel: content, echoed: ""}
}

// buildMCP reconstructs an MCP tool result. Gram stores mcp.result_json for
// these, which is the same JSON text.
func buildMCP(path, body string) sample {
	content := mustJSON(mcpResponse{
		Content: []mcpTextBlock{{Type: "text", Text: body}},
		IsError: false,
	})
	return sample{shape: shapeMCP, path: path, content: content, novel: content, echoed: ""}
}
