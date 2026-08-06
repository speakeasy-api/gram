import type { MCPSetupGuide } from "@gram/client/models/components/mcpsetupguide.js";

// Every guide is also published to the docs site under its own slug, which is
// the guide's identity in the docs catalog rather than the server's in a
// registry: BigQuery is `google-big-query` here and `google-bigquery` in its
// registry specifier.
const GUIDE_DOCS_BASE =
  "https://www.speakeasy.com/docs/ai-control-plane/guides";

export function docsUrl(guide: MCPSetupGuide): string {
  return `${GUIDE_DOCS_BASE}/${guide.slug}`;
}

// Both lookup keys describe the same server, so a second guide only appears
// when they disagree.
export function soleGuide(guides: MCPSetupGuide[]): MCPSetupGuide | undefined {
  return guides.length === 1 ? guides[0] : undefined;
}

/**
 * How the panel header names a guide: the server on the first line, what the
 * panel is holding on the second.
 *
 * Two lookup keys matching different guides leaves no single server to name, so
 * the heading collapses back to one line.
 */
export function setupGuideHeading(guides: MCPSetupGuide[]): {
  title: string;
  subtitle?: string;
} {
  const only = soleGuide(guides);
  return only
    ? { title: only.title, subtitle: "MCP setup guide" }
    : { title: "MCP setup guides" };
}
