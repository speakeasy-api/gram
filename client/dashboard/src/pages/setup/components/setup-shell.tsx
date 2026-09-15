import type { ReactNode } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router";
import { OnboardingFooter } from "./onboarding-footer";
import { OnboardingHeader } from "./onboarding-header";
import { SetupViewButton } from "./setup-view-button";

export function SetupShell({
  children,
  isPending = false,
}: {
  children: ReactNode;
  isPending?: boolean;
}): JSX.Element {
  const navigate = useNavigate();
  const { orgSlug } = useParams();
  const [searchParams] = useSearchParams();
  const fromWorkstreams = searchParams.get("from") === "workstreams";

  return (
    <div className="bg-background flex h-screen max-h-dvh flex-col overflow-hidden supports-[height:100dvh]:h-dvh">
      <OnboardingHeader onLeave={() => void navigate(`/${orgSlug}`)}>
        {fromWorkstreams ? (
          <div className="md:hidden">
            <SetupViewButton wizard disabled={isPending} />
          </div>
        ) : (
          <SetupViewButton wizard disabled={isPending} />
        )}
      </OnboardingHeader>
      {children}
      <OnboardingFooter />
    </div>
  );
}
