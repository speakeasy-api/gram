/**
 * API and upstream URLs cross a trust boundary, so parse without a base to
 * keep relative and protocol-relative inputs from becoming navigable.
 */
export function safeExternalHttpUrl(
  raw: string | null | undefined,
): string | null {
  if (!raw) return null;

  try {
    const url = new URL(raw);
    return url.protocol === "http:" || url.protocol === "https:"
      ? url.href
      : null;
  } catch {
    return null;
  }
}

/** Keep scheme validation and isolated-tab flags together at navigation sinks. */
export function openSafeExternalUrl(raw: string | null | undefined): boolean {
  const url = safeExternalHttpUrl(raw);
  if (!url) return false;

  // A "noopener" feature string makes window.open return null even on
  // success, hiding popup-blocker failures — sever the opener manually so a
  // null return reliably means the tab did not open.
  const opened = window.open(url, "_blank");
  if (!opened) return false;
  opened.opener = null;
  return true;
}

/**
 * Resolve a redirect produced by this SPA while preventing external or
 * executable schemes from escaping the dashboard origin.
 */
export function safeSameOriginUrl(
  raw: string | null | undefined,
): string | null {
  if (!raw) return null;

  try {
    const url = new URL(raw, window.location.origin);
    const isHttp = url.protocol === "http:" || url.protocol === "https:";
    return isHttp && url.origin === window.location.origin ? url.href : null;
  } catch {
    return null;
  }
}
