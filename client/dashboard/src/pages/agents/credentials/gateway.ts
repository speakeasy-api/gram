/**
 * Whether an endpoint may carry a bearer key. HTTPS everywhere, except
 * loopback, where the request never reaches a network. An unparseable URL is
 * treated as unsafe rather than assumed fine.
 */
export function isCredentialSafe(raw: string): boolean {
  let parsed;
  try {
    parsed = new URL(raw);
  } catch {
    return false;
  }
  if (parsed.protocol === "https:") return true;
  return (
    parsed.protocol === "http:" &&
    ["localhost", "127.0.0.1", "[::1]", "::1"].includes(parsed.hostname)
  );
}
