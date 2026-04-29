import { useSession } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRoutes } from "@/routes";
import { useEffect, useRef } from "react";
import { Navigate, useLocation } from "react-router";
import { NewAssistantOnboarding } from "./AssistantOnboarding";
import { OnboardingFrame } from "./OnboardingFrame";

export function AssistantsOnboardingPage() {
  const session = useSession();
  const location = useLocation();
  const telemetry = useTelemetry();
  const routes = useRoutes();
  const startedRef = useRef(false);

  const isUnauthenticated = !session.session || !session.activeOrganizationId;

  useEffect(() => {
    if (isUnauthenticated) return;
    if (startedRef.current) return;
    startedRef.current = true;
    telemetry.capture("assistants_onboarding_started");
  }, [isUnauthenticated, telemetry]);

  if (isUnauthenticated) {
    const returnTo = encodeURIComponent(location.pathname + location.search);
    return <Navigate to={`/register?returnTo=${returnTo}`} replace />;
  }

  const handleDone = (assistantId: string) => {
    telemetry.capture("assistants_onboarding_completed", { assistantId });
    routes.assistants.goTo();
  };

  return (
    <OnboardingFrame onExit={() => routes.assistants.goTo()}>
      <NewAssistantOnboarding onAssistantSaved={handleDone} />
    </OnboardingFrame>
  );
}
