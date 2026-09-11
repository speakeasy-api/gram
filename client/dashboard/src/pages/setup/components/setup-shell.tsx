import type { ReactNode } from "react";
import { Link, useLocation, useNavigate, useParams } from "react-router";
import { OnboardingFooter } from "./onboarding-footer";
import { OnboardingHeader } from "./onboarding-header";
import { Button } from "@/components/ui/Button";
import { useOrgRoutes } from "@/routes";

export function SetupShell({ children }: { children: ReactNode }): JSX.Element {
  const navigate = useNavigate();
  const { orgSlug } = useParams();
  const routes = useOrgRoutes();
  const location = useLocation();

  return (
    <div className="bg-background flex h-screen max-h-dvh flex-col overflow-hidden supports-[height:100dvh]:h-dvh">
      <OnboardingHeader onLeave={() => void navigate(`/${orgSlug}`)}>
        <Button asChild variant="tertiary" size="sm">
          <Link
            to={{
              pathname: routes.setup.href(),
              search: location.search,
              hash: location.hash,
            }}
          >
            Return to onboarding
          </Link>
        </Button>
      </OnboardingHeader>
      {children}
      <OnboardingFooter />
    </div>
  );
}
