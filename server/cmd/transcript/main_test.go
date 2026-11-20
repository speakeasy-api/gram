package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/transcript"
)

func scenarioFiles(names ...string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, filepath.Join("testdata", n))
	}
	return out
}

// TestMerge_Scenarios merges every seed scenario in both orders, and each
// file into itself (a resend). matched and added count the entries of the
// files after the first. A case with knownBug states what a correct merge
// would give; the test is skipped while the merge still gives something else.
func TestMerge_Scenarios(t *testing.T) {
	t.Parallel()
	cases := []struct {
		files    []string
		matched  int
		added    int
		knownBug string
	}{
		// Inference never sees the reply to the last prompt.
		{files: scenarioFiles("basic/transcript.jsonl", "basic/inference.jsonl"), matched: 19, added: 0},
		{files: scenarioFiles("basic/inference.jsonl", "basic/transcript.jsonl"), matched: 8, added: 1},
		{files: scenarioFiles("basic/transcript.jsonl", "basic/transcript.jsonl"), matched: 9, added: 0},
		{files: scenarioFiles("basic/inference.jsonl", "basic/inference.jsonl"), matched: 19, added: 0},

		{files: scenarioFiles("parallel-tools/transcript.jsonl", "parallel-tools/inference.jsonl"), matched: 15, added: 0},
		{files: scenarioFiles("parallel-tools/inference.jsonl", "parallel-tools/transcript.jsonl"), matched: 8, added: 1},
		{files: scenarioFiles("parallel-tools/transcript.jsonl", "parallel-tools/transcript.jsonl"), matched: 9, added: 0},
		{files: scenarioFiles("parallel-tools/inference.jsonl", "parallel-tools/inference.jsonl"), matched: 15, added: 0},

		{
			files: scenarioFiles("rewind-same-text/transcript.jsonl", "rewind-same-text/inference.jsonl"), matched: 17, added: 0,
		},
		// Inference cannot tell the two "config.yaml" prompts apart, so the
		// second branch's frames join the first; the transcript then adds the
		// second branch, and its last reply that inference never saw.
		{files: scenarioFiles("rewind-same-text/inference.jsonl", "rewind-same-text/transcript.jsonl"), matched: 5, added: 5},
		{files: scenarioFiles("rewind-same-text/transcript.jsonl", "rewind-same-text/transcript.jsonl"), matched: 10, added: 0},
		{files: scenarioFiles("rewind-same-text/inference.jsonl", "rewind-same-text/inference.jsonl"), matched: 17, added: 0},

		// Inference never sees the replies that end each tree.
		{files: scenarioFiles("compaction/transcript.jsonl", "compaction/inference.jsonl"), matched: 17, added: 0},
		{files: scenarioFiles("compaction/inference.jsonl", "compaction/transcript.jsonl"), matched: 10, added: 2},
		{files: scenarioFiles("compaction/transcript.jsonl", "compaction/transcript.jsonl"), matched: 12, added: 0},
		{files: scenarioFiles("compaction/inference.jsonl", "compaction/inference.jsonl"), matched: 17, added: 0},

		{files: scenarioFiles("system-reminders/transcript.jsonl", "system-reminders/inference.jsonl"), matched: 9, added: 0},
		{files: scenarioFiles("system-reminders/inference.jsonl", "system-reminders/transcript.jsonl"), matched: 5, added: 1},
		{files: scenarioFiles("system-reminders/transcript.jsonl", "system-reminders/transcript.jsonl"), matched: 6, added: 0},
		{files: scenarioFiles("system-reminders/inference.jsonl", "system-reminders/inference.jsonl"), matched: 9, added: 0},
		{
			files: scenarioFiles("system-reminders/transcript.jsonl", "system-reminders/inference-reminder-blocks.jsonl"), matched: 9, added: 0,
		},
		{
			files: scenarioFiles("system-reminders/inference-reminder-blocks.jsonl", "system-reminders/transcript.jsonl"), matched: 5, added: 1,
		},
		{files: scenarioFiles("system-reminders/inference-reminder-blocks.jsonl", "system-reminders/inference-reminder-blocks.jsonl"), matched: 9, added: 0},

		{files: scenarioFiles("compliance/page-1.json", "compliance/page-2.json"), matched: 0, added: 2},
		{files: scenarioFiles("compliance/page-1.json", "compliance/export-full.json"), matched: 6, added: 2},
		{files: scenarioFiles("compliance/export-full.json", "compliance/page-1.json"), matched: 6, added: 0},
		{files: scenarioFiles("compliance/page-1.json", "compliance/page-1.json"), matched: 6, added: 0},
		{files: scenarioFiles("compliance/page-1.json", "compliance/page-2.json", "compliance/export-full.json"), matched: 8, added: 2},

		// The first Compliance page ends before the last prompt, which only
		// inference then adds; inference never sees the last reply.
		{files: scenarioFiles("claude-ai-chat/compliance.json", "claude-ai-chat/inference.jsonl"), matched: 17, added: 1},
		{files: scenarioFiles("claude-ai-chat/inference.jsonl", "claude-ai-chat/compliance.json"), matched: 7, added: 0},
		{files: scenarioFiles("claude-ai-chat/compliance.json", "claude-ai-chat/compliance-page-2.json", "claude-ai-chat/inference.jsonl"), matched: 18, added: 2},
		{files: scenarioFiles("claude-ai-chat/inference.jsonl", "claude-ai-chat/compliance.json", "claude-ai-chat/compliance-page-2.json"), matched: 8, added: 1},
		{files: scenarioFiles("claude-ai-chat/compliance.json", "claude-ai-chat/compliance.json"), matched: 7, added: 0},
		{files: scenarioFiles("claude-ai-chat/inference.jsonl", "claude-ai-chat/inference.jsonl"), matched: 18, added: 0},

		{files: scenarioFiles("cowork-three-way/transcript.jsonl", "cowork-three-way/inference.jsonl"), matched: 32, added: 0},
		{files: scenarioFiles("cowork-three-way/inference.jsonl", "cowork-three-way/transcript.jsonl"), matched: 11, added: 1},
		{files: scenarioFiles("cowork-three-way/transcript.jsonl", "cowork-three-way/compliance.json"), matched: 12, added: 0},
		{files: scenarioFiles("cowork-three-way/compliance.json", "cowork-three-way/transcript.jsonl"), matched: 12, added: 0},
		{files: scenarioFiles("cowork-three-way/inference.jsonl", "cowork-three-way/compliance.json"), matched: 11, added: 1},
		{files: scenarioFiles("cowork-three-way/compliance.json", "cowork-three-way/inference.jsonl"), matched: 32, added: 0},
		{files: scenarioFiles("cowork-three-way/transcript.jsonl", "cowork-three-way/inference.jsonl", "cowork-three-way/compliance.json"), matched: 44, added: 0},
		{files: scenarioFiles("cowork-three-way/compliance.json", "cowork-three-way/inference.jsonl", "cowork-three-way/transcript.jsonl"), matched: 44, added: 0},
		{files: scenarioFiles("cowork-three-way/compliance.json", "cowork-three-way/compliance.json"), matched: 12, added: 0},
	}
	for _, tc := range cases {
		names := make([]string, 0, len(tc.files))
		for _, f := range tc.files {
			names = append(names, strings.TrimPrefix(filepath.ToSlash(f), "testdata/"))
		}
		t.Run(strings.Join(names, "+"), func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			sum, err := mergeCommand(&out, tc.files)
			require.NoError(t, err)
			require.Zero(t, sum.duplicates, "possible duplicates:\n%s", out.String())
			if tc.knownBug != "" {
				require.False(t, sum.matched == tc.matched && sum.added == tc.added,
					"the known bug no longer shows; remove knownBug from this case")
				t.Skipf("known merge bug: %s (got %d matched, %d added; a correct merge gives %d matched, %d added)",
					tc.knownBug, sum.matched, sum.added, tc.matched, tc.added)
			}
			require.Equal(t, tc.matched, sum.matched, "matched\n%s", out.String())
			require.Equal(t, tc.added, sum.added, "added\n%s", out.String())
		})
	}
}

