import {
  defineFilters,
  type FilterValues,
  type OptionsById,
} from "@/components/filters";
import { attachmentToURNPrefix } from "@/lib/sources";
import type { SourceOption } from "./source-list";

/**
 * Filters for the sources shelf.
 *
 * Kind is what tells sources apart at a glance. The rest answer the questions
 * people bring to the list: is this source serving anything, and is it the
 * one that broke the last push.
 */
export const SOURCE_FILTERS = defineFilters([
  { id: "kind", label: "Kind", kind: "multiselect" },
  {
    id: "usedInMcp",
    label: "Used in MCP",
    kind: "select",
    allLabel: "Any MCP usage",
    description:
      "Whether an MCP server carries tools generated from the source.",
  },
  {
    id: "format",
    label: "Format",
    kind: "multiselect",
    description: "OpenAPI documents only. Functions have no format.",
  },
  {
    id: "failing",
    label: "Deployment errors",
    kind: "boolean",
    description: "Only sources that caused the latest deployment to fail.",
  },
]);

export type SourceFilterValues = FilterValues<typeof SOURCE_FILTERS>;

export const SOURCE_FILTER_OPTIONS: OptionsById = {
  kind: [
    { value: "openapi", label: "OpenAPI document" },
    { value: "function", label: "Function" },
  ],
  usedInMcp: [
    { value: "yes", label: "Used in an MCP server" },
    { value: "no", label: "Not used in any MCP server" },
  ],
  format: [
    { value: "json", label: "JSON" },
    { value: "yaml", label: "YAML" },
  ],
};

export type SourceFormat = "json" | "yaml";

/** Map an asset's content type onto the coarse format facet. */
export function contentTypeToFormat(
  contentType: string | undefined,
): SourceFormat | undefined {
  if (!contentType) return undefined;
  if (contentType.includes("yaml") || contentType.includes("yml")) {
    return "yaml";
  }
  if (contentType.includes("json")) return "json";
  return undefined;
}

/**
 * The key a source's deployment failure is looked up by.
 *
 * Failures are logged against the latest deployment while the list reads the
 * active one, and every push mints new deployment-asset ids, so the two can't
 * be joined by id. Kind and slug survive a new version.
 */
export function sourceFailureKey(source: {
  kind: SourceOption["kind"];
  slug?: string | undefined;
}): string {
  return `${source.kind}:${source.slug ?? ""}`;
}

/**
 * Hosted MCP servers are toolsets, and a toolset names its tools by URN under
 * the source's own prefix, so a source is "used" when any toolset carries a
 * URN under it.
 */
export function sourceUsedInMcp(
  source: SourceOption,
  toolsetToolUrns: string[],
): boolean {
  if (!source.slug) return false;
  const prefix = attachmentToURNPrefix(source.kind, source.slug);
  return toolsetToolUrns.some((urn) => urn.startsWith(prefix));
}

export interface SourceFacets {
  kind: SourceOption["kind"];
  usedInMcp: boolean;
  /** OpenAPI only. */
  format?: SourceFormat | undefined;
  failing: boolean;
}

export function sourceFacets(
  source: SourceOption,
  toolsetToolUrns: string[],
  failingKeys: ReadonlySet<string>,
): SourceFacets {
  return {
    kind: source.kind,
    usedInMcp: sourceUsedInMcp(source, toolsetToolUrns),
    format:
      source.kind === "openapi"
        ? contentTypeToFormat(source.contentType)
        : undefined,
    failing: failingKeys.has(sourceFailureKey(source)),
  };
}

export function matchesSourceFilters(
  facets: SourceFacets,
  values: SourceFilterValues,
): boolean {
  if (values.kind.length > 0 && !values.kind.includes(facets.kind)) {
    return false;
  }

  // Only the declared "yes"/"no" values filter; a stale or malformed URL param
  // (e.g. ?usedInMcp=maybe) is ignored rather than silently treated as "no".
  if (values.usedInMcp === "yes" || values.usedInMcp === "no") {
    const wantUsed = values.usedInMcp === "yes";
    if (facets.usedInMcp !== wantUsed) return false;
  }

  if (values.failing && !facets.failing) return false;

  // Functions have no format, so an active format filter excludes them, the
  // same way picking a kind narrows the list.
  if (values.format.length > 0) {
    if (!facets.format || !values.format.includes(facets.format)) return false;
  }

  return true;
}

/** Name or slug: the CLI hands people slugs, so those must find the source too. */
export function matchesSourceSearch(
  source: Pick<SourceOption, "name" | "slug">,
  query: string,
): boolean {
  const needle = query.trim().toLowerCase();
  if (!needle) return true;
  return (
    source.name.toLowerCase().includes(needle) ||
    (source.slug?.toLowerCase().includes(needle) ?? false)
  );
}

export function hasActiveSourceFilters(values: SourceFilterValues): boolean {
  return (
    values.kind.length > 0 ||
    values.format.length > 0 ||
    values.usedInMcp !== null ||
    values.failing
  );
}
