import { useEffect, useState } from "react";
import _Cal, { getCalApi, type EmbedEvent } from "@calcom/embed-react";
import { LockoutPaygCheckoutPanel } from "@/components/billing/lockout-payg-checkout-panel";
import { useSessionData } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import {
  CAL_DEMO_LINK,
  CAL_DEMO_NAMESPACE,
  CAL_DEMO_URL,
  CAL_EMBED_TIMEOUT_MS,
  SALES_EMAIL,
  splitDisplayName,
} from "./demo-booking";

// Cal's .d.ts returns the legacy global `JSX.Element`, incompatible with
// react-jsx/TS5. Widen only the return type; keep prop types intact so a
// mistyped prop (e.g. calLink) is still caught.
type CalProps = Parameters<typeof _Cal>[0];
const Cal = _Cal as unknown as (props: CalProps) => React.ReactElement | null;

// The embed renders in an iframe, so the `.auth-brand` custom properties can't
// cascade into it — Cal's own theme variables have to be handed the same
// literal values ("2E Book a demo" frame in the design project). `cal-brand`
// drives the selected day/time pills, which the frame draws as the dark CTA
// swatch on white.
const CAL_BRAND_VARS = {
  "cal-bg": "transparent",
  "cal-bg-subtle": "hsl(0, 0%, 97%)",
  "cal-bg-emphasis": "hsl(0, 0%, 92%)",
  "cal-bg-muted": "hsl(0, 0%, 98%)",
  "cal-border": "hsl(0, 0%, 86%)",
  "cal-border-subtle": "hsl(0, 0%, 92%)",
  "cal-border-emphasis": "hsl(0, 0%, 60%)",
  "cal-text": "#000",
  "cal-text-emphasis": "#000",
  "cal-text-subtle": "hsl(0, 0%, 33%)",
  "cal-text-muted": "hsl(0, 0%, 46%)",
  "cal-brand": "hsl(0, 0%, 20%)",
  "cal-brand-emphasis": "hsl(0, 0%, 14%)",
  "cal-brand-text": "#fff",
};

type CalApi = Awaited<ReturnType<typeof getCalApi>>;

// Why the calendar never appeared. `code` is Cal's own failure code, present
// only on `linkFailed`.
type EmbedFailure = {
  reason: "timeout" | "link_failed" | "api_error";
  code?: string;
};

// `hideEventTypeDetails` and `cssVarsPerTheme` belong to Cal's UiConfig, not to
// the `config` prop (PrefillAndIframeAttrsConfig) — passing them there only
// appends inert query params, which is why the embed kept drawing its own
// title/duration block. They have to go through the "ui" instruction.
// The auth surface is fixed light mode, so both themes get the same values: a
// dark-mode visitor would otherwise get a dark calendar inside a white card.
//
// The instruction resolves to Cal's `doInIframe`, which throws when the
// instance it lands on has no live iframe, so it is sent twice and never
// unguarded: once as soon as the API is up (Cal replays a queued `ui` against
// the iframe once one exists) and again on `linkReady`, when the iframe is
// definitely there. Applying it early is what avoids a flash of Cal's own
// styling; applying it again is what makes the styling arrive at all if the
// early attempt landed on a stale instance.
function applyBranding(cal: CalApi) {
  try {
    cal("ui", {
      theme: "light",
      hideEventTypeDetails: true,
      cssVarsPerTheme: { light: CAL_BRAND_VARS, dark: CAL_BRAND_VARS },
    });
  } catch {
    // An unbranded calendar is a cosmetic problem. Letting the throw escape as
    // an unhandled error, which is what used to happen, is not.
  }
}

/**
 * Tracks whether Cal's iframe actually paints.
 *
 * Cal keeps the iframe hidden until it emits `linkReady`, so that event — or
 * `linkFailed`, or neither before the timeout — is the whole story: the visitor
 * either has a calendar or is looking at an empty box.
 */
