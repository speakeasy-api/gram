import type { BusinessMemoryContentScopeNode } from "@gram/client/models/components/businessmemorycontentscopenode.js";

export type ScopeSelection =
  | { kind: "namespace"; value: string }
  | { kind: "tag"; value: string };

interface ScopeChild {
  label: string;
  tag: string;
  memoryCount: number;
}

export interface ScopeTreeNode {
  namespace: string;
  memoryCount: number;
  children: ScopeChild[];
}

function splitScopeTag(tag: string): {
  namespace: string;
  child: string | null;
} {
  const separator = tag.indexOf(":");
  if (separator < 0) return { namespace: tag, child: null };
  return {
    namespace: tag.slice(0, separator),
    child: tag.slice(separator + 1),
  };
}

export function buildScopeTree(
  contentScopes: readonly BusinessMemoryContentScopeNode[],
): ScopeTreeNode[] {
  const namespaces = new Map<string, ScopeTreeNode>();

  for (const node of contentScopes) {
    if (node.parentScope || !node.scope) continue;
    namespaces.set(node.scope, {
      namespace: node.scope,
      memoryCount: node.memoryCount,
      children: [],
    });
  }

  for (const node of contentScopes) {
    if (!node.parentScope) continue;
    const namespace = namespaces.get(node.parentScope);
    if (!namespace) continue;
    namespace.children.push({
      label: splitScopeTag(node.scope).child ?? node.scope,
      tag: node.scope,
      memoryCount: node.memoryCount,
    });
  }

  return [...namespaces.values()]
    .map((node) => ({
      ...node,
      children: node.children.sort((a, b) => a.label.localeCompare(b.label)),
    }))
    .sort((a, b) => a.namespace.localeCompare(b.namespace));
}

export function scopeSelectionToFilter(selection: ScopeSelection | null): {
  contentScope?: string;
  contentScopeNamespace?: string;
} {
  if (!selection) return {};
  if (selection.kind === "tag") {
    return { contentScope: selection.value };
  }
  return { contentScopeNamespace: selection.value };
}
