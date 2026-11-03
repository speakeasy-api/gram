package demoseed

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The seeded MCP App: a Speakeasy Function exposing one tool whose result renders
// in an HTML UI resource. It exists so the Playground's MCP Apps surface has
// something to run locally without hand-deploying a function first.
//
// Local only. The shared demo org deliberately does not get it: a functions
// deployment is not inert data — production would have to actually provision
// and run it.
const (
	MCPAppFunctionSlug = "playground-mcp-app"
	MCPAppToolName     = "show_dashboard"
	MCPAppResourceURI  = "ui://" + MCPAppFunctionSlug + "/dashboard"
	MCPAppRuntime      = "nodejs:22"
)

// The urns are built through the urn package rather than spelled out: the
// resource urn slugifies its URI, and a hand-written literal would silently
// stop matching what the runtime resolves.
func mcpAppToolURN() string {
	return urn.NewTool(urn.ToolKindFunction, MCPAppFunctionSlug, MCPAppToolName).String()
}

func mcpAppResourceURN() string {
	return urn.NewResource(urn.ResourceKindFunction, MCPAppFunctionSlug, MCPAppResourceURI).String()
}

//go:embed mcpapp/app.html
var mcpAppHTML string

//go:embed mcpapp/functions.js
var mcpAppFunctionsJS string

// mcpAppInputSchema is the tool's declared input, shared by the manifest in
// the archive and the function_tool_definitions row that the dashboard reads.
const mcpAppInputSchema = `{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "A short topic to render inside the seeded dashboard"
    }
  },
  "required": ["query"],
  "additionalProperties": false
}`

const mcpAppToolDescription = "Return demo data for the seeded MCP Apps playground example"

// buildMCPAppArchive produces the function bundle the runner executes: a zip
// of manifest.json plus functions.js, with the app's HTML injected as a
// constant ahead of the module so app.html can stay an ordinary HTML file.
//
// The zip is built deterministically (fixed order, no timestamps) so a reseed
// produces a byte-identical asset and the assets (project_id, sha256) unique
// constraint keeps pointing at the same row.
func buildMCPAppArchive() ([]byte, error) {
	html, err := json.Marshal(mcpAppHTML)
	if err != nil {
		return nil, fmt.Errorf("encode mcp app html: %w", err)
	}
	source := "const HTML = " + string(html) + ";\n" + mcpAppFunctionsJS

	manifest, err := json.MarshalIndent(map[string]any{
		"version": "0.0.0",
		"tools": []map[string]any{{
			"name":        MCPAppToolName,
			"description": mcpAppToolDescription,
			"inputSchema": json.RawMessage(mcpAppInputSchema),
			"meta":        map[string]any{"ui/resourceUri": MCPAppResourceURI},
		}},
		"resources": []map[string]any{{
			"name":        "playground_dashboard",
			"title":       "Playground MCP App",
			"description": "A tiny interactive HTML dashboard for validating MCP Apps in the playground",
			"uri":         MCPAppResourceURI,
			"mimeType":    "text/html;profile=mcp-app",
			"meta":        map[string]any{"ui": map[string]any{"prefersBorder": true}},
		}},
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode mcp app manifest: %w", err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, file := range []struct {
		name string
		body []byte
	}{
		{"manifest.json", manifest},
		{"functions.js", []byte(source)},
	} {
		w, err := zw.Create(file.name)
		if err != nil {
			return nil, fmt.Errorf("add %s to mcp app archive: %w", file.name, err)
		}
		if _, err := w.Write(file.body); err != nil {
			return nil, fmt.Errorf("write %s into mcp app archive: %w", file.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("finalize mcp app archive: %w", err)
	}

	return buf.Bytes(), nil
}

// uploadedAsset describes a blob the fixtures wrote and the assets row that
// must point at it.
type uploadedAsset struct {
	url    string
	sha256 string
	size   int
}

// uploadMCPAppArchive writes the function bundle through the asset store (fs
// locally) and returns what the assets row needs to reference it.
func uploadMCPAppArchive(ctx context.Context, blob assets.BlobStore) (uploadedAsset, error) {
	archive, err := buildMCPAppArchive()
	if err != nil {
		return uploadedAsset{}, err
	}

	sum := sha256.Sum256(archive)
	w, objURL, err := blob.Write(ctx, "demoseed/"+MCPAppFunctionSlug+".zip", "application/zip", int64(len(archive)))
	if err != nil {
		return uploadedAsset{}, fmt.Errorf("open mcp app asset for writing: %w", err)
	}
	if _, err := w.Write(archive); err != nil {
		defer o11y.NoLogDefer(w.Close)
		return uploadedAsset{}, fmt.Errorf("write mcp app asset: %w", err)
	}
	if err := w.Close(); err != nil {
		return uploadedAsset{}, fmt.Errorf("finalize mcp app asset: %w", err)
	}

	return uploadedAsset{
		url:    objURL.String(),
		sha256: hex.EncodeToString(sum[:]),
		size:   len(archive),
	}, nil
}

// MCPAppToolsetSlug is the seeded toolset the MCP App is attached to.
const MCPAppToolsetSlug = "acme-support-tools"
