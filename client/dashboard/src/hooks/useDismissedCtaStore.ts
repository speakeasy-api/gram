import { useSyncExternalStore } from "react";

/**
 * Creates a localStorage-backed, slug-scoped boolean flag with a module-level
 * pub/sub. Used for "dismiss to the sidebar" CTAs whose two surfaces live in
 * different parts of the tree (e.g. a banner or dock plus a sidebar resume
 * button), so they sync off a single source of truth instead of a shared
 * React context.
 */
export function createDismissedCtaStore(prefix: string): {
  useDismissed: (slug: string | undefined) => boolean;
  write: (slug: string, value: boolean) => void;
} {
  const listeners = new Set<() => void>();
  const storageKey = (slug: string) => `${prefix}:${slug}`;
  // Session-scoped fallback for when localStorage is unavailable (storage
  // disabled, some private-browsing modes): writes land here regardless, so
  // dismiss/resume still works for the session — it just won't persist.
  const memory = new Map<string, boolean>();

  function read(slug: string): boolean {
    try {
      return localStorage.getItem(storageKey(slug)) === "true";
    } catch {
      return memory.get(slug) ?? false;
    }
  }

  function subscribe(listener: () => void) {
    listeners.add(listener);
    return () => {
      listeners.delete(listener);
    };
  }

  function write(slug: string, value: boolean) {
    memory.set(slug, value);
    try {
      if (value) {
        localStorage.setItem(storageKey(slug), "true");
      } else {
        localStorage.removeItem(storageKey(slug));
      }
    } catch {
      // localStorage unavailable — `memory` above keeps the value readable
      // for the session
    }
    listeners.forEach((listener) => listener());
  }

  function useDismissed(slug: string | undefined): boolean {
    return useSyncExternalStore(
      subscribe,
      () => (slug ? read(slug) : false),
      () => false,
    );
  }

  return { useDismissed, write };
}
