/**
 * A value the server holds but will not read back.
 *
 * Both surfaces that edit upstream credentials solve this today, differently:
 * the header list uses a "***" sentinel plus a hadSecret flag plus an
 * isUnchangedSecret() predicate, and the agent credential form lets a redacted
 * value seed an empty field and hopes nobody notices. One union replaces both.
 *
 * The distinction that matters on the wire is between "leave it alone" and
 * "replace it", which is exactly what the two members say.
 */
export type Secret =
  | { readonly kind: "unchanged" }
  | { readonly kind: "set"; readonly value: string };

/** What the API returns in place of a secret it will not disclose. */
export const REDACTED_SECRET = "***";

export const UNCHANGED_SECRET: Secret = { kind: "unchanged" };

/**
 * Read a secret off a server record. A redacted value means the server is
 * holding one it will not show us, which is "unchanged", not an empty string.
 */
export function secretFromServerValue(
  value: string | null | undefined,
  isSecret: boolean,
): Secret {
  if (isSecret && (value === REDACTED_SECRET || !value))
    return UNCHANGED_SECRET;
  return { kind: "set", value: value ?? "" };
}

/**
 * The value to send, or undefined to omit the field entirely. Omitting is what
 * tells the API to keep what it already has — sending the sentinel back would
 * store the literal "***".
 */
export function secretToWireValue(secret: Secret): string | undefined {
  return secret.kind === "set" ? secret.value : undefined;
}

/** Whether a secret carries something the operator actually entered. */
export function secretHasValue(secret: Secret): boolean {
  return secret.kind === "set" && secret.value.trim() !== "";
}

export function secretsEqual(a: Secret, b: Secret): boolean {
  if (a.kind !== b.kind) return false;
  return a.kind !== "set" || b.kind !== "set" || a.value === b.value;
}
