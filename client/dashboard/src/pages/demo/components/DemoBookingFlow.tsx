import { useEffect } from "react";
import _Cal from "@calcom/embed-react";
import { useSessionData } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { CAL_DEMO_LINK, splitDisplayName } from "./demo-booking";

// Cal's .d.ts returns the legacy global `JSX.Element`, incompatible with
// react-jsx/TS5. Widen only the return type; keep prop types intact so a
// mistyped prop (e.g. calLink) is still caught.
type CalProps = Parameters<typeof _Cal>[0];
const Cal = _Cal as unknown as (props: CalProps) => React.ReactElement | null;

export function DemoBookingFlow(): JSX.Element {
  const { session } = useSessionData();
  const telemetry = useTelemetry();

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

  return (
    <div className="flex w-full flex-col gap-3">
      <div className="flex flex-col gap-1">
        <h2 className="text-lg font-semibold text-gray-900">
          Looks like your company is new to Speakeasy
        </h2>
        <p className="text-sm text-gray-600">
          Book time with our team to activate your account and get started.
        </p>
      </div>
      <div className="h-[70vh] min-h-[520px] w-full overflow-auto">
        <Cal
          calLink={CAL_DEMO_LINK}
          config={{
            layout: "month_view",
            theme: "light",
            hideEventTypeDetails: "true",
            name,
            email,
            // Prefill key must match the booking question's identifier on the
            // Cal event (see CAL_DEMO_LINK).
            company: companyName,
            cssVarsPerTheme: JSON.stringify({
              light: { "cal-bg": "transparent" },
              dark: { "cal-bg": "transparent" },
            }),
          }}
          style={{ width: "100%", height: "100%", overflow: "auto" }}
        />
      </div>
    </div>
  );
}
