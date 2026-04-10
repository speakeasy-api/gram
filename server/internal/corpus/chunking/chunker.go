package chunking

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type Strategy struct {
	ChunkBy      string `json:"chunk_by"`
	MaxChunkSize int    `json:"max_chunk_size"`
	MinChunkSize int    `json:"min_chunk_size"`
}

type Chunk struct {
	ID          string
	FilePath    string
	HeadingPath string
	Breadcrumb  string
	Content     string
	ContentText string
}

func ChunkMarkdown(filePath string, source []byte, strategy Strategy) ([]Chunk, error) {
	if strategy.ChunkBy == "file" {
		plainText, err := ExtractPlainText(source)
		if err != nil {
			return nil, fmt.Errorf("extract text: %w", err)
		}
		return []Chunk{{
			ID:          GenerateChunkID(filePath, ""),
			FilePath:    filePath,
			HeadingPath: "",
			Breadcrumb:  "",
			Content:     string(source),
			ContentText: plainText,
		}}, nil
	}

	targetLevel := headingLevel(strategy.ChunkBy)
	if targetLevel == 0 {
		return nil, fmt.Errorf("invalid chunk_by: %s", strategy.ChunkBy)
	}

	reader := text.NewReader(source)
	parser := goldmark.DefaultParser()
	doc := parser.Parse(reader)

	sections := splitByHeading(doc, source, targetLevel)

	var chunks []Chunk
	for _, sec := range sections {
		plainText, err := ExtractPlainText([]byte(sec.content))
		if err != nil {
			return nil, fmt.Errorf("extract text for %s: %w", sec.headingPath, err)
		}
		chunks = append(chunks, Chunk{
			ID:          GenerateChunkID(filePath, sec.headingPath),
			FilePath:    filePath,
			HeadingPath: sec.headingPath,
			Breadcrumb:  GenerateBreadcrumb(sec.headings),
			Content:     sec.content,
			ContentText: plainText,
		})
	}

	// Filter out heading-only chunks with no meaningful body text
	chunks = filterEmpty(chunks)

	if strategy.MinChunkSize > 0 {
		chunks = mergeRunts(chunks, strategy.MinChunkSize)
	}
	if strategy.MaxChunkSize > 0 {
		chunks = splitOversized(chunks, strategy.MaxChunkSize, filePath)
	}

	return chunks, nil
}

func headingLevel(chunkBy string) int {
	switch chunkBy {
	case "h1":
		return 1
	case "h2":
		return 2
	case "h3":
		return 3
	default:
		return 0
	}
}

type section struct {
	headings    []string
	headingPath string
	content     string
}

func splitByHeading(doc ast.Node, source []byte, targetLevel int) []section {
	var sections []section
	var currentHeadings []string
	var currentStart int
	inSection := false

	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		heading, ok := child.(*ast.Heading)
		if !ok {
			continue
		}

		if heading.Level < targetLevel {
			if inSection {
				content := extractRange(source, currentStart, headingLineStart(child, source))
				sections = append(sections, section{
					headings:    copySlice(currentHeadings),
					headingPath: buildHeadingPath(currentHeadings),
					content:     strings.TrimSpace(content),
				})
			}

			headingText := extractNodeText(heading, source)
			if heading.Level <= len(currentHeadings) {
				currentHeadings = currentHeadings[:heading.Level-1]
			}
			currentHeadings = append(currentHeadings, headingText)
			// Start tracking content from this heading — if no target-level
			// heading follows, this becomes its own section.
			currentStart = headingLineStart(child, source)
			inSection = true
			continue
		}

		if heading.Level > targetLevel {
			continue
		}

		if inSection {
			content := extractRange(source, currentStart, headingLineStart(child, source))
			sections = append(sections, section{
				headings:    copySlice(currentHeadings),
				headingPath: buildHeadingPath(currentHeadings),
				content:     strings.TrimSpace(content),
			})
		}

		headingText := extractNodeText(heading, source)
		if heading.Level <= len(currentHeadings) {
			currentHeadings = currentHeadings[:heading.Level-1]
		}
		currentHeadings = append(currentHeadings, headingText)
		currentStart = headingLineStart(child, source)
		inSection = true
	}

	if inSection {
		content := extractRange(source, currentStart, len(source))
		sections = append(sections, section{
			headings:    copySlice(currentHeadings),
			headingPath: buildHeadingPath(currentHeadings),
			content:     strings.TrimSpace(content),
		})
	}

	return sections
}

func nodeStart(node ast.Node) int {
	// For headings, the text segment starts after the "## " markers.
	// Walk backwards from the text start to find the line beginning.
	if node.Lines().Len() > 0 {
		return node.Lines().At(0).Start
	}
	if fc := node.FirstChild(); fc != nil {
		pos := nodeStart(fc)
		// Walk backwards to find the start of the line (past "## " markers)
		return pos
	}
	return 0
}

// headingLineStart returns the byte offset of the line containing this heading,
// which includes the "## " marker prefix.
func headingLineStart(node ast.Node, source []byte) int {
	pos := nodeStart(node)
	// Walk backwards to start of line
	for pos > 0 && source[pos-1] != '\n' {
		pos--
	}
	return pos
}

