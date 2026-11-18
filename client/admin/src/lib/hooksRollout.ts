// Shared by the Hooks rollout page and the organization Features tab, which
// both edit pins.

/**
 * A pin must be a whole number from 1 up to the version the server publishes:
 * the server refuses anything higher, because such a pin would clear future
 * hooks releases before anyone decided to roll them out.
 */
export function validHooksRolloutVersion(
  value: string,
  currentVersion: number,
): number | undefined {
  if (value.trim() === "") return undefined;
  const version = Number(value);
  return Number.isInteger(version) && version >= 1 && version <= currentVersion
    ? version
    : undefined;
}

export const HOOKS_ROLLOUT_PROPAGATION_NOTE =
  "Pin changes reach customers on the next hourly plugin rollout sweep. Lowering a pin holds back later versions; it never downgrades a hooks plugin that is already published.";
