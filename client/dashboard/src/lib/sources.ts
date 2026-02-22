/**
 * Source type as used in deployment assets ("openapi", "function", "externalmcp").
 * URN kind as used in tool URNs ("http", "function", "externalmcp").
 *
 * The only non-trivial mapping is "openapi" ↔ "http".
 */

export type SourceType = "openapi" | "function" | "externalmcp";
export type UrnKind = "http" | "function" | "externalmcp";

const sourceTypeToUrn: Record<SourceType, UrnKind> = {
  openapi: "http",
  function: "function",
  externalmcp: "externalmcp",
};

const urnToSourceType: Record<UrnKind, SourceType> = {
  http: "openapi",
  function: "function",
  externalmcp: "externalmcp",
};

export function sourceTypeToUrnKind(type: SourceType): UrnKind {
  return sourceTypeToUrn[type];
}

export function urnKindToSourceType(kind: UrnKind): SourceType {
  return urnToSourceType[kind];
}

export function attachmentToURNPrefix(type: SourceType, slug: string): string {
  return `tools:${sourceTypeToUrnKind(type)}:${slug}:`;
}
