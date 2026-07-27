import type { UpsertGlobalToolVariationForm } from "@gram/client/models/components/upsertglobaltoolvariationform.js";

export const GOOGLE_WORKSPACE_REGISTRY_SPECIFIER =
  "io.github.taylorwilsdon/workspace-mcp";

type CatalogToolCuration = Omit<
  UpsertGlobalToolVariationForm,
  "srcToolName" | "srcToolUrn"
> & {
  sourceName: string;
};

const GOOGLE_WORKSPACE_TOOL_CURATIONS: CatalogToolCuration[] = [
  {
    sourceName: "import_to_google_doc",
    name: "create_rich_doc",
    title: "Create rich Google Doc",
    description:
      'Use this to create a native Google Doc with headings, formatted text, lists, or tables. Pass the complete document as HTML with source_format: "html". Prefer this over create_doc whenever the request asks for structure or formatting.',
  },
  {
    sourceName: "create_doc",
    description:
      "Create a Google Doc from plain text only. Use create_rich_doc instead when the request includes headings, formatted text, lists, tables, or other rich structure.",
  },
];

export function catalogToolCurations(
  registrySpecifier: string,
  remoteMcpServerId: string,
): UpsertGlobalToolVariationForm[] {
  if (registrySpecifier !== GOOGLE_WORKSPACE_REGISTRY_SPECIFIER) {
    return [];
  }

  return GOOGLE_WORKSPACE_TOOL_CURATIONS.map(
    ({ sourceName, ...variation }) => ({
      ...variation,
      srcToolName: sourceName,
      srcToolUrn: `tools:externalmcp:${remoteMcpServerId}:${sourceName}`,
    }),
  );
}
