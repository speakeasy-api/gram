/** Helpers for editing the Okta namespace of a registry record as text. */

export const OKTA_NAMESPACE = "com.speakeasy.ai/okta";

/**
 * Adds an OIN name to the record's Okta namespace and returns the new JSON
 * text, or null when the text is not a JSON object. Everything else in the
 * record is preserved as parsed; the caller formats the result.
 */
export function addOinName(text: string, name: string): string | null {
  let root: unknown;
  try {
    root = JSON.parse(text);
  } catch {
    return null;
  }
  if (typeof root !== "object" || root === null || Array.isArray(root)) {
    return null;
  }
  const record = root as Record<string, unknown>;
  const meta =
    typeof record._meta === "object" &&
    record._meta !== null &&
    !Array.isArray(record._meta)
      ? (record._meta as Record<string, unknown>)
      : {};
  const namespace =
    typeof meta[OKTA_NAMESPACE] === "object" &&
    meta[OKTA_NAMESPACE] !== null &&
    !Array.isArray(meta[OKTA_NAMESPACE])
      ? (meta[OKTA_NAMESPACE] as Record<string, unknown>)
      : {};
  const names = Array.isArray(namespace.oinNames)
    ? namespace.oinNames.filter((n): n is string => typeof n === "string")
    : [];
  if (!names.includes(name)) names.push(name);
  record._meta = {
    ...meta,
    [OKTA_NAMESPACE]: { ...namespace, oinNames: names },
  };
  return JSON.stringify(record);
}

/** Reads the OIN names currently in the editor text, if it parses. */
export function currentOinNames(text: string): string[] {
  try {
    const root = JSON.parse(text) as {
      _meta?: { [OKTA_NAMESPACE]?: { oinNames?: unknown } };
    };
    const names = root?._meta?.[OKTA_NAMESPACE]?.oinNames;
    return Array.isArray(names)
      ? names.filter((n): n is string => typeof n === "string")
      : [];
  } catch {
    return [];
  }
}
