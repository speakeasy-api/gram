package mcp

import "github.com/speakeasy-api/gram/server/internal/platformtools"

// SetPlatformThreadToolForTest replaces a builtin only in the test binary.
func SetPlatformThreadToolForTest(s *Service, slug string, tool platformtools.PlatformToolExecutor) {
	s.platformToolsets[slug] = platformtools.Toolset{Slug: slug, Tools: []platformtools.ExternalTool{{Executor: tool, RequiredFeature: "", RequiredScope: ""}}}
}
