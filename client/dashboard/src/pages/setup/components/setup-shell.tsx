import type { ReactNode } from "react";
import { useNavigate, useParams } from "react-router";
import { OnboardingFooter } from "./onboarding-footer";
import { OnboardingHeader } from "./onboarding-header";

export function SetupShell({ children }: { children: ReactNode }): JSX.Element {
  const navigate = useNavigate();
  const { orgSlug } = useParams();

  return (
    <div className="bg-background flex h-screen max-h-dvh flex-col overflow-hidden supports-[height:100dvh]:h-dvh">
      <OnboardingHeader onLeave={() => void navigate(`/${orgSlug}`)} />
      {children}
      <OnboardingFooter />
    </div>
  );
}
