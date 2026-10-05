import type { ComputedValueKey } from "./definition";

export interface SetupValues {
  /** Absent while there is nothing to show. */
  values?: Record<ComputedValueKey, string>;
  /** Why the values cannot be shown, or null when they are. */
  unavailableReason: string | null;
}

/**
 * The values a platform is pointed at: Gram's token endpoint, its issuer and
 * the API host the returned token is sent to.
 *
 * These come from the server's own derivation, the one its authorization
 * server metadata publishes, and are never assembled here: a second derivation
 * would drift from what a client discovers and show values that fail. Until the
 * server exposes that read (AIM-370), there is nothing to show.
 */
export function useSetupValues(): SetupValues {
  return {
    unavailableReason:
      "Gram can't show these values here yet. Ask your Gram contact for them.",
  };
}
