import { RECENTS_STORAGE_PREFIX } from "@/lib/local-storage-keys";
import { useSessionInfo } from "@gram/client/react-query/sessionInfo.js";
import { useEffect, useState } from "react";

/**
 * Recently-visited pages for the command palette.
 *
 * Kept entirely in localStorage — recents are per-device, ephemeral state, so no
 * backend is involved. Entries store only page labels/paths (no sensitive data)
 * and are scoped by user + org/project so the list stays relevant when switching
 * workspaces and is isolated between users who share a browser profile.
 * Cross-device sync would be the only reason to move this server-side; that's
 * intentionally out of scope.
 */
export interface RecentEntry {
  label: string;
  href: string;
  icon?: string;
  visitedAt: number;
}

const MAX_RECENTS = 6;
const UPDATED_EVENT = "gram:recents-updated";

// Opaque ids make poor Recents labels. UUIDs and other long tokens fall back
// to the section title until a detail page registers a human name.
const UUID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function isOpaqueIdSegment(segment: string): boolean {
  return UUID_RE.test(segment) || segment.length > 24;
}

/**
 * Label a visited page for the Recents list. Section/list pages keep the route
 * title; detail pages prefer the human-readable last path segment (a slug like
 * "notion"). Opaque id segments are not shown — those pages register a name
 * via `useRecentLabelOverride` once their record loads.
 */
export function pageLabel(
  title: string,
  baseHref: string,
  pathname: string,
): string {
  if (pathname === baseHref || !pathname.startsWith(baseHref)) return title;
  const segment = decodeURIComponent(
    pathname.split("/").filter(Boolean).pop() ?? "",
  );
  if (!segment || isOpaqueIdSegment(segment)) return title;
  return segment;
}

// --- Human label overrides for opaque-id detail pages ----------------------
//
// Visits are recorded centrally in App from the active route, which can only
// derive a label from the URL. For pages keyed by an opaque id — e.g. the
// assistant detail page at /assistants/<uuid> or a Project Assistant
// conversation at /chat/<uuid> — that yields the section title instead of the
// resource name. Such pages register a readable label here once their data
// resolves; App consults it when recording the visit and re-records when an
// override arrives asynchronously.
const labelOverrides = new Map<string, string>();
export const RECENTS_LABEL_OVERRIDE_EVENT = "gram:recents-label-override";

/** Register a human label for a visited href (keyed by pathname). */
function setRecentLabelOverride(href: string, label: string): void {
  if (labelOverrides.get(href) === label) return;
  labelOverrides.set(href, label);
  window.dispatchEvent(
    new CustomEvent(RECENTS_LABEL_OVERRIDE_EVENT, { detail: { href } }),
  );
}

/** Read a previously-registered label override for an href, if any. */
export function getRecentLabelOverride(href: string): string | undefined {
  return labelOverrides.get(href);
}

/**
 * Register a readable Recents label for the current detail page. Pages keyed by
 * an opaque id call this once their record loads so the command palette's
 * "Recently Visited" list shows the resource name rather than a raw id. No-ops
 * until `label` is available.
 */
export function useRecentLabelOverride(
  href: string,
  label: string | undefined,
): void {
  useEffect(() => {
    if (!label) return;
    setRecentLabelOverride(href, label);
  }, [href, label]);
}

/**
 * The current user's id, used to scope recents per authenticated user. Derived
 * from the SDK session hook so it works outside the `AuthProvider` (the command
 * palette is mounted above it).
 *
 * `enabled` gates the underlying `auth.info` request: the command palette passes
 * its open state so we never poll `auth.info` on every page (including the
 * unauthenticated login page, where it 401s). Inside `AuthProvider`, prefer
 * `useUser()` instead — the session is already fetched there.
 */
export function useRecentsUserId(enabled = true): string | undefined {
  const { data } = useSessionInfo(undefined, undefined, {
    enabled,
    refetchOnWindowFocus: false,
    retry: false,
    throwOnError: false,
  });
  return data?.result?.userId || undefined;
}

// Bump when the entry shape, href scheme, or label format changes so stale
// entries (e.g. older "Title · <short id>" labels) are dropped.
const STORAGE_VERSION = "v3";

function storageKey(
  userId?: string,
  orgSlug?: string,
  projectSlug?: string,
): string {
  return `${RECENTS_STORAGE_PREFIX}${STORAGE_VERSION}:${userId ?? ""}:${orgSlug ?? ""}:${projectSlug ?? ""}`;
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
  userId: string | undefined,
  orgSlug: string | undefined,
  projectSlug: string | undefined,
  entry: Omit<RecentEntry, "visitedAt">,
): void {
  const key = storageKey(userId, orgSlug, projectSlug);
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
  userId: string | undefined,
  orgSlug: string | undefined,
  projectSlug: string | undefined,
  enabled: boolean,
): RecentEntry[] {
  const key = storageKey(userId, orgSlug, projectSlug);
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
