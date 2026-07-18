package github

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	maxGitAdvertisementBytes       = 1024 * 1024
	maxRepositoryArchiveBytes      = 32 * 1024 * 1024
	maxRepositoryUncompressedBytes = 256 * 1024 * 1024
	// MaxRepositorySkillBytes is the largest SKILL.md the scanner will return.
	MaxRepositorySkillBytes = 64 * 1024
	// MaxRepositorySkills is the largest number of SKILL.md candidates a
	// repository may contain before the scan is rejected.
	MaxRepositorySkills = 100
	githubBase          = "https://github.com"
	codeloadBase        = "https://codeload.github.com"
)

var (
	ErrInvalidRepositoryURL = errors.New("invalid GitHub repository URL")
	ErrPublicRepoNotFound   = errors.New("public GitHub repository not found")
	ErrRepositoryEmpty      = errors.New("repository has no default branch")
	ErrTooManySkillFiles    = errors.New("repository contains too many SKILL.md files")
	ErrRepositoryTooLarge   = errors.New("repository archive exceeds size limits")
)

type PublicRepositoryFile struct {
	Path    string
	Content string
}

// PublicRepositorySkippedFile is a SKILL.md candidate excluded from a snapshot
// because it exceeded the per-file size limit.
type PublicRepositorySkippedFile struct {
	Path string
	Size int64
}

type PublicRepositorySnapshot struct {
	URL           string
	FullName      string
	DefaultBranch string
	CommitSHA     string
	Visibility    string
	Files         []PublicRepositoryFile
	Skipped       []PublicRepositorySkippedFile
}

type PublicRepositoryReader interface {
	FetchSkillFiles(ctx context.Context, repositoryURL string) (*PublicRepositorySnapshot, error)
}

type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type PublicClient struct {
	httpClient  httpDoer
	githubBase  string
	archiveBase string
}

func NewPublicClient(httpClient httpDoer) *PublicClient {
	return &PublicClient{httpClient: httpClient, githubBase: githubBase, archiveBase: codeloadBase}
}

func (c *PublicClient) FetchSkillFiles(ctx context.Context, repositoryURL string) (*PublicRepositorySnapshot, error) {
	owner, repo, err := parsePublicRepositoryURL(repositoryURL)
	if err != nil {
		return nil, err
	}

	commitSHA, defaultBranch, err := c.resolveHead(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	archiveURL := fmt.Sprintf("%s/%s/%s/tar.gz/%s", strings.TrimRight(c.archiveBase, "/"), url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(commitSHA))
	files, skipped, err := c.fetchArchive(ctx, archiveURL)
	if err != nil {
		return nil, fmt.Errorf("fetch repository archive: %w", err)
	}

	return &PublicRepositorySnapshot{
		URL:           fmt.Sprintf("https://github.com/%s/%s", owner, repo),
		FullName:      owner + "/" + repo,
		DefaultBranch: defaultBranch,
		CommitSHA:     commitSHA,
		Visibility:    "public",
		Files:         files,
		Skipped:       skipped,
	}, nil
}

func (c *PublicClient) resolveHead(ctx context.Context, owner, repo string) (string, string, error) {
	refsURL := fmt.Sprintf("%s/%s/%s.git/info/refs?service=git-upload-pack", strings.TrimRight(c.githubBase, "/"), url.PathEscape(owner), url.PathEscape(repo))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, refsURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("create request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("resolve repository HEAD: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
	if resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrBodyLen))
		return "", "", ErrPublicRepoNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrBodyLen))
		return "", "", fmt.Errorf("resolve repository HEAD: status %d", resp.StatusCode)
	}

	commitSHA, defaultBranch, err := parseUploadPackAdvertisement(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("resolve repository HEAD: %w", err)
	}
	return commitSHA, defaultBranch, nil
}

// parseUploadPackAdvertisement reads a git-upload-pack smart HTTP ref
// advertisement (protocol v0) just far enough to extract the HEAD commit and
// the default branch from the HEAD symref capability, then stops without
// consuming the remaining ref listing.
func parseUploadPackAdvertisement(r io.Reader) (string, string, error) {
	reader := bufio.NewReader(io.LimitReader(r, maxGitAdvertisementBytes))
	for {
		pkt, err := readPktLine(reader)
		if errors.Is(err, io.EOF) {
			// The advertisement ended without a single ref line.
			return "", "", ErrRepositoryEmpty
		}
		if err != nil {
			return "", "", err
		}
		if pkt == nil {
			// Flush packet separates the service announcement from the refs.
			continue
		}
		line := strings.TrimSuffix(string(pkt), "\n")
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}

		refPart, capabilities, _ := strings.Cut(line, "\x00")
		oid, refName, ok := strings.Cut(refPart, " ")
		if !ok || !validObjectID(oid) {
			return "", "", fmt.Errorf("malformed ref advertisement line %q", line)
		}
		if refName == "capabilities^{}" || strings.Trim(oid, "0") == "" {
			return "", "", ErrRepositoryEmpty
		}
		if refName != "HEAD" {
			return "", "", ErrRepositoryEmpty
		}

		for capability := range strings.SplitSeq(capabilities, " ") {
			if branch, ok := strings.CutPrefix(capability, "symref=HEAD:refs/heads/"); ok && branch != "" {
				return oid, branch, nil
			}
		}
		return "", "", fmt.Errorf("advertisement does not include a HEAD symref to a branch")
	}
}

