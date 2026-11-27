import { waitFor } from "@testing-library/react";

/**
 * Hook state changes do not trigger waitFor's DOM MutationObserver. Poll cheap
 * state assertions promptly, retaining Testing Library's timeout and act handling.
 * Use ordinary waitFor for DOM queries; their retries can be expensive.
 */
export function waitForHookState(
  assertion: () => void,
  options?: { timeout?: number },
): Promise<void> {
  return waitFor(assertion, { ...options, interval: 1 });
}
