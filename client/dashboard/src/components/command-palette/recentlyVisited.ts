import { useEffect, useState } from "react";

/**
 * Recently-visited pages for the command palette.
 *
 * Kept entirely in localStorage — recents are per-device, ephemeral state, so no
 * backend is involved. Entries store only page labels/paths (no sensitive data)
 * and are scoped by org/project so the list stays relevant when switching
 * workspaces. They are NOT scoped per authenticated user: anyone using the same
 * browser profile shares the list, which is acceptable for non-sensitive
 * navigation history. Cross-device (or per-user) sync would be the only reason
 * to move this server-side; that's intentionally out of scope.
 */
export interface RecentEntry {
  label: string;
  href: string;
  icon?: string;
  visitedAt: number;
}

const MAX_RECENTS = 6;
const UPDATED_EVENT = "gram:recents-updated";

function storageKey(orgSlug?: string, projectSlug?: string): string {
  return `gram:recents:${orgSlug ?? ""}:${projectSlug ?? ""}`;
}

function read(key: string): RecentEntry[] {
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return [];
    const parsed: unknown = JSON.parse(raw);
    return Array.isArray(parsed) ? (parsed as RecentEntry[]) : [];
  } catch {
    return [];
  }
}

/** Record a page visit. Dedupes by href (most-recent wins) and caps the list. */
export function recordVisit(
  orgSlug: string | undefined,
  projectSlug: string | undefined,
  entry: Omit<RecentEntry, "visitedAt">,
): void {
  const key = storageKey(orgSlug, projectSlug);
  const next = [
    { ...entry, visitedAt: Date.now() },
    ...read(key).filter((e) => e.href !== entry.href),
  ].slice(0, MAX_RECENTS);
  try {
    localStorage.setItem(key, JSON.stringify(next));
    // The native `storage` event only fires in *other* tabs, so notify this one.
    window.dispatchEvent(new Event(UPDATED_EVENT));
  } catch {
    // Storage disabled or over quota — recents are best-effort, so ignore.
  }
}

/**
 * Read the recents for the current scope. `enabled` (the palette's open state)
 * gates the read so we only touch localStorage when the palette is shown.
 */
export function useRecentlyVisited(
  orgSlug: string | undefined,
  projectSlug: string | undefined,
  enabled: boolean,
): RecentEntry[] {
  const key = storageKey(orgSlug, projectSlug);
  const [entries, setEntries] = useState<RecentEntry[]>([]);

  useEffect(() => {
    if (!enabled) return;
    const refresh = () => setEntries(read(key));
    refresh();
    window.addEventListener(UPDATED_EVENT, refresh);
    window.addEventListener("storage", refresh);
    return () => {
      window.removeEventListener(UPDATED_EVENT, refresh);
      window.removeEventListener("storage", refresh);
    };
  }, [key, enabled]);

  return entries;
}
