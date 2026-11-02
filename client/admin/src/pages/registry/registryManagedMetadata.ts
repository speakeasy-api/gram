import {
  applyEdits,
  findNodeAtLocation,
  modify,
  parseTree,
} from "jsonc-parser";
import type { ValidationIssue } from "@/lib/registryValidation";

const namespaces = ["com.speakeasy.ai/icons", "com.speakeasy.ai/registry"];

export function managedMetadataIssues(text: string): ValidationIssue[] {
  const root = parseTree(text);
  const issues: ValidationIssue[] = [];
  let seenMeta = false;
  for (const property of root?.children ?? []) {
    if (property.children?.[0]?.value !== "_meta") continue;
    if (seenMeta)
      issues.push({
        path: "/_meta",
        message: "Remove duplicate _meta properties before saving.",
      });
    seenMeta = true;
    const meta = property.children?.[1];
    if (meta?.type !== "object") {
      issues.push({
        path: "/_meta",
        message:
          "Use an object for _meta, or remove it. Icons and publication metadata are managed separately.",
      });
      continue;
    }
    for (const child of meta.children ?? []) {
      const key = child.children?.[0]?.value;
      if (namespaces.includes(key))
        issues.push({
          path: `/_meta/${key.replaceAll("/", "~1")}`,
          message: `Remove ${key} from Record JSON. Icons and publication metadata are managed separately; use the icon controls or publication action instead.`,
        });
    }
  }
  return issues;
}

// Insert only a placeholder through modify, then splice the original JSON value.
// Unknown fields and numeric lexemes must never be reserialized.
function copyManagedMetadata(text: string, source: string): string {
  const root = parseTree(source);
  for (const key of namespaces) {
    const path = ["_meta", key];
    const node = root && findNodeAtLocation(root, path);
    if (!node) continue;
    text = applyEdits(text, modify(text, path, null, {}));
    const placeholder = findNodeAtLocation(parseTree(text)!, path)!;
    text =
      text.slice(0, placeholder.offset) +
      source.slice(node.offset, node.offset + node.length) +
      text.slice(placeholder.offset + placeholder.length);
  }
  return text;
}

export function splitManagedMetadata(text: string): {
  text: string;
  managed: string;
} {
  // Stored records are canonical JSONB. If ambiguous JSON ever reaches this
  // boundary, leave it repairable: existing validation blocks Save, not render.
  const metas =
    parseTree(text)?.children?.filter(
      (property) => property.children?.[0]?.value === "_meta",
    ) ?? [];
  if (metas.length > 1) return { text, managed: "{}" };
  const keys =
    metas[0]?.children?.[1]?.children?.map(
      (property) => property.children?.[0]?.value,
    ) ?? [];
  if (
    namespaces.some(
      (key) => keys.filter((candidate) => candidate === key).length > 1,
    )
  )
    return { text, managed: "{}" };
  const managed = copyManagedMetadata("{}", text);
  for (const key of namespaces) {
    const root = parseTree(text);
    if (root && findNodeAtLocation(root, ["_meta", key]))
      text = applyEdits(text, modify(text, ["_meta", key], undefined, {}));
  }
  return { text, managed };
}

export function mergeManagedMetadata(text: string, managed: string): string {
  const issues = managedMetadataIssues(text);
  if (issues.length) throw new Error(issues[0]!.message);
  return copyManagedMetadata(text, managed);
}
