import { useSessionData } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { Link } from "react-router";
import { useCaptureEnterpriseGateViewed } from "@/contexts/Telemetry";
import { AuthShell } from "@/pages/login/components/auth-shell";
import { DemoBookingFlow } from "@/pages/demo/components/DemoBookingFlow";

export default function BookDemo(): JSX.Element {
  const client = useSdkClient();
  const { session } = useSessionData();

  useCaptureEnterpriseGateViewed({
    email: session?.user.email ?? "",
    organizationId: session?.organization?.id ?? "",
    organizationName: session?.organization?.name ?? "",
    organizationSlug: session?.organization?.slug ?? "",
  });

  const handleLogout = async () => {
    await client.auth.logout();
    window.location.href = "/login";
  };

  return (
    <AuthShell
      page="Book a demo"
      contentClassName="max-w-[560px]"
      // The card carries its own prefill footnote instead ("2E Book a demo").
      showTerms={false}
      headerAction={
        <button
          type="button"
          onClick={() => void handleLogout()}
          className="auth-mono text-[13px] leading-none text-(--muted) transition-colors hover:text-black"
        >
          Log out
        </button>
      }
    >
      <DemoBookingFlow />
      <div className="mt-6 text-center">
        <Link
          to="/explore-demo"
          className="auth-mono text-xs text-(--muted) underline underline-offset-4 transition-colors hover:text-black"
        >
          Or explore a live demo org →
        </Link>
      </div>
    </AuthShell>
  );
}
