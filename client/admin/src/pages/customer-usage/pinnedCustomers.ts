import { useState } from "react";

// Pins are a per-viewer convenience kept in this browser only: they do not
// follow the operator to another machine and are not shared with teammates.
// The key carries the signed-in operator, so a second operator signing in on
// the same browser does not inherit the first one's pins. Storage can be
// unavailable (private windows, blocked site data), so every read and write is
// guarded and the page works without it.
const STORAGE_PREFIX = "gram-admin:customer-usage:pinned";

export function pinnedCustomersStorageKey(viewer: string): string {
  return `${STORAGE_PREFIX}:${viewer.toLowerCase()}`;
}

export function readPinnedCustomers(viewer: string | undefined): string[] {
  if (!viewer) return [];
  try {
    const raw = localStorage.getItem(pinnedCustomersStorageKey(viewer));
    const parsed: unknown = raw ? JSON.parse(raw) : [];
    return Array.isArray(parsed)
      ? parsed.filter((id): id is string => typeof id === "string")
      : [];
  } catch {
    return [];
  }
}

function writePinnedCustomers(
  viewer: string | undefined,
  ids: ReadonlySet<string>,
): void {
  if (!viewer) return;
  try {
    localStorage.setItem(
      pinnedCustomersStorageKey(viewer),
      JSON.stringify([...ids]),
    );
  } catch {
    // The pin still applies for this visit; it just is not remembered.
  }
}

type PinState = {
  viewer: string | undefined;
  pinned: ReadonlySet<string>;
};

// `viewer` identifies the signed-in operator, such as their email. Without one,
// pins work for the visit and are not remembered.
export function usePinnedCustomers(viewer: string | undefined): {
  pinned: ReadonlySet<string>;
  togglePinned: (organizationID: string) => void;
} {
  const [state, setState] = useState<PinState>(() => ({
    viewer,
    pinned: new Set(readPinnedCustomers(viewer)),
  }));
  // A different operator gets their own pins, read fresh.
  if (state.viewer !== viewer) {
    setState({ viewer, pinned: new Set(readPinnedCustomers(viewer)) });
  }

  // Persist in the click handler rather than in a state updater, which React
  // may call more than once.
  const togglePinned = (organizationID: string): void => {
    const pinned = new Set(state.pinned);
    if (pinned.has(organizationID)) pinned.delete(organizationID);
    else pinned.add(organizationID);
    writePinnedCustomers(state.viewer, pinned);
    setState({ viewer: state.viewer, pinned });
  };

  return { pinned: state.pinned, togglePinned };
}
