import { useSessionData } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { Link } from "react-router";
import { useCaptureTrialExpiredGateViewed } from "@/contexts/Telemetry";
import { AuthShell } from "@/pages/login/components/auth-shell";
import { DemoBookingFlow } from "@/pages/demo/components/DemoBookingFlow";
import { isValidDate } from "@/lib/trial-status";
import { format } from "date-fns";

const DATELESS_TITLE = "Your 14-day trial has ended.";

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
  const title = isValidDate(endsAt)
    ? `Your 14-day trial ended on ${format(endsAt, "MMMM d, yyyy")}.`
    : DATELESS_TITLE;

  return (
    <AuthShell
      page="Talk to us"
      contentClassName="max-w-[560px]"
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
        title={title}
        subtitle="Your projects, MCP servers, and toolsets are still here. Book time with our team to move to a paid plan."
      />
      <div className="mt-6 text-center">
        <Link
          to="/explore-demo"
          className="auth-mono text-[12px] text-[var(--muted)] underline underline-offset-4 transition-colors hover:text-black"
        >
          Or explore a live demo org →
        </Link>
      </div>
    </AuthShell>
  );
}
