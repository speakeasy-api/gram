import { useEffect } from "react";
import _Cal, { getCalApi } from "@calcom/embed-react";
import { LockoutPaygCheckoutPanel } from "@/components/billing/lockout-payg-checkout-panel";
import { useSessionData } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { CAL_DEMO_LINK, splitDisplayName } from "./demo-booking";

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

// `hideEventTypeDetails` and `cssVarsPerTheme` belong to Cal's UiConfig, not to
// the `config` prop (PrefillAndIframeAttrsConfig) — passing them there only
// appends inert query params, which is why the embed kept drawing its own
// title/duration block. They have to go through the "ui" instruction.
// The auth surface is fixed light mode, so both themes get the same values: a
// dark-mode visitor would otherwise get a dark calendar inside a white card.
function useCalBranding() {
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      const cal = await getCalApi();
      if (cancelled) return;
      cal("ui", {
        theme: "light",
        hideEventTypeDetails: true,
        cssVarsPerTheme: { light: CAL_BRAND_VARS, dark: CAL_BRAND_VARS },
      });
    })();
    return () => {
      cancelled = true;
    };
  }, []);
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

  useCalBranding();

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
        <div className="h-[clamp(500px,54vh,600px)] w-full overflow-auto">
          <Cal
            calLink={CAL_DEMO_LINK}
            config={{
              // Caller defaults go first so the identity below wins: the type
              // is an open record and cannot stop a caller passing `email`,
              // but attendee details are this component's to set.
              // Empty entries are dropped rather than sent, so an unset
              // default leaves the field open instead of blanking it.
              ...Object.fromEntries(
                Object.entries(formDefaults ?? {}).filter(([, v]) => v),
              ),
              layout: "month_view",
              theme: "light",
              name,
              email,
              // Key must match the booking question's identifier on the Cal
              // event.
              "Company-Name": companyName,
            }}
            style={{ width: "100%", height: "100%", overflow: "auto" }}
          />
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