func TestTree_EverySeedFile(t *testing.T) {
	t.Parallel()
	nodes := map[string]int{
		"basic/transcript.jsonl":                           9,
		"basic/inference.jsonl":                            8,
		"parallel-tools/transcript.jsonl":                  9,
		"parallel-tools/inference.jsonl":                   8,
		"rewind-same-text/transcript.jsonl":                10,
		"rewind-same-text/inference.jsonl":                 7,
		"compaction/transcript.jsonl":                      12,
		"compaction/inference.jsonl":                       10,
		"system-reminders/transcript.jsonl":                6,
		"system-reminders/inference.jsonl":                 5,
		"system-reminders/inference-reminder-blocks.jsonl": 5,
		"compliance/page-1.json":                           6,
		"compliance/page-2.json":                           2,
		"compliance/export-full.json":                      8,
		"claude-ai-chat/compliance.json":                   7,
		"claude-ai-chat/compliance-page-2.json":            2,
		"claude-ai-chat/inference.jsonl":                   8,
		"cowork-three-way/transcript.jsonl":                12,
		"cowork-three-way/inference.jsonl":                 11,
		"cowork-three-way/compliance.json":                 12,
	}
	files, err := filepath.Glob(filepath.Join("testdata", "*", "*.json*"))
	require.NoError(t, err)
	require.Len(t, files, len(nodes), "every seed file has an expected node count")
	for _, file := range files {
		rel := strings.TrimPrefix(filepath.ToSlash(file), "testdata/")
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			sum, err := treeCommand(&out, file)
			require.NoError(t, err)
			require.Zero(t, sum.duplicates, "possible duplicates:\n%s", out.String())
			require.Equal(t, nodes[rel], sum.nodes, out.String())
		})
	}
}

