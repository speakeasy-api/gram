import { useCallback, useState } from "react";

// Pins are a per-viewer convenience kept in this browser only: they do not
// follow the operator to another machine and are not shared with teammates.
// Storage can be unavailable (private windows, blocked site data), so every
// read and write is guarded and the page works without it.
const STORAGE_KEY = "gram-admin:customer-usage:pinned";

export function readPinnedCustomers(): string[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    const parsed: unknown = raw ? JSON.parse(raw) : [];
    return Array.isArray(parsed)
      ? parsed.filter((id): id is string => typeof id === "string")
      : [];
  } catch {
    return [];
  }
}

function writePinnedCustomers(ids: ReadonlySet<string>): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify([...ids]));
  } catch {
    // The pin still applies for this visit; it just is not remembered.
  }
}

export function usePinnedCustomers(): {
  pinned: ReadonlySet<string>;
  togglePinned: (organizationID: string) => void;
} {
  const [pinned, setPinned] = useState<ReadonlySet<string>>(
    () => new Set(readPinnedCustomers()),
  );
  const togglePinned = useCallback((organizationID: string) => {
    setPinned((current) => {
      const next = new Set(current);
      if (next.has(organizationID)) next.delete(organizationID);
      else next.add(organizationID);
      writePinnedCustomers(next);
      return next;
    });
  }, []);
  return { pinned, togglePinned };
}
