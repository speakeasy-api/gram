import { googleAds } from "@/lib/google-ads";
import { useEffect, useRef } from "react";
import { useLocation, useNavigate } from "react-router";

/**
 * Query param the server sets on the post-login redirect when that login just
 * provisioned an organization for a brand-new user (see withSignedUpParam in
 * server/internal/auth/redirect.go). It is the only signal the dashboard has
 * that a signup, rather than an ordinary login, completed.
 */
export const SIGNED_UP_PARAM = "signed_up";

/** The Google Ads conversion event for a platform signup. */
export const SIGNUP_CONVERSION_EVENT = "platform_signup";

/**
 * Report a completed platform signup to Google Ads. `onComplete` runs once
 * the event is out the door (or immediately when tracking is inactive), for
 * callers that navigate away right after.
 */
export function reportSignupConversion(onComplete?: () => void): void {
  googleAds.trackConversion(
    SIGNUP_CONVERSION_EVENT,
    { product: "AI Control Plane" },
    onComplete,
  );
}

/**
 * Fire the signup conversion once when the page was landed on with the
 * server's signed-up mark, then drop the mark from the URL so a reload or a
 * shared link cannot count the same signup again.
 *
 * `sessionReady` gates the event on the session having resolved with an
 * active organization — the mark is only ever set by a login that created
 * one, so this rules out firing on a stale or forged URL.
 */
export function useSignupConversion(sessionReady: boolean): void {
  const location = useLocation();
  const navigate = useNavigate();

  // Latched on the first render: the mark is stripped from the URL below
  // before the session has resolved, so the fact that we landed with it has
  // to outlive the URL. null means not read yet.
  const pendingRef = useRef<boolean | null>(null);
  if (pendingRef.current === null) {
    pendingRef.current = new URLSearchParams(location.search).has(
      SIGNED_UP_PARAM,
    );
  }

  const hasMark = new URLSearchParams(location.search).has(SIGNED_UP_PARAM);
  useEffect(() => {
    if (!hasMark) {
      return;
    }
    const params = new URLSearchParams(location.search);
    params.delete(SIGNED_UP_PARAM);
    const search = params.toString();
    void navigate(
      {
        pathname: location.pathname,
        search: search ? `?${search}` : "",
        hash: location.hash,
      },
      { replace: true },
    );
  }, [hasMark, location.pathname, location.search, location.hash, navigate]);

  useEffect(() => {
    if (!pendingRef.current) {
      return;
    }
    // Start loading gtag.js while the session resolves so the event does not
    // wait on the script once it is ready to go.
    googleAds.initialize();
    if (!sessionReady) {
      return;
    }
    pendingRef.current = false;
    reportSignupConversion();
  }, [sessionReady]);
}