func TestTree_CompactionSummaryStartsANewTree(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	_, err := treeCommand(&out, filepath.Join("testdata", "compaction", "transcript.jsonl"))
	require.NoError(t, err)
	require.Contains(t, out.String(), "[synthetic] continues → a3\n")
}

func TestTree_ComplianceSessionSkipsSyntheticMarkers(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	_, err := treeCommand(&out, filepath.Join("testdata", "cowork-three-way", "compliance.json"))
	require.NoError(t, err)
	require.Contains(t, out.String(), "1 synthetic marker messages skipped")
	require.NotContains(t, out.String(), "system prompt is not shown")
}

func TestRun_ExitStatus(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, run([]string{"merge", "testdata/basic/transcript.jsonl", "testdata/basic/inference.jsonl"}, &stdout, &stderr))
	require.Contains(t, stdout.String(), "19 matched · 0 added · 0 possible duplicates\n")

	stderr.Reset()
	require.Equal(t, 2, run([]string{"tree", "testdata/README.md"}, io.Discard, &stderr))
	require.Contains(t, stderr.String(), "unknown input")

	stderr.Reset()
	require.Equal(t, 2, run([]string{"merge", "testdata/basic/transcript.jsonl"}, io.Discard, &stderr))
	require.Contains(t, stderr.String(), "usage:")
}

func TestDuplicates(t *testing.T) {
	t.Parallel()
	line := func(id string) []transcript.NaturalID {
		return []transcript.NaturalID{{Kind: transcript.KindClaudeEntry, Value: id}}
	}
	key := transcript.ContentKey(transcript.Entry{Role: transcript.RoleUser, Text: "yes"})
	siblings := func(aIDs, bIDs []transcript.NaturalID) *session {
		s := newSession()
		top := &transcript.Node{}
		a := &transcript.Node{Parent: top, ContentKey: key, Entry: transcript.Entry{Role: transcript.RoleUser, Text: "yes", IDs: aIDs}}
		b := &transcript.Node{Parent: top, ContentKey: key, Entry: transcript.Entry{Role: transcript.RoleUser, Text: "yes", IDs: bIDs}}
		top.Children = []*transcript.Node{a, b}
		s.add(a, transcript.SourceAnthropicInference)
		s.add(b, transcript.SourceClaudeTranscript)
		return s
	}

	require.Len(t, siblings(nil, line("u1")).duplicates(), 1, "the merge should have matched them")
	require.Empty(t, siblings(line("u1"), line("u2")).duplicates(), "two transcript lines are two entries")
}

