import { useSessionData } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useCaptureTrialExpiredGateViewed } from "@/contexts/Telemetry";
import { AuthShell } from "@/pages/login/components/auth-shell";
import { DemoBookingFlow } from "@/pages/demo/components/DemoBookingFlow";
import { isValidDate } from "@/lib/trial-status";
import { format } from "date-fns";

const SALES_EMAIL = "sales@speakeasy.com";

// The gate an organization lands on once its enterprise trial has ended and the
// sweep has demoted it.
export default function TrialEnded(): JSX.Element {
  const client = useSdkClient();
  const { session } = useSessionData();

  useCaptureTrialExpiredGateViewed({
    email: session?.user.email ?? "",
    organizationId: session?.organization?.id ?? "",
    organizationName: session?.organization?.name ?? "",
    organizationSlug: session?.organization?.slug ?? "",
  });

  const handleLogout = async () => {
    await client.auth.logout();
    window.location.href = "/login";
  };

  // A session can reach this page without a parseable end date; naming the day
  // is a nicety, so drop it rather than rendering "Invalid Date".
  const endsAt = session?.trial?.endsAt;
  const statusLabel = isValidDate(endsAt)
    ? `Trial ended ${format(endsAt, "MMM do")}`
    : "Trial ended";

  return (
    <AuthShell
      page="Talk to us"
      singleColumn
      // The card carries its own prefill footnote instead ("2E Book a demo").
      showTerms={false}
      headerAction={
        <button
          type="button"
          onClick={() => void handleLogout()}
          className="auth-mono text-[12px] text-[var(--muted)] transition-colors hover:text-black"
        >
          Log out
        </button>
      }
    >
      <DemoBookingFlow
        eventLabel="Upgrade Trial — 30 min"
        intro={
          <div className="grid w-full grid-cols-1 items-start gap-10 md:grid-cols-2">
            <div className="flex flex-col gap-2.5">
              <span className="auth-mono flex items-center gap-2.5 text-[12px] text-[var(--muted)]">
                {/* Static, unlike the showcase's live-session dot: the pulse
                    reads as "happening now", which is the opposite of this. */}
                <i
                  aria-hidden="true"
                  className="size-[7px] rounded-full bg-[var(--vermilion)]"
                />
                {statusLabel}
              </span>
              <p className="text-[40px] leading-[1.05] font-thin tracking-[-0.035em] [font-family:var(--f-display)]">
                Book a call to upgrade.
              </p>
            </div>
            <div className="flex flex-col gap-2 md:border-l md:border-[var(--edge-soft)] md:pl-10">
              <p className="text-[14px] tracking-[0.0025em] text-[var(--muted-strong)]">
                Trials run 14 days. Book 30 minutes and we&rsquo;ll find the
                plan that fits your organization.
              </p>
              <p className="auth-mono-text text-[12px] text-[var(--muted)]">
                MCP servers, observability data, and policies retained for 30
                days.
              </p>
            </div>
          </div>
        }
      />
      <div className="text-center">
        <a
          href={`mailto:${SALES_EMAIL}`}
          className="auth-mono text-[12px] text-[var(--muted)] underline underline-offset-4 transition-colors hover:text-black"
        >
          Or email {SALES_EMAIL} →
        </a>
      </div>
    </AuthShell>
  );
}
