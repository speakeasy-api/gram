import { Button } from "@/components/ui/button";
import { Type } from "@/components/ui/type";
import {
  ONBOARDING_CTA_CONTENT_VT_CLASS,
  ONBOARDING_CTA_VT_CLASS,
  useOnboardingCta,
} from "@/hooks/useOnboardingCta";
import { cn } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import { ArrowRight, Wrench } from "lucide-react";

export function OnboardingBanner(): JSX.Element | null {
  const orgRoutes = useOrgRoutes();
  const { eligible, dismissed, dismiss } = useOnboardingCta();

  if (!eligible || dismissed) return null;

  return (
    <div
      className={cn(
        "border-border/60 bg-muted/20 dark:bg-white/[0.03] w-full border-b",
        ONBOARDING_CTA_VT_CLASS,
      )}
    >
      <div
        className={cn(
          "mx-auto flex max-w-7xl items-center gap-4 px-8 py-5",
          ONBOARDING_CTA_CONTENT_VT_CLASS,
        )}
      >
        <div className="bg-background border-border/60 flex size-10 shrink-0 items-center justify-center rounded-lg border shadow-sm">
          <Wrench className="text-foreground size-5" strokeWidth={1.75} />
        </div>

        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <Type
            variant="subheading"
            as="span"
            className="text-foreground text-sm leading-tight font-semibold"
          >
            Finish setup
          </Type>
          <Type
            small
            muted
            className="hidden max-w-10/12 text-sm sm:line-clamp-2"
          >
            Set up Single Sign-On, Directory Sync, Plugin Marketplace, Agent
            Platforms, and Policies for your organization.
          </Type>
        </div>

        <div className="flex shrink-0 items-center gap-1">
          <orgRoutes.setup.Link>
            <Button variant="secondary" size="sm" className="group">
              Continue setup
              <ArrowRight className="size-3.5 transition-transform group-hover:translate-x-0.5" />
            </Button>
          </orgRoutes.setup.Link>
          <Button
            variant="ghost"
            size="sm"
            onClick={dismiss}
            aria-label="Dismiss setup banner"
          >
            Dismiss
          </Button>
        </div>
      </div>
    </div>
  );
}