// TestClaudeInput_ToleratesRealFileShapes covers shapes seen in real Claude
// Code transcripts that the seed sessions do not have.
func TestClaudeInput_ToleratesRealFileShapes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name string, lines ...string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
		return path
	}

	t.Run("a reply written before the prompt it follows", func(t *testing.T) {
		t.Parallel()
		path := write("late.jsonl",
			`{"type":"attachment","uuid":"a0","parentUuid":null,"sessionId":"s","attachment":{"type":"hook_success"}}`,
			`{"type":"assistant","uuid":"a2","parentUuid":"u1","sessionId":"s","message":{"id":"msg_1","role":"assistant","content":[{"type":"text","text":"Hello."}]}}`,
			`{"type":"user","uuid":"u1","parentUuid":"a0","sessionId":"s","promptId":"p1","message":{"role":"user","content":"Hi"}}`,
		)
		var out bytes.Buffer
		sum, err := treeCommand(&out, path)
		require.NoError(t, err)
		require.Equal(t, 2, sum.nodes)
		require.Contains(t, out.String(), "1 lines come before the line they follow")
	})

	t.Run("a compaction boundary whose old leaf comes after it", func(t *testing.T) {
		t.Parallel()
		path := write("forward.jsonl",
			`{"type":"user","uuid":"u1","parentUuid":null,"sessionId":"s","promptId":"p1","message":{"role":"user","content":"Hi"}}`,
			`{"type":"system","subtype":"compact_boundary","uuid":"b1","parentUuid":null,"logicalParentUuid":"d1","sessionId":"s"}`,
			`{"type":"user","uuid":"s1","parentUuid":"b1","sessionId":"s","isCompactSummary":true,"message":{"role":"user","content":"Summary."}}`,
			`{"type":"system","subtype":"turn_duration","uuid":"d1","parentUuid":"s1","sessionId":"s"}`,
		)
		var out bytes.Buffer
		sum, err := treeCommand(&out, path)
		require.NoError(t, err)
		require.Equal(t, 2, sum.nodes)
		require.Contains(t, out.String(), "1 compaction boundaries name an old leaf")
	})

	t.Run("a parent in another file", func(t *testing.T) {
		t.Parallel()
		path := write("continued.jsonl",
			`{"type":"user","uuid":"u1","parentUuid":"elsewhere","sessionId":"s","promptId":"p1","message":{"role":"user","content":"Hi"}}`,
		)
		var out bytes.Buffer
		sum, err := treeCommand(&out, path)
		require.NoError(t, err)
		require.Equal(t, 1, sum.nodes)
		require.Contains(t, out.String(), "1 lines name a parent that is not in this file")
	})

	t.Run("prompt ids shared by the lines of one turn", func(t *testing.T) {
		t.Parallel()
		path := write("shared-prompt.jsonl",
			`{"type":"user","uuid":"u1","parentUuid":null,"sessionId":"s","promptId":"p1","message":{"role":"user","content":"<command-name>/model</command-name>"}}`,
			`{"type":"user","uuid":"u2","parentUuid":"u1","sessionId":"s","promptId":"p1","message":{"role":"user","content":"Hi"}}`,
		)
		var out bytes.Buffer
		sum, err := treeCommand(&out, path)
		require.NoError(t, err)
		require.Equal(t, 2, sum.nodes, "the second line is not matched to the first by its prompt id")
	})
}
