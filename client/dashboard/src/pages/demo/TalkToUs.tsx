import { useSessionData } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useCaptureUpgradeGateViewed } from "@/contexts/Telemetry";
import { AuthShell } from "@/pages/login/components/auth-shell";
import { DemoBookingFlow } from "@/pages/demo/components/DemoBookingFlow";
import {
  getTrialLifecycleFromDates,
  type TrialLifecycle,
} from "@/lib/trial-status";
import { getGateCopy } from "@/pages/demo/upgrade-gate-copy";
import { Link, Navigate } from "react-router";

const SALES_EMAIL = "sales@speakeasy.com";

// Reached two ways: the gate redirects an organization here once its trial has
// ended and the sweep has demoted it, or a user on a running trial navigates
// here to upgrade early.
export default function TalkToUs(): JSX.Element {
  const { session } = useSessionData();
  const now = new Date();
  const lifecycle: TrialLifecycle = getTrialLifecycleFromDates(
    session?.trial,
    now,
  );

  // No trial on the session means a converted customer or an org that never
  // trialed. Neither has a trial to talk about, and the gate never sends them
  // here, so bounce rather than describe a trial they do not have.
  if (lifecycle === "none") {
    return <Navigate to="/" replace />;
  }

  return <UpgradeGate lifecycle={lifecycle} now={now} />;
}

function UpgradeGate({
  lifecycle,
  now,
}: {
  lifecycle: TrialLifecycle;
  now: Date;
}): JSX.Element {
  const client = useSdkClient();
  const { session } = useSessionData();

  useCaptureUpgradeGateViewed({
    email: session?.user.email ?? "",
    organizationId: session?.organization?.id ?? "",
    organizationName: session?.organization?.name ?? "",
    organizationSlug: session?.organization?.slug ?? "",
    trialLifecycle: lifecycle,
  });

  const handleLogout = async () => {
    await client.auth.logout();
    window.location.href = "/login";
  };

  const copy = getGateCopy(session?.trial, now);

  return (
    <AuthShell
      page="Talk to us"
      singleColumn
      // The card carries its own prefill footnote instead ("2E Book a demo").
      showTerms={false}
      headerAction={
        // A walled org has nothing to go back to, so logging out is the only
        // exit. Anyone else still has a dashboard and arrived here by choice.
        lifecycle === "expired" ? (
          <button
            type="button"
            onClick={() => void handleLogout()}
            className="auth-mono text-[13px] leading-none text-[var(--muted)] transition-colors hover:text-black"
          >
            Log out
          </button>
        ) : (
          <Link
            to="/"
            className="auth-mono text-[13px] leading-none text-[var(--muted)] transition-colors hover:text-black"
          >
            Back to dashboard
          </Link>
        )
      }
    >
      <DemoBookingFlow
        eventLabel="Upgrade Trial — 30 min"
        intro={
          <div className="grid w-full grid-cols-1 items-start gap-4 md:grid-cols-[2fr_2.25fr]">
            <div className="flex flex-col gap-2">
              <span className="auth-mono flex items-center gap-2.5 text-[12px] text-[var(--muted)]">
                <i
                  aria-hidden="true"
                  className={`size-[7px] rounded-full ${copy.dotClassName}`}
                />
                {copy.status}
              </span>
              <h1 className="text-[40px] leading-[1.05] font-thin tracking-tight [font-family:var(--f-display)]">
                Book a call to upgrade.
              </h1>
            </div>
            <div className="flex flex-col gap-2 md:border-l md:border-[var(--edge-soft)] md:pl-6">
              <p className="text-[14px] text-[var(--muted-strong)]">
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
