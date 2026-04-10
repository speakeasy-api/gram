package chunking

import "errors"

type Strategy struct {
	ChunkBy      string // "h1", "h2", "h3", "file"
	MaxChunkSize int
	MinChunkSize int
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
	return nil, errors.New("not implemented")
}

func ExtractPlainText(markdown []byte) (string, error) {
	return "", errors.New("not implemented")
}

func GenerateChunkID(filePath, headingPath string) string {
	return ""
}

func GenerateBreadcrumb(headings []string) string {
	return ""
}
