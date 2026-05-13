import { useSessionData } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useCaptureEnterpriseGateViewed } from "@/contexts/Telemetry";
import { AuthLayout } from "@/pages/login/components/login-section";
import { JourneyDemo } from "@/pages/login/components/journey-demo";
import { Button } from "@speakeasy-api/moonshine";
import {
  CalendarIcon,
  CheckCircle2Icon,
  LogOutIcon,
  MailIcon,
} from "lucide-react";

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

  const orgName = session?.organization?.name;

  return (
    <main className="flex min-h-screen flex-col md:flex-row">
      <JourneyDemo />

      <AuthLayout
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
        {/* Success message */}
        <div className="flex flex-col items-center gap-3 text-center">
          <div className="flex h-12 w-12 items-center justify-center rounded-full bg-emerald-50">
            <CheckCircle2Icon className="h-6 w-6 text-emerald-600" />
          </div>
          <h1 className="text-2xl font-bold tracking-tight text-gray-900">
            {orgName ? `${orgName} is set up` : "You're all set up"}
          </h1>
          <p className="text-sm leading-relaxed text-[#8B8684]">
            Book a demo with our team to activate your account and get started
            with the Speakeasy MCP Platform.
          </p>
        </div>

        {/* What you'll get */}
        <div className="w-full rounded-lg border border-gray-200 bg-white p-5">
          <p className="mb-3 text-xs font-medium tracking-wide text-[#8B8684] uppercase">
            In your demo
          </p>
          <ul className="space-y-2.5 text-sm text-gray-700">
            <li className="flex items-start gap-2.5">
              <span className="mt-0.5 h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" />
              Platform walkthrough tailored to your use case
            </li>
            <li className="flex items-start gap-2.5">
              <span className="mt-0.5 h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" />
              Help configuring MCP servers and integrations
            </li>
            <li className="flex items-start gap-2.5">
              <span className="mt-0.5 h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" />
              Security and governance best practices
            </li>
          </ul>
        </div>

        {/* CTA */}
        <div className="flex w-full flex-col gap-3">
          <Button variant="brand" asChild className="w-full">
            <a
              href="https://www.speakeasy.com/book-demo"
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center justify-center gap-2"
            >
              <CalendarIcon className="h-4 w-4" />
              Book a Demo
            </a>
          </Button>

          <a
            href="mailto:sales@speakeasy.com"
            className="inline-flex items-center justify-center gap-1.5 text-xs text-[#8B8684] transition-colors hover:text-slate-600"
          >
            <MailIcon className="h-3.5 w-3.5" />
            Or contact sales@speakeasy.com
          </a>
        </div>
      </AuthLayout>
    </main>
  );
}
