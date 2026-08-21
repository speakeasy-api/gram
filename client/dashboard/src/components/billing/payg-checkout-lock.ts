import { useSyncExternalStore } from "react";

/**
 * Single-flight lock for the pay-as-you-go checkout, scoped to an
 * organization and shared by every mount of the CTA.
 *
 * The CTA renders in more than one place at once (the billing page and the
 * sidebar trial card), and each mount owns a separate mutation, so per-instance
 * state cannot stop a click on one from opening a second Stripe session while
 * the other's request is still in flight. `isLocked` is readable synchronously
 * inside the click handler — before React can re-render anything — and the
 * subscription lets every mount disable together.
 */
const lockedOrganizations = new Set<string>();
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function isPaygCheckoutLocked(organizationId: string): boolean {
  return lockedOrganizations.has(organizationId);
}

export function setPaygCheckoutLocked(
  organizationId: string,
  locked: boolean,
): void {
  if (locked) {
    lockedOrganizations.add(organizationId);
  } else {
    lockedOrganizations.delete(organizationId);
  }
  listeners.forEach((listener) => listener());
}

/** Releases every lock. The store outlives unmount, so tests have to reset it. */
export function resetPaygCheckoutLocks(): void {
  lockedOrganizations.clear();
  listeners.forEach((listener) => listener());
}

export function usePaygCheckoutLocked(organizationId: string): boolean {
  return useSyncExternalStore(
    subscribe,
    () => isPaygCheckoutLocked(organizationId),
    () => false,
  );
}
