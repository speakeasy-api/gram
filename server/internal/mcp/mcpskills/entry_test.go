package mcpskills_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
)

func TestEntryWireMatchesSpecShape(t *testing.T) {
	t.Parallel()

	content := "---\nname: git-workflow\ndescription: Follow this team's Git conventions for branching and commits\nlicense: Apache-2.0\n---\n\n# Git workflow\n"
	digest := sha256.Sum256([]byte(content))
	entry := mcpskills.Entry{
		SkillID:   uuid.New(),
		VersionID: uuid.New(),
		PluginID:  uuid.New(),
		Name:      "git-workflow",
		Content:   content,
		RawSHA256: hex.EncodeToString(digest[:]),
		Frontmatter: map[string]any{
			"name":        "git-workflow",
			"description": "Follow this team's Git conventions for branching and commits",
			"license":     "Apache-2.0",
		},
		Pinned: false,
	}

	encoded, err := json.Marshal(entry.Wire())
	require.NoError(t, err)

	expected := `{
	  "uri": "skill://git-workflow/SKILL.md",
	  "frontmatter": {
	    "name": "git-workflow",
	    "description": "Follow this team's Git conventions for branching and commits",
	    "license": "Apache-2.0"
	  },
	  "resources": [
	    {
	      "uri": "skill://git-workflow/SKILL.md",
	      "digest": "sha256:` + hex.EncodeToString(digest[:]) + `",
	      "size": ` + jsonInt(len(content)) + `
	    }
	  ]
	}`
	require.JSONEq(t, expected, string(encoded))
}

func TestEntryDigestIsLowercaseHex(t *testing.T) {
	t.Parallel()

	entry := mcpskills.Entry{
		SkillID: uuid.Nil, VersionID: uuid.Nil, PluginID: uuid.Nil, Name: "x", Content: "", Frontmatter: nil, Pinned: false,
		RawSHA256: "ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789",
	}
	require.Equal(t, "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", entry.Digest())
}

func TestEntrySizeCountsBytes(t *testing.T) {
	t.Parallel()

	entry := mcpskills.Entry{
		SkillID: uuid.Nil, VersionID: uuid.Nil, PluginID: uuid.Nil, Name: "x", Content: "héllo", RawSHA256: "", Frontmatter: nil, Pinned: false,
	}
	require.Equal(t, 6, entry.Size())
	require.Equal(t, 6, entry.Wire().Resources[0].Size)
}

func jsonInt(v int) string {
	encoded, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
