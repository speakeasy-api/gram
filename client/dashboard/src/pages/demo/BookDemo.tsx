import { useSessionData } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useCaptureEnterpriseGateViewed } from "@/contexts/Telemetry";
import { AuthLayout } from "@/pages/login/components/login-section";
import { JourneyDemo } from "@/pages/login/components/journey-demo";
import { DemoBookingFlow } from "@/pages/demo/components/DemoBookingFlow";
import { LogOutIcon } from "lucide-react";

export default function BookDemo() {
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
    <main className="flex min-h-screen flex-col md:flex-row">
      <JourneyDemo />

      <AuthLayout
        contentClassName="max-w-2xl"
        topRight={
          <button
            onClick={handleLogout}
            className="flex items-center gap-1.5 text-xs text-[#8B8684] transition-colors hover:text-slate-600"
          >
            <LogOutIcon className="h-3.5 w-3.5" />
            Log out
          </button>
        }
      >
        <DemoBookingFlow />
      </AuthLayout>
    </main>
  );
}
