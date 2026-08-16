import { FullBleedBanner } from "@/components/full-bleed-banner";
import { Button } from "@/components/ui/Button";
import {
  ONBOARDING_CTA_CONTENT_VT_CLASS,
  ONBOARDING_CTA_VT_CLASS,
  useOnboardingCta,
} from "@/hooks/useOnboardingCta";
import { useSlugs } from "@/contexts/Sdk";
import { useOrgRoutes } from "@/routes";
import { ArrowRight, Wrench } from "lucide-react";

export function OnboardingBanner(): JSX.Element | null {
  const orgRoutes = useOrgRoutes();
  const { projectSlug } = useSlugs();
  const { eligible, dismissed, dismiss } = useOnboardingCta();

  // Project-scoped routes (/:orgSlug/projects/:projectSlug/...) are too deep
  // in a specific workflow for an org-wide setup nudge — only show this on
  // org-level pages (home, org settings, etc).
  if (!eligible || dismissed || projectSlug) return null;

  return (
    <FullBleedBanner
      icon={Wrench}
      title="Finish setup"
      description="Set up Single Sign-On, Directory Sync, Plugin Marketplace, Agent Platforms, and Policies for your organization."
      // The setup list is long enough to crowd out the actions on a narrow
      // screen, where the heading alone still says what the banner is for.
      descriptionClassName="hidden max-w-10/12 sm:line-clamp-2"
      className={ONBOARDING_CTA_VT_CLASS}
      contentClassName={ONBOARDING_CTA_CONTENT_VT_CLASS}
      actions={
        <>
          <orgRoutes.setup.Link>
            <Button variant="secondary" size="sm" className="group">
              Continue setup
              <ArrowRight className="size-3.5 transition-transform group-hover:translate-x-0.5" />
            </Button>
          </orgRoutes.setup.Link>
          <Button
            variant="tertiary"
            size="sm"
            onClick={dismiss}
            aria-label="Dismiss setup banner"
          >
            Dismiss
          </Button>
        </>
      }
    />
  );
}
