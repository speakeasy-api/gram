import { createDismissedCtaStore } from "@/hooks/useDismissedCtaStore";
import { useSyncExternalStore } from "react";

const startedStore = createDismissedCtaStore("gram-project-guide-started");
const mcpServerStoragePrefix = "gram-project-guide-mcp-server";
const mcpServerListeners = new Set<() => void>();
const mcpServerMemory = new Map<string, string>();

function projectGuideScope(
  orgSlug: string | undefined,
  projectSlug: string | undefined,
): string | undefined {
  if (!orgSlug || !projectSlug) return undefined;
  return `${orgSlug}:${projectSlug}`;
}

export function useProjectGuideStarted(
  orgSlug: string | undefined,
  projectSlug: string | undefined,
): boolean {
  return startedStore.useDismissed(projectGuideScope(orgSlug, projectSlug));
}

export function markProjectGuideStarted(
  orgSlug: string,
  projectSlug: string,
): void {
  const scope = projectGuideScope(orgSlug, projectSlug);
  if (scope) startedStore.write(scope, true);
}

function mcpServerStorageKey(scope: string): string {
  return `${mcpServerStoragePrefix}:${scope}`;
}

function readMcpServerSelection(scope: string | undefined): string | undefined {
  if (!scope) return undefined;
  const cached = mcpServerMemory.get(scope);
  if (cached !== undefined) return cached;
  try {
    return localStorage.getItem(mcpServerStorageKey(scope)) ?? undefined;
  } catch {
    return undefined;
  }
}

function subscribeToMcpServerSelection(listener: () => void): () => void {
  mcpServerListeners.add(listener);
  const onStorage = (event: StorageEvent) => {
    if (event.key === null) {
      mcpServerMemory.clear();
      listener();
      return;
    }
    if (!event.key.startsWith(`${mcpServerStoragePrefix}:`)) return;
    const scope = event.key.slice(mcpServerStoragePrefix.length + 1);
    if (!scope) return;
    if (event.newValue === null) {
      mcpServerMemory.delete(scope);
    } else {
      mcpServerMemory.set(scope, event.newValue);
    }
    listener();
  };
  window.addEventListener("storage", onStorage);
  return () => {
    mcpServerListeners.delete(listener);
    window.removeEventListener("storage", onStorage);
  };
}

export function useProjectGuideMcpServerSelection(
  orgSlug: string | undefined,
  projectSlug: string | undefined,
): string | undefined {
  const scope = projectGuideScope(orgSlug, projectSlug);
  return useSyncExternalStore(
    subscribeToMcpServerSelection,
    () => readMcpServerSelection(scope),
    () => undefined,
  );
}

export function markProjectGuideMcpServerSelected(
  orgSlug: string,
  projectSlug: string,
  registrySpecifier: string,
): void {
  const scope = projectGuideScope(orgSlug, projectSlug);
  if (!scope) return;
  mcpServerMemory.set(scope, registrySpecifier);
  try {
    localStorage.setItem(mcpServerStorageKey(scope), registrySpecifier);
  } catch {
    // localStorage unavailable — memory above keeps the selection readable
    // for this session.
  }
  mcpServerListeners.forEach((listener) => listener());
}
