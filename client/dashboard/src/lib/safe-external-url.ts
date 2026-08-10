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

  // A "noopener"/"noreferrer" feature string makes window.open return null
  // even on success, hiding popup-blocker failures. Instead, open a uniquely
  // named blank tab so null reliably means the popup was blocked, disown it,
  // then navigate it through an anchor that strips the Referer header. The
  // random name keeps opens from ever reusing an earlier external tab.
  const target = `gram-external-${crypto.randomUUID()}`;
  const opened = window.open("", target);
  if (!opened) return false;
  opened.opener = null;

  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.target = target;
  anchor.referrerPolicy = "no-referrer";
  anchor.click();
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
