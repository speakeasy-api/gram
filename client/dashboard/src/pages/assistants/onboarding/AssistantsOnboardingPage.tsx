import { useSession } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRoutes } from "@/routes";
import { useCallback, useEffect, useRef } from "react";
import { useLocation, useNavigate } from "react-router";
import { NewAssistantOnboarding } from "./AssistantOnboarding";
import { AssistantsOnboardingPreview } from "./AssistantsOnboardingPreview";
import { OnboardingFrame } from "./OnboardingFrame";

export function AssistantsOnboardingPage() {
  const session = useSession();
  const location = useLocation();
  const navigate = useNavigate();
  const telemetry = useTelemetry();
  const routes = useRoutes();
  const startedRef = useRef(false);

  const isAuthenticated = !!session.session && !!session.activeOrganizationId;

  const handleDone = useCallback(
    (assistantId: string) => {
      telemetry.capture("assistants_onboarding_completed", { assistantId });
      routes.assistants.goTo();
    },
    [telemetry, routes.assistants],
  );

  const handleLogin = useCallback(() => {
    const returnTo = encodeURIComponent(location.pathname + location.search);
    navigate(`/login?returnTo=${returnTo}`);
  }, [location.pathname, location.search, navigate]);

  useEffect(() => {
    if (!isAuthenticated) return;
    if (startedRef.current) return;
    startedRef.current = true;
    telemetry.capture("assistants_onboarding_started");
  }, [isAuthenticated, telemetry]);

  if (!isAuthenticated) {
    return (
      <OnboardingFrame onPrimaryAction={handleLogin} primaryActionLabel="Login">
        <AssistantsOnboardingPreview onLoginPrompt={handleLogin} />
      </OnboardingFrame>
    );
  }

  return (
    <OnboardingFrame
      onPrimaryAction={() => routes.assistants.goTo()}
      primaryActionLabel="Exit to AI control plane"
    >
      <NewAssistantOnboarding onAssistantSaved={handleDone} chromeless />
    </OnboardingFrame>
  );
}
