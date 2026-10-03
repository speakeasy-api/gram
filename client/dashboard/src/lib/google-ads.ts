/**
 * Google Ads conversion tracking through the Google tag (gtag.js).
 *
 * Mirrors the marketing site's loader: `window.gtag` is stubbed as a
 * `dataLayer` pusher before the script is injected, so events queued while
 * gtag.js is still loading are replayed once it lands. Only conversion events
 * are sent from here — pageviews and product analytics stay in PostHog.
 *
 * The tag id is baked in at build time from GRAM_GOOGLE_TAG_ID (see
 * vite.config.ts). It is a public identifier, the same kind the marketing site
 * ships in its bundle, but it is env-driven so local and preview builds never
 * load the tag. On top of that, one image serves every environment, so the tag
 * is also gated on the production hostname: an empty id or any other host
 * turns every call here into a no-op.
 */

declare global {
  interface Window {
    dataLayer?: unknown[];
    gtag?: (...args: unknown[]) => void;
  }
}

const GTAG_SCRIPT_SRC = "https://www.googletagmanager.com/gtag/js";
const PRODUCTION_HOSTNAME = "app.getgram.ai";

// How long a conversion that must finish before a navigation may hold the
// navigation back. gtag reports events with `event_callback`, but only once
// gtag.js itself has loaded; a blocked or slow script must not strand the user.
const EVENT_TIMEOUT_MS = 1000;

export type GoogleAdsConfig = {
  /** The Google tag id (`AW-…` or `G-…`). Empty disables tracking. */
  tagId: string;
  /** Whether this page load may talk to Google at all. */
  enabled: () => boolean;
};

export type GoogleAds = {
  /**
   * Inject gtag.js. Idempotent — subsequent calls are no-ops so a second
   * script tag is never added (React StrictMode, remounts). Returns whether
   * tracking is active on this page load.
   */
  initialize: () => boolean;
  /**
   * Send a conversion event. `onComplete` runs once the event has been
   * reported — or right away when tracking is inactive, or after a bounded
   * wait if gtag never answers — so callers can navigate away without
   * dropping the hit.
   */
  trackConversion: (
    eventName: string,
    params?: Record<string, unknown>,
    onComplete?: () => void,
  ) => void;
};

export const isProductionDashboard = (): boolean =>
  window.location.hostname === PRODUCTION_HOSTNAME;

export function createGoogleAds(config: GoogleAdsConfig): GoogleAds {
  let initialized = false;

  const isActive = (): boolean => config.tagId !== "" && config.enabled();

  const initialize = (): boolean => {
    if (!isActive()) {
      return false;
    }
    if (initialized) {
      return true;
    }
    initialized = true;

    window.dataLayer = window.dataLayer ?? [];
    // gtag.js reads `arguments`, not a rest array, so the stub must forward
    // the arguments object itself. This is the shape of the vendor snippet.
    window.gtag =
      window.gtag ??
      function gtag() {
        window.dataLayer?.push(arguments);
      };
    window.gtag("js", new Date());
    window.gtag("config", config.tagId);

    const script = document.createElement("script");
    script.src = `${GTAG_SCRIPT_SRC}?id=${encodeURIComponent(config.tagId)}`;
    script.async = true;
    document.head.appendChild(script);

    return true;
  };

  const trackConversion: GoogleAds["trackConversion"] = (
    eventName,
    params,
    onComplete,
  ) => {
    if (!initialize()) {
      onComplete?.();
      return;
    }

    let completed = false;
    const complete = (): void => {
      if (completed) {
        return;
      }
      completed = true;
      onComplete?.();
    };

    const eventParams: Record<string, unknown> = { ...params };
    if (onComplete) {
      eventParams["event_callback"] = complete;
      eventParams["event_timeout"] = EVENT_TIMEOUT_MS;
      window.setTimeout(complete, EVENT_TIMEOUT_MS);
    }

    window.gtag?.("event", eventName, eventParams);
  };

  return { initialize, trackConversion };
}

export const googleAds: GoogleAds = createGoogleAds({
  tagId: __GRAM_GOOGLE_TAG_ID__,
  enabled: isProductionDashboard,
});
