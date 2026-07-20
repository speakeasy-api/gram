package github

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func pktLine(payload string) string {
	return fmt.Sprintf("%04x%s", len(payload)+4, payload)
}

func advertisement(refLines ...string) string {
	var builder strings.Builder
	builder.WriteString(pktLine("# service=git-upload-pack\n"))
	builder.WriteString("0000")
	for _, line := range refLines {
		builder.WriteString(pktLine(line))
	}
	builder.WriteString("0000")
	return builder.String()
}

func TestPublicClientFetchSkillFiles(t *testing.T) {
	t.Parallel()

	validContent := "---\nname: release-notes\ndescription: Draft release notes.\n---\n# Release notes\n"
	invalidContent := "not a skill manifest"
	commitSHA := "0123456789abcdef0123456789abcdef01234567"
	archive := repositoryArchive(t, []PublicRepositoryFile{
		{Path: "nested/SKILL.md", Content: validContent},
		{Path: "SKILL.md", Content: invalidContent},
		{Path: "ignored/skill.md", Content: "ignored"},
		{Path: "huge/SKILL.md", Content: strings.Repeat("x", MaxRepositorySkillBytes+1)},
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/example/repo.git/info/refs":
			if r.URL.Query().Get("service") != "git-upload-pack" {
				http.Error(w, "unexpected git service", http.StatusBadRequest)
				return
			}
			response := advertisement(commitSHA + " HEAD\x00multi_ack thin-pack symref=HEAD:refs/heads/main agent=git/github\n")
			if _, err := w.Write([]byte(response)); err != nil {
				t.Errorf("write response: %v", err)
			}
		case "/example/repo/tar.gz/" + commitSHA:
			if _, err := w.Write(archive); err != nil {
				t.Errorf("write response: %v", err)
			}
		default:
			t.Errorf("unexpected request: %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := &PublicClient{httpClient: server.Client(), githubBase: server.URL, archiveBase: server.URL}
	snapshot, err := client.FetchSkillFiles(t.Context(), "https://github.com/example/repo")
	require.NoError(t, err)
	require.Equal(t, "example/repo", snapshot.FullName)
	require.Equal(t, commitSHA, snapshot.CommitSHA)
	require.Equal(t, "main", snapshot.DefaultBranch)
	require.Equal(t, []PublicRepositoryFile{
		{Path: "SKILL.md", Content: invalidContent},
		{Path: "nested/SKILL.md", Content: validContent},
	}, snapshot.Files)
	require.Equal(t, []PublicRepositorySkippedFile{
		{Path: "huge/SKILL.md", Size: MaxRepositorySkillBytes + 1},
	}, snapshot.Skipped)
}

func TestPublicClientFetchSkillFilesTooManyCandidates(t *testing.T) {
	t.Parallel()

	commitSHA := "0123456789abcdef0123456789abcdef01234567"
	files := make([]PublicRepositoryFile, 0, MaxRepositorySkills+1)
	for i := range MaxRepositorySkills + 1 {
		files = append(files, PublicRepositoryFile{Path: fmt.Sprintf("skills/skill-%d/SKILL.md", i), Content: "x"})
	}
	archive := repositoryArchive(t, files)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/example/repo.git/info/refs":
			response := advertisement(commitSHA + " HEAD\x00symref=HEAD:refs/heads/main\n")
			if _, err := w.Write([]byte(response)); err != nil {
				t.Errorf("write response: %v", err)
			}
		case "/example/repo/tar.gz/" + commitSHA:
			if _, err := w.Write(archive); err != nil {
				t.Errorf("write response: %v", err)
			}
		default:
			t.Errorf("unexpected request: %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := &PublicClient{httpClient: server.Client(), githubBase: server.URL, archiveBase: server.URL}
	_, err := client.FetchSkillFiles(t.Context(), "https://github.com/example/repo")
	require.ErrorIs(t, err, ErrTooManySkillFiles)
}

func TestParseUploadPackAdvertisementBranchWithSlashes(t *testing.T) {
	t.Parallel()

	commitSHA := "0123456789abcdef0123456789abcdef01234567"
	response := advertisement(commitSHA + " HEAD\x00thin-pack symref=HEAD:refs/heads/release/v2 agent=git/github\n")
	oid, branch, err := parseUploadPackAdvertisement(strings.NewReader(response))
	require.NoError(t, err)
	require.Equal(t, commitSHA, oid)
	require.Equal(t, "release/v2", branch)
}

func TestParseUploadPackAdvertisementStopsAfterHeadLine(t *testing.T) {
	t.Parallel()

	commitSHA := "0123456789abcdef0123456789abcdef01234567"
	var builder strings.Builder
	builder.WriteString(pktLine("# service=git-upload-pack\n"))
	builder.WriteString("0000")
	builder.WriteString(pktLine(commitSHA + " HEAD\x00symref=HEAD:refs/heads/main\n"))
	// Follow with more ref data than the advertisement byte limit to prove
	// the parser stops reading after the HEAD line.
	for range 40000 {
		builder.WriteString(pktLine(commitSHA + " refs/tags/some-long-tag-name-padding-the-advertisement\n"))
	}
	builder.WriteString("0000")

	oid, branch, err := parseUploadPackAdvertisement(strings.NewReader(builder.String()))
	require.NoError(t, err)
	require.Equal(t, commitSHA, oid)
	require.Equal(t, "main", branch)
}

func TestParseUploadPackAdvertisementEmptyRepository(t *testing.T) {
	t.Parallel()

	zeroID := strings.Repeat("0", 40)
	response := advertisement(zeroID + " capabilities^{}\x00symref=HEAD:refs/heads/main\n")
	_, _, err := parseUploadPackAdvertisement(strings.NewReader(response))
	require.ErrorIs(t, err, ErrRepositoryEmpty)

	_, _, err = parseUploadPackAdvertisement(strings.NewReader(pktLine("# service=git-upload-pack\n") + "0000" + "0000"))
	require.ErrorIs(t, err, ErrRepositoryEmpty)
}

func TestParseUploadPackAdvertisementMissingSymref(t *testing.T) {
	t.Parallel()

	commitSHA := "0123456789abcdef0123456789abcdef01234567"
	response := advertisement(commitSHA + " HEAD\x00multi_ack thin-pack\n")
	_, _, err := parseUploadPackAdvertisement(strings.NewReader(response))
	require.Error(t, err)
	require.Contains(t, err.Error(), "HEAD symref")
}

func TestParseUploadPackAdvertisementMalformedInput(t *testing.T) {
	t.Parallel()

	_, _, err := parseUploadPackAdvertisement(strings.NewReader("zzzz nonsense"))
	require.Error(t, err)

	_, _, err = parseUploadPackAdvertisement(strings.NewReader(pktLine("not a ref line\n")))
	require.Error(t, err)

	truncated := pktLine("# service=git-upload-pack\n") + "0000" + "00ff0123"
	_, _, err = parseUploadPackAdvertisement(strings.NewReader(truncated))
	require.Error(t, err)
}

func repositoryArchive(t *testing.T, files []PublicRepositoryFile) []byte {
	t.Helper()

	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, file := range files {
		require.NoError(t, tarWriter.WriteHeader(&tar.Header{
			Name:     "repo-root/" + file.Path,
			Mode:     0o644,
			Size:     int64(len(file.Content)),
			Typeflag: tar.TypeReg,
		}))
		_, err := tarWriter.Write([]byte(file.Content))
		require.NoError(t, err)
	}
	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())
	return buf.Bytes()
}

func TestParsePublicRepositoryURLRejectsTraversal(t *testing.T) {
	t.Parallel()

	_, _, err := parsePublicRepositoryURL("https://github.com/../repos")
	require.ErrorIs(t, err, ErrInvalidRepositoryURL)
	_, _, err = parsePublicRepositoryURL("https://example.com/example/repo")
	require.ErrorIs(t, err, ErrInvalidRepositoryURL)
	_, _, err = parsePublicRepositoryURL("https://github.com/example/repo/issues")
	require.ErrorIs(t, err, ErrInvalidRepositoryURL)
}
