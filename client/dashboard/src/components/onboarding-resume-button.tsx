import {
  ONBOARDING_CTA_CONTENT_VT_CLASS,
  ONBOARDING_CTA_VT_CLASS,
  useOnboardingCta,
} from "@/hooks/useOnboardingCta";
import { useOrgRoutes } from "@/routes";
import { ArrowUpRight, Wrench } from "lucide-react";
import { SidebarFooterAction } from "./sidebar-footer-action";

/**
 * Persistent entry point back into onboarding, shown as a standout item in the
 * sidebar footer once the {@link OnboardingBanner} has been dismissed. It shares
 * a view-transition name with the banner, so dismissing the banner genies it down
 * into this button, and the arrow-up control genies it back to the top (see
 * `useOnboardingCta`).
 */
export function OnboardingResumeButton(): JSX.Element | null {
  const orgRoutes = useOrgRoutes();
  const { eligible, dismissed, resume } = useOnboardingCta();

  if (!eligible || !dismissed) return null;

  return (
    <SidebarFooterAction
      to={orgRoutes.setup.href()}
      icon={Wrench}
      label="Finish setup"
      className={ONBOARDING_CTA_VT_CLASS}
      contentClassName={ONBOARDING_CTA_CONTENT_VT_CLASS}
      trailing={
        <button
          type="button"
          aria-label="Restore setup banner"
          title="Restore setup banner"
          onClick={resume}
          className="text-muted-foreground hover:text-foreground hover:bg-background/80 shrink-0 rounded-md p-1 transition-colors group-data-[collapsible=icon]:hidden"
        >
          <ArrowUpRight className="size-3.5" />
        </button>
      }
    />
  );
}
