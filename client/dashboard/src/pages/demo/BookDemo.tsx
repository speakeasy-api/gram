import { Button } from "@/components/ui/Button";
import { useSessionData } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
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
      contentClassName="max-w-[50rem] gap-6"
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
      <Button
        variant="primary"
        size="md"
        icon="arrow-right"
        iconAfter
        href="/explore-demo"
      >
        Explore a Live Demo
      </Button>
    </AuthShell>
  );
}