func extractRange(source []byte, start, end int) string {
	if start >= len(source) {
		return ""
	}
	if end > len(source) {
		end = len(source)
	}
	return string(source[start:end])
}

func extractNodeText(node ast.Node, source []byte) string {
	var buf bytes.Buffer
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if t, ok := child.(*ast.Text); ok {
			buf.Write(t.Segment.Value(source))
		}
	}
	return buf.String()
}

func buildHeadingPath(headings []string) string {
	var parts []string
	for _, h := range headings {
		parts = append(parts, slugify(h))
	}
	return strings.Join(parts, "/")
}

func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		if r == ' ' || r == '-' || r == '_' {
			return '-'
		}
		return -1
	}, s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

func copySlice(s []string) []string {
	c := make([]string, len(s))
	copy(c, s)
	return c
}

func filterEmpty(chunks []Chunk) []Chunk {
	var result []Chunk
	for _, c := range chunks {
		// A chunk that only contains its own heading has no body content.
		// Check if content has text beyond the heading line(s).
		lines := strings.Split(strings.TrimSpace(c.Content), "\n")
		hasBody := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			hasBody = true
			break
		}
		if hasBody {
			result = append(result, c)
		}
	}
	return result
}

func mergeRunts(chunks []Chunk, minSize int) []Chunk {
	if len(chunks) <= 1 {
		return chunks
	}

	var result []Chunk
	for i, c := range chunks {
		if len(c.ContentText) < minSize && i > 0 {
			prev := &result[len(result)-1]
			prev.Content += "\n\n" + c.Content
			prev.ContentText += " " + c.ContentText
		} else {
			result = append(result, c)
		}
	}
	return result
}

func splitOversized(chunks []Chunk, maxSize int, filePath string) []Chunk {
	var result []Chunk
	for _, c := range chunks {
		if len(c.ContentText) <= maxSize {
			result = append(result, c)
			continue
		}

		paragraphs := strings.Split(c.Content, "\n\n")
		var current Chunk
		current.FilePath = filePath
		current.HeadingPath = c.HeadingPath
		current.Breadcrumb = c.Breadcrumb

		for _, p := range paragraphs {
			// If a single paragraph exceeds max, split by sentences/words
			segments := splitParagraph(p, maxSize)
			for _, seg := range segments {
				trimmed := strings.TrimSpace(seg)
				if len(current.ContentText)+len(trimmed) > maxSize && current.ContentText != "" {
					current.ID = GenerateChunkID(filePath, current.HeadingPath+fmt.Sprintf("/part-%d", len(result)))
					result = append(result, current)
					current = Chunk{
						FilePath:    filePath,
						HeadingPath: c.HeadingPath,
						Breadcrumb:  c.Breadcrumb,
						ID:          "",
						Content:     "",
						ContentText: "",
					}
				}
				if current.Content != "" {
					current.Content += "\n\n"
					current.ContentText += " "
				}
				current.Content += seg
				current.ContentText += trimmed
			}
		}

		if current.Content != "" {
			current.ID = GenerateChunkID(filePath, current.HeadingPath+fmt.Sprintf("/part-%d", len(result)))
			result = append(result, current)
		}
	}
	return result
}

func splitParagraph(p string, maxSize int) []string {
	if len(p) <= maxSize {
		return []string{p}
	}

	// Split at sentence boundaries (". ") first, then word boundaries
	var segments []string
	var current strings.Builder
	words := strings.FieldsSeq(p)
	for w := range words {
		if current.Len()+len(w)+1 > maxSize && current.Len() > 0 {
			segments = append(segments, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteByte(' ')
		}
		current.WriteString(w)
	}
	if current.Len() > 0 {
		segments = append(segments, current.String())
	}
	return segments
}

func ExtractPlainText(markdown []byte) (string, error) {
	reader := text.NewReader(markdown)
	parser := goldmark.DefaultParser()
	doc := parser.Parse(reader)

	var buf bytes.Buffer
	extractTextRecursive(doc, markdown, &buf)
	return strings.TrimSpace(buf.String()), nil
}

func extractTextRecursive(node ast.Node, source []byte, buf *bytes.Buffer) {
	switch n := node.(type) {
	case *ast.Text:
		buf.Write(n.Segment.Value(source))
		if n.HardLineBreak() || n.SoftLineBreak() {
			buf.WriteByte(' ')
		}
	case *ast.String:
		buf.Write(n.Value)
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		for i := 0; i < node.Lines().Len(); i++ {
			line := node.Lines().At(i)
			buf.Write(line.Value(source))
		}
		return
	case *ast.Heading:
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			extractTextRecursive(child, source, buf)
		}
		buf.WriteByte(' ')
		return
	case *ast.Paragraph:
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			extractTextRecursive(child, source, buf)
		}
		buf.WriteByte(' ')
		return
	case *ast.Link:
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			extractTextRecursive(child, source, buf)
		}
		return
	case *ast.AutoLink:
		buf.Write(n.Label(source))
		return
	}

	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		extractTextRecursive(child, source, buf)
	}
}

func GenerateChunkID(filePath, headingPath string) string {
	if headingPath == "" {
		return filePath
	}
	return filePath + "#" + headingPath
}

func GenerateBreadcrumb(headings []string) string {
	return strings.Join(headings, " > ")
}
