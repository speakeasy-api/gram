/**
 * Sign-up attribution.
 *
 * Paid traffic reaches the sign-up page with utm_* params (the marketing site
 * carries them over from the visitor's landing page). The account itself is
 * created server-side in the auth callback, after the identity-provider round
 * trip, so the server-side sign-up events never see the browser's URL or
 * analytics session. The sign-up page forwards both on the login request; the
 * server stashes them against the login nonce and attaches them to the
 * sign-up events.
 */

const UTM_KEYS = [
  "utm_source",
  "utm_medium",
  "utm_campaign",
  "utm_term",
  "utm_content",
] as const;

const STORAGE_KEY = "speakeasy-signup-utm";
// Ad platforms never send anything close to this; the server truncates too.
const MAX_VALUE_LENGTH = 200;

type UtmParams = Partial<Record<(typeof UTM_KEYS)[number], string>>;

function readUtmParams(search: string): UtmParams {
  const query = new URLSearchParams(search);
  const params: UtmParams = {};
  for (const key of UTM_KEYS) {
    const value = query.get(key)?.trim();
    if (value) params[key] = value.slice(0, MAX_VALUE_LENGTH);
  }
  return params;
}

/**
 * Remember the UTMs the dashboard was opened with, for the rest of the tab's
 * session. A visitor often lands on `/` or a deep link and is redirected to
 * `/sign-up` without the query string, so the sign-up page can't rely on its
 * own URL. Call once at boot, before any redirect.
 */
export function rememberLandingUtm(search?: string): void {
  // Runs at module load; tests and tooling may import without a browser.
  if (typeof window === "undefined") return;
  const params = readUtmParams(search ?? window.location.search);
  if (Object.keys(params).length === 0) return;
  try {
    // Last touch: a new campaign replaces the previous set entirely.
    window.sessionStorage.setItem(STORAGE_KEY, JSON.stringify(params));
  } catch {
    // Storage blocked; the sign-up page's own URL still works.
  }
}

function storedUtm(): UtmParams {
  try {
    const raw = window.sessionStorage.getItem(STORAGE_KEY);
    if (!raw) return {};
    // Re-validate on read: storage is user-writable.
    return readUtmParams(
      new URLSearchParams(JSON.parse(raw) as Record<string, string>).toString(),
    );
  } catch {
    return {};
  }
}

let sessionIdSource: (() => string | undefined) | null = null;

/** Registered by the telemetry provider once its analytics client is up. */
export function setAnalyticsSessionIdSource(
  source: (() => string | undefined) | null,
): void {
  sessionIdSource = source;
}

/**
 * Query params to add to the sign-up login request: the current URL's UTMs,
 * falling back to the ones remembered at boot, plus the analytics session id.
 */
export function signupAttributionParams(
  search = window.location.search,
): Record<string, string> {
  const current = readUtmParams(search);
  const utm = Object.keys(current).length > 0 ? current : storedUtm();
  const params: Record<string, string> = { ...utm };

  let sessionId: string | undefined;
  try {
    sessionId = sessionIdSource?.();
  } catch {
    sessionId = undefined;
  }
  if (sessionId) params.analytics_session_id = sessionId;
  return params;
}
