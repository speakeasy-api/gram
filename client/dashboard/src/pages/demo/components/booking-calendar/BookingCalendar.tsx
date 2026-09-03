import { useEffect, useId } from "react";
import _Cal, { getCalApi } from "@calcom/embed-react";
import { useSessionData } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { BOOKING_CALENDAR_LINK, splitDisplayName } from "./booking-calendar";

type CalProps = Parameters<typeof _Cal>[0];
const Cal = _Cal as unknown as (props: CalProps) => React.ReactElement | null;

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

function useCalBranding(namespace: string) {
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      const cal = await getCalApi({ namespace });
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
  }, [namespace]);
}

export type BookingFormDefaults = Record<string, string | undefined>;

export type BookingCalendarProps = {
  eventLabel?: string;
  formDefaults?: BookingFormDefaults;
};

export function BookingCalendar({
  eventLabel = "AI transformation — 30 min",
  formDefaults,
}: BookingCalendarProps): JSX.Element {
  const { session } = useSessionData();
  const telemetry = useTelemetry();
  const namespace = `booking-calendar-${useId().replaceAll(":", "")}`;

  useCalBranding(namespace);

  const email = session?.user.email ?? "";
  const { firstName, lastName } = splitDisplayName(session?.user.displayName);
  const name = [firstName, lastName].filter(Boolean).join(" ");
  const companyName = session?.organization?.name ?? "";

  useEffect(() => {
    const handler = (event: MessageEvent) => {
      try {
        const data =
          typeof event.data === "string"
            ? (JSON.parse(event.data) as Record<string, unknown>)
            : (event.data as Record<string, unknown>);
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
        // Ignore non-JSON postMessages from other senders.
      }
    };
    window.addEventListener("message", handler);
    return () => window.removeEventListener("message", handler);
  }, [firstName, lastName, email, telemetry]);

  const prefill = [email, companyName].filter(Boolean).join(" · ");

  return (
    <div className="bg-card">
      <div className="w-full overflow-hidden border-b border-edge">
        <div className="flex py-4 items-baseline gap-4 border-b border-edge px-6">
          <span className="auth-mono text-xs">{eventLabel}</span>
          <span className="auth-mono-text text-xs text-muted">Google Meet</span>
        </div>
        {prefill ? (
          <p className="px-6 py-3 text-center text-body-sm bg-background border-b border-edge">
            Details prefilled from your account: {prefill}
          </p>
        ) : null}
        <div className="h-[clamp(500px,54vh,640px)] w-full overflow-auto">
          <Cal
            calLink={BOOKING_CALENDAR_LINK}
            namespace={namespace}
            config={{
              ...Object.fromEntries(
                Object.entries(formDefaults ?? {}).filter(([, value]) => value),
              ),
              layout: "month_view",
              theme: "light",
              name,
              email,
              "Company-Name": companyName,
            }}
            style={{ width: "100%", height: "100%", overflow: "auto" }}
          />
        </div>
      </div>
    </div>
  );
}
