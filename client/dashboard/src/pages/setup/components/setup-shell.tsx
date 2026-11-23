import type { ReactNode } from "react";
import { useNavigate, useParams } from "react-router";
import { OnboardingFooter } from "./onboarding-footer";
import { OnboardingHeader } from "./onboarding-header";

export function SetupShell({ children }: { children: ReactNode }): JSX.Element {
  const navigate = useNavigate();
  const { orgSlug } = useParams();
  const homeHref = `/${orgSlug}`;

  return (
    <div className="bg-background flex h-screen max-h-dvh flex-col overflow-hidden supports-[height:100dvh]:h-dvh">
      <OnboardingHeader
        homeHref={homeHref}
        onLeave={() => void navigate(homeHref)}
      />
      {children}
      <OnboardingFooter />
    </div>
  );
}