function useCalEmbedStatus(): { ready: boolean; failure: EmbedFailure | null } {
  const [ready, setReady] = useState(false);
  const [failure, setFailure] = useState<EmbedFailure | null>(null);

  useEffect(() => {
    // First outcome wins; cleanup closes it so a late callback can neither
    // report twice nor touch an unmounted tree.
    let settled = false;
    let cal: CalApi | undefined;

    const settle = (next: EmbedFailure | null) => {
      if (settled) return;
      settled = true;
      if (next) setFailure(next);
      else setReady(true);
    };

    const onLinkReady = () => {
      if (cal) applyBranding(cal);
      settle(null);
    };
    const onLinkFailed = (event: EmbedEvent<"linkFailed">) => {
      settle({ reason: "link_failed", code: event.detail.data.code });
    };

    const timer = window.setTimeout(
      () => settle({ reason: "timeout" }),
      CAL_EMBED_TIMEOUT_MS,
    );

    void (async () => {
      try {
        cal = await getCalApi({ namespace: CAL_DEMO_NAMESPACE });
        if (settled) return;
        cal("on", { action: "linkReady", callback: onLinkReady });
        cal("on", { action: "linkFailed", callback: onLinkFailed });
        applyBranding(cal);
      } catch {
        settle({ reason: "api_error" });
      }
    })();

    return () => {
      settled = true;
      window.clearTimeout(timer);
      cal?.("off", { action: "linkReady", callback: onLinkReady });
      cal?.("off", { action: "linkFailed", callback: onLinkFailed });
    };
  }, []);

  return { ready, failure };
}

// Covers the window before `linkReady`, which is Cal's own signal that the
// iframe is worth showing. Without it the visitor watches the embed's spinner
// appear and then vanish behind a hidden iframe.
function CalendarLoading(): JSX.Element {
  return (
    <div className="absolute inset-0 flex items-center justify-center bg-(--card)">
      <span className="auth-mono animate-pulse text-xs text-(--muted) motion-reduce:animate-none">
        Loading calendar
      </span>
    </div>
  );
}

// Both routes out of here are ordinary links, so neither depends on the embed
// that just failed.
function CalendarFallback(): JSX.Element {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-2.5 px-6 text-center">
      <p className="text-[16px] tracking-[0.0025em]">
        The booking calendar could not load.
      </p>
      <p className="max-w-[32rem] text-sm tracking-[0.0025em] text-(--muted-strong)">
        Open the booking page in a new tab, or email the team and we will find a
        time.
      </p>
      <div className="mt-1 flex flex-wrap items-center justify-center gap-x-6 gap-y-2">
        <a
          href={CAL_DEMO_URL}
          target="_blank"
          rel="noopener noreferrer"
          className="auth-mono text-xs text-(--link) underline underline-offset-4"
        >
          Book a demo
        </a>
        <a
          href={`mailto:${SALES_EMAIL}`}
          className="auth-mono text-xs text-(--link) underline underline-offset-4"
        >
          Email {SALES_EMAIL}
        </a>
      </div>
    </div>
  );
}

// Sits above the card on the cold-signup gate. The expired-trial gate passes
// its own header instead.
const DEFAULT_INTRO = (
  <div className="text-center mb-4">
    <p className="text-[16px] tracking-[0.0025em]">
      Looks like your company is new to Speakeasy.
    </p>
    <p className="mt-1.5 text-sm tracking-[0.0025em] text-(--muted-strong)">
      Book time with our team to activate your account and get started.
    </p>
  </div>
);

/**
 * Values to prefill on the booking form, keyed by the question's identifier on
 * the Cal event. The form carries more questions than any one caller fills:
 * `source` ("How'd you hear about us?"), `notes` ("Additional notes"), `title`,
 * `attendeePhoneNumber`. Name, email and `Company-Name` come from the session
 * and are always sent. Anything omitted is left for the user to answer.
 */
export type BookingFormDefaults = Record<string, string | undefined>;

