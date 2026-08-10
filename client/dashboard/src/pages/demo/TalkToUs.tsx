import { useSessionData } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useCaptureTrialExpiredGateViewed } from "@/contexts/Telemetry";
import { AuthShell } from "@/pages/login/components/auth-shell";
import { DemoBookingFlow } from "@/pages/demo/components/DemoBookingFlow";
import {
  getTrialLifecycleFromDates,
  getTrialStatusFromDates,
  isValidDate,
} from "@/lib/trial-status";
import { format } from "date-fns";

const SALES_EMAIL = "sales@speakeasy.com";

const SHARED_BODY =
  "Book 30 minutes and we'll find the plan that fits your organization.";

type GateCopy = {
  /** Colour of the status dot. Static — the pulse is for live sessions. */
  dotClassName: string;
  status: string;
  body: string;
  detail: string;
};

// The page serves two audiences: an org walled after its trial ran out, and an
// org still inside its trial that wants to upgrade early. Only the header copy
// differs; the booking card below is the same either way. A trial that has
// ended is the exception, so the running trial is the default — a session with
// no readable trial gets that wording rather than a third variant.
function getGateCopy(
  trial: { startedAt: Date; endsAt: Date } | null | undefined,
  now: Date,
): GateCopy {
  const endsAt = trial?.endsAt;

  if (getTrialLifecycleFromDates(trial, now) === "expired") {
    return {
      dotClassName: "bg-[var(--vermilion)]",
      status: isValidDate(endsAt)
        ? `Trial ended ${format(endsAt, "MMM do, yyyy")}`
        : "Trial ended",
      body: `Trials run 14 days. ${SHARED_BODY}`,
      detail:
        "Your MCP servers, observability data, and policies are still here when you upgrade.",
    };
  }

  const remainingDays = getTrialStatusFromDates(trial, now)?.remainingDays;
  return {
    dotClassName: "bg-[var(--moss)]",
    status:
      remainingDays === undefined
        ? "Trial in progress"
        : `Trial · ${remainingDays} day${remainingDays === 1 ? "" : "s"} left`,
    body: `Upgrade before your trial ends and nothing pauses. ${SHARED_BODY}`,
    detail:
      "Your MCP servers, observability data, and policies carry over unchanged.",
  };
}

// Reached two ways: the gate redirects an organization here once its trial has
// ended and the sweep has demoted it, or a user on a running trial navigates
// here to upgrade early.
export default function TalkToUs(): JSX.Element {
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

  const copy = getGateCopy(session?.trial, new Date());

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
                <i
                  aria-hidden="true"
                  className={`size-[7px] rounded-full ${copy.dotClassName}`}
                />
                {copy.status}
              </span>
              <h1 className="text-[40px] leading-[1.05] font-thin tracking-[-0.035em] [font-family:var(--f-display)]">
                Book a call to upgrade.
              </h1>
            </div>
            <div className="flex flex-col gap-2 md:border-l md:border-[var(--edge-soft)] md:pl-10">
              <p className="text-[14px] tracking-[0.0025em] text-[var(--muted-strong)]">
                {copy.body}
              </p>
              <p className="auth-mono-text text-[12px] text-[var(--muted)]">
                {copy.detail}
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