// readPktLine reads one pkt-line. It returns nil payload for a flush packet
// ("0000") and io.ErrUnexpectedEOF style errors for truncated input.
func readPktLine(reader *bufio.Reader) ([]byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, fmt.Errorf("read pkt-line length: %w", err)
	}
	length := 0
	for _, digit := range header {
		value := int(digit)
		switch {
		case digit >= '0' && digit <= '9':
			value -= '0'
		case digit >= 'a' && digit <= 'f':
			value -= 'a' - 10
		default:
			return nil, fmt.Errorf("invalid pkt-line length %q", header)
		}
		length = length<<4 | value
	}
	if length == 0 {
		return nil, nil
	}
	if length < 4 {
		return nil, fmt.Errorf("invalid pkt-line length %d", length)
	}
	payload := make([]byte, length-4)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, fmt.Errorf("read pkt-line payload: %w", err)
	}
	return payload, nil
}

// validObjectID accepts SHA-1 and SHA-256 hex object ids.
func validObjectID(oid string) bool {
	if len(oid) != 40 && len(oid) != 64 {
		return false
	}
	for _, r := range oid {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func parsePublicRepositoryURL(repositoryURL string) (string, string, error) {
	parsed, err := url.Parse(repositoryURL)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "github.com") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", ErrInvalidRepositoryURL
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 {
		return "", "", ErrInvalidRepositoryURL
	}
	owner := parts[0]
	repo := strings.TrimSuffix(parts[1], ".git")
	if !validPublicRepositorySegment(owner) || !validPublicRepositorySegment(repo) {
		return "", "", ErrInvalidRepositoryURL
	}
	return owner, repo, nil
}

func validPublicRepositorySegment(value string) bool {
	if value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '.' && r != '-' {
			return false
		}
	}
	return value != ""
}

func (c *PublicClient) fetchArchive(ctx context.Context, archiveURL string) ([]PublicRepositoryFile, []PublicRepositorySkippedFile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("execute request: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrBodyLen))
		return nil, nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	compressed := &io.LimitedReader{R: resp.Body, N: maxRepositoryArchiveBytes + 1}
	reader, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, nil, fmt.Errorf("open gzip stream: %w", err)
	}
	defer o11y.NoLogDefer(reader.Close)
	uncompressed := &io.LimitedReader{R: reader, N: maxRepositoryUncompressedBytes + 1}
	archive := tar.NewReader(uncompressed)
	files := make([]PublicRepositoryFile, 0)
	skipped := make([]PublicRepositorySkippedFile, 0)
	candidates := 0
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if compressed.N <= 0 || uncompressed.N <= 0 {
				return nil, nil, ErrRepositoryTooLarge
			}
			return nil, nil, fmt.Errorf("read tar stream: %w", err)
		}
		if (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) || path.Base(header.Name) != "SKILL.md" {
			continue
		}
		candidates++
		if candidates > MaxRepositorySkills {
			return nil, nil, ErrTooManySkillFiles
		}

		_, relativePath, found := strings.Cut(header.Name, "/")
		relativePath = path.Clean(relativePath)
		if !found || relativePath == "." || path.IsAbs(relativePath) || strings.HasPrefix(relativePath, "../") {
			return nil, nil, fmt.Errorf("archive contains invalid path %q", header.Name)
		}
		if header.Size > MaxRepositorySkillBytes {
			skipped = append(skipped, PublicRepositorySkippedFile{Path: relativePath, Size: header.Size})
			continue
		}
		content, err := io.ReadAll(archive)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", relativePath, err)
		}
		files = append(files, PublicRepositoryFile{Path: relativePath, Content: string(content)})
	}
	slices.SortFunc(files, func(a, b PublicRepositoryFile) int { return strings.Compare(a.Path, b.Path) })
	slices.SortFunc(skipped, func(a, b PublicRepositorySkippedFile) int { return strings.Compare(a.Path, b.Path) })
	return files, skipped, nil
}