export function DemoBookingFlow({
  intro = DEFAULT_INTRO,
  eventLabel = "AI transformation — 30 min",
  formDefaults,
}: {
  /** Rendered above the booking card. Pass `null` to omit it entirely. */
  intro?: React.ReactNode;
  /** Names the meeting in the card header, which the embed itself hides. */
  eventLabel?: string;
  formDefaults?: BookingFormDefaults;
} = {}): JSX.Element {
  const { session } = useSessionData();
  const telemetry = useTelemetry();

  const { ready, failure } = useCalEmbedStatus();

  // Until 2026-08 the only thing this flow reported was a successful booking,
  // so an embed that never rendered showed up as silence rather than as a
  // failure. Both outcomes are captured now: the pair is what makes a failure
  // rate — and an alert on it — possible.
  useEffect(() => {
    if (!failure) return;
    telemetry.capture("demo_booking_embed_failed", {
      reason: failure.reason,
      code: failure.code,
      cal_link: CAL_DEMO_LINK,
      timeout_ms: CAL_EMBED_TIMEOUT_MS,
    });
  }, [failure, telemetry]);

  useEffect(() => {
    if (!ready) return;
    telemetry.capture("demo_booking_embed_loaded", { cal_link: CAL_DEMO_LINK });
  }, [ready, telemetry]);

  const email = session?.user.email ?? "";
  const { firstName, lastName } = splitDisplayName(session?.user.displayName);
  const name = [firstName, lastName].filter(Boolean).join(" ");
  // The org is created from the company name entered during sign-up, so it
  // doubles as the answer to the Cal form's company question.
  const companyName = session?.organization?.name ?? "";

  useEffect(() => {
    const handler = (e: MessageEvent) => {
      try {
        const data =
          typeof e.data === "string"
            ? (JSON.parse(e.data) as Record<string, unknown>)
            : (e.data as Record<string, unknown>);
        if (
          data?.originator === "CAL" &&
          (data?.fullType as string)?.endsWith("bookingSuccessful")
        ) {
          telemetry.capture("booked_demo", {
            first_name: firstName,
            last_name: lastName,
            email,
          });
        }
      } catch {
        // ignore non-JSON postMessages from other senders
      }
    };
    window.addEventListener("message", handler);
    return () => window.removeEventListener("message", handler);
  }, [firstName, lastName, email, telemetry]);

  // The frame shows both, but a session can be missing either.
  const prefill = [email, companyName].filter(Boolean).join(" · ");

  return (
    <div className="flex w-full flex-col items-center gap-2">
      {intro}

      {/* Both gates render through this flow, so the checkout offer lives here
          rather than in each page — the calendar below stays the fallback for
          anyone it doesn't apply to. */}
      <LockoutPaygCheckoutPanel />

      <div className="w-full overflow-hidden border border-(--edge) bg-(--card)">
        {/* The embed runs with `hideEventTypeDetails`, so this header is what
            names the meeting — as in the design frame. */}
        <div className="flex h-11 items-center justify-between border-b border-(--edge-soft) px-[18px]">
          <span className="auth-mono text-xs">{eventLabel}</span>
          <span className="auth-mono-text text-xs text-(--muted)">
            Google Meet
          </span>
        </div>

        {/* Tall enough for a six-row month without clipping the last week,
            capped so the card still clears the fold on a laptop viewport. */}
        <div className="relative h-[clamp(500px,54vh,600px)] w-full overflow-auto">
          {failure ? (
            <CalendarFallback />
          ) : (
            <>
              <Cal
                namespace={CAL_DEMO_NAMESPACE}
                calLink={CAL_DEMO_LINK}
                config={{
                  // Caller defaults go first so the identity below wins: the
                  // type is an open record and cannot stop a caller passing
                  // `email`, but attendee details are this component's to set.
                  // Empty entries are dropped rather than sent, so an unset
                  // default leaves the field open instead of blanking it.
                  ...Object.fromEntries(
                    Object.entries(formDefaults ?? {}).filter(([, v]) => v),
                  ),
                  layout: "month_view",
                  theme: "light",
                  name,
                  email,
                  // Key must match the booking question's identifier on the
                  // Cal event.
                  "Company-Name": companyName,
                }}
                style={{ width: "100%", height: "100%", overflow: "auto" }}
              />
              {!ready && <CalendarLoading />}
            </>
          )}
        </div>
      </div>

      {prefill && (
        <p className="auth-mono-text text-center text-[11px] tracking-[0.02em] text-(--muted)">
          Details prefilled from your account: {prefill}
        </p>
      )}
    </div>
  );
}
