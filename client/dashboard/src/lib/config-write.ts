/**
 * Marks a hand-written mutation as a configuration write: sign-in setup,
 * credentials, members or their servers. SDK mutations are recognised by their
 * key; a mutation that wraps several SDK calls has none, so it carries this
 * instead, and gateway member states read again after it succeeds.
 */
export const CONFIG_WRITE_META = { configWrite: true } as const;

export function isConfigWriteMeta(meta: unknown): boolean {
  return (
    typeof meta === "object" &&
    meta !== null &&
    (meta as { configWrite?: unknown }).configWrite === true
  );
}
