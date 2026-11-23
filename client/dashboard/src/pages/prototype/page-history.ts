import { findResource } from "@/resources/registry";
import { useCallback, useState } from "react";
import type { ResourceLocation } from "@/resources/kit/ResourceView";

/** A page open on the surface. */
export interface OpenPage {
  resource: string;
  location: ResourceLocation;
}

// ---------------------------------------------------------------------------
// History: the surface keeps a browser-style back/forward stack.
// ---------------------------------------------------------------------------

export interface PageHistory {
  page: OpenPage | null;
  open: (page: OpenPage | null) => void;
  back: () => void;
  forward: () => void;
  canBack: boolean;
  canForward: boolean;
  /** Pages opened this session, newest first, without repeats. */
  recent: OpenPage[];
}

function samePage(a: OpenPage | null, b: OpenPage | null): boolean {
  return addressPath(a) === addressPath(b);
}

export function usePageHistory(): PageHistory {
  const [stack, setStack] = useState<{
    entries: (OpenPage | null)[];
    index: number;
  }>({ entries: [null], index: 0 });

  const open = useCallback((page: OpenPage | null) => {
    setStack((s) => {
      if (samePage(s.entries[s.index] ?? null, page)) return s;
      const entries = [...s.entries.slice(0, s.index + 1), page];
      return { entries, index: entries.length - 1 };
    });
  }, []);

  const page = stack.entries[stack.index] ?? null;
  const recent: OpenPage[] = [];
  for (const entry of [...stack.entries].reverse()) {
    if (entry && !recent.some((r) => samePage(r, entry))) recent.push(entry);
  }

  return {
    page,
    open,
    back: () => setStack((s) => ({ ...s, index: Math.max(0, s.index - 1) })),
    forward: () =>
      setStack((s) => ({
        ...s,
        index: Math.min(s.entries.length - 1, s.index + 1),
      })),
    canBack: stack.index > 0,
    canForward: stack.index < stack.entries.length - 1,
    recent: recent.slice(0, 6),
  };
}

// ---------------------------------------------------------------------------
// Addresses: every page has one, and typing one opens it.
// ---------------------------------------------------------------------------

function addressPath(page: OpenPage | null): string {
  if (!page) return "";
  return page.location.view === "record"
    ? `${page.resource}/${page.location.id}`
    : page.resource;
}

export function pageAddress(project: string, page: OpenPage | null): string {
  const path = addressPath(page);
  return `speakeasy://${project}${path ? `/${path}` : ""}`;
}

/** Accepts a full address or just `resource[/id]`. */
export function parseAddress(
  input: string,
  project: string,
): OpenPage | null | "invalid" {
  const trimmed = input
    .trim()
    .replace(/^speakeasy:\/\//, "")
    .replace(new RegExp(`^${project}/?`), "")
    .replace(/^\/+|\/+$/g, "");
  if (!trimmed) return null;
  const [resource, ...rest] = trimmed.split("/");
  if (!resource || !findResource(resource)) return "invalid";
  const id = rest.join("/");
  return id
    ? { resource, location: { view: "record", id } }
    : { resource, location: { view: "list" } };
}
