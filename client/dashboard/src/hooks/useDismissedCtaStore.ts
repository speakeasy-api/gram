import { useSyncExternalStore } from "react";

type ScopedStorageStore<T> = {
  useValue: (slug: string | undefined) => T;
  write: (slug: string, value: T) => void;
};

export function createScopedStorageStore<T>(
  prefix: string,
  defaultValue: T,
  decode: (stored: string | null) => T,
  encode: (value: T) => string | null,
): ScopedStorageStore<T> {
  const listeners = new Set<() => void>();
  const storageKey = (slug: string) => `${prefix}:${slug}`;
  const memory = new Map<string, T>();

  function read(slug: string | undefined): T {
    if (!slug) return defaultValue;
    if (memory.has(slug)) return memory.get(slug)!;
    try {
      return decode(localStorage.getItem(storageKey(slug)));
    } catch {
      return defaultValue;
    }
  }

  function subscribe(listener: () => void) {
    listeners.add(listener);
    const onStorage = (event: StorageEvent) => {
      if (event.key === null) {
        memory.clear();
        listener();
        return;
      }
      if (!event.key.startsWith(`${prefix}:`)) return;
      const slug = event.key.slice(prefix.length + 1);
      if (!slug) return;
      memory.set(slug, decode(event.newValue));
      listener();
    };
    window.addEventListener("storage", onStorage);
    return () => {
      listeners.delete(listener);
      window.removeEventListener("storage", onStorage);
    };
  }

  function write(slug: string, value: T) {
    memory.set(slug, value);
    try {
      const serialized = encode(value);
      if (serialized === null) {
        localStorage.removeItem(storageKey(slug));
      } else {
        localStorage.setItem(storageKey(slug), serialized);
      }
    } catch {
      // localStorage unavailable — `memory` above keeps the value readable
      // for the session.
    }
    listeners.forEach((listener) => listener());
  }

  function useValue(slug: string | undefined): T {
    return useSyncExternalStore(
      subscribe,
      () => read(slug),
      () => defaultValue,
    );
  }

  return { useValue, write };
}

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
  const store = createScopedStorageStore(
    prefix,
    false,
    (stored) => stored === "true",
    (value) => (value ? "true" : null),
  );
  return { useDismissed: store.useValue, write: store.write };
}
