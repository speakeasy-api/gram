import {
  BRAND_MESH_SURFACE_CLASS,
  BrandMeshLayers,
} from "@/components/brand-mesh";
import { DEFAULT_DATE_RANGE_PRESET } from "@/components/observe/useDateRangeFilter";
import { buildProjectOverviewQuery } from "@/components/project/projectOverviewQuery";
import { PROJECT_GUIDE_ENTRY_PATH } from "@/components/project-guide/GuideEntryRedirect";
import { Button } from "@/components/ui/Button";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { useOnboardingCta } from "@/hooks/useOnboardingCta";
import { useOrgSetupStarted } from "@/hooks/useOrgSetupStarted";
import { useOrgWelcomeBanner } from "@/hooks/useOrgWelcomeBanner";
import { usePlatformMcpDashboardVisibility } from "@/hooks/usePlatformMcpDashboardVisibility";
import { useRBAC } from "@/hooks/useRBAC";
import { getPreferredProject } from "@/lib/preferredProject";
import { getTrialLifecycleFromDates } from "@/lib/trial-status";
import { cn } from "@/lib/utils";
import {
  activeOrgHomeAnnouncement,
  useOrgHomeAnnouncement,
} from "@/pages/org/orgHomeAnnouncements";
import {
  isOverviewZeroData,
  recommendedWelcomeCardId,
  selectWelcomeCardIds,
  welcomeHeadline,
  type WelcomeCardId,
} from "@/pages/org/orgWelcomeBannerState";
import { useOrgRoutes, useRoutes } from "@/routes";
import {
  Action,
  Surface,
} from "@gram/client/models/components/recorddashboardctaeventrequestbody.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { useRecordPlatformMCPDashboardCtaEventMutation } from "@gram/client/react-query/recordPlatformMCPDashboardCtaEvent.js";
import { useQuery } from "@tanstack/react-query";
import { Fragment, useEffect, useRef } from "react";
import { Link } from "react-router";

// Matches the page column OrgHome applies to everything below the banner.
const COLUMN_CLASS = "mx-auto w-full max-w-7xl px-8";

const CARD_SHELL_CLASS =
  "group bg-card border-border hover:border-foreground relative flex min-h-[250px] flex-col gap-3 overflow-hidden border px-6.5 pt-7.5 pb-6.5 no-underline transition-colors hover:no-underline";

type RouteCard = {
  id: WelcomeCardId;
  index: string;
  title: string;
  body: string;
  cta: string;
  meta?: string;
  to: string;
  recommended?: boolean;
  onClick?: () => void;
};

/**
 * Welcome banner for org home: a hero over up to three paths into the
 * dashboard, chosen from trial × admin × zero-data.
 */
export function OrgWelcomeBanner(): JSX.Element | null {
  const organization = useOrganization();
  const { trial } = useSession();
  const { orgSlug } = useSlugs();
  const orgRoutes = useOrgRoutes();
  const { visible } = useOrgWelcomeBanner();
  const { hasScope } = useRBAC();
  const { setupStarted, markSetupStarted } = useOrgSetupStarted(orgSlug);
  const { eligible: canSetUpOrg } = useOnboardingCta();
  const { enabled: platformMcpEnabled } = usePlatformMcpDashboardVisibility();
  const { data: featuresData } = useProductFeatures({
    organizationId: organization.id,
  });
  const logsEnabled = featuresData?.logsEnabled === true;
  const recordCta = useRecordPlatformMCPDashboardCtaEventMutation();
  const announcement = activeOrgHomeAnnouncement();
  const { dismissed: announcementDismissed, dismiss: dismissAnnouncement } =
    useOrgHomeAnnouncement(orgSlug, announcement?.id);

  const startProject =
    getPreferredProject(organization.projects) ??
    organization.projects.find((project) => project.slug === "default") ??
    organization.projects[0];

  const gramClient = useGramContext();
  const { data: overview, isPending: isOverviewPending } = useQuery({
    ...buildProjectOverviewQuery(gramClient, {
      organization: organization.slug,
      project: startProject?.slug ?? "",
      range: { preset: DEFAULT_DATE_RANGE_PRESET },
    }),
    enabled: logsEnabled && Boolean(organization.slug && startProject?.slug),
  });

  const isTrial = getTrialLifecycleFromDates(trial, new Date()) === "active";
  const isAdmin = hasScope("org:admin");
  const isZeroData =
    !startProject ||
    !logsEnabled ||
    isOverviewPending ||
    isOverviewZeroData(overview);

  const cardIds = selectWelcomeCardIds({
    isTrial,
    isAdmin,
    isZeroData,
    canSetUpOrg,
    platformMcpEnabled,
  });
  const recommendedId = recommendedWelcomeCardId(cardIds);

  const projectRoutes = useRoutes({ projectSlug: startProject?.slug });
  const platformMcpHref = `${orgRoutes.platformMcp.href()}?setup=1&entrySource=organization_home`;
  const cards: RouteCard[] = cardIds.map((id, i) => {
    const recommended = id === recommendedId;
    const index = String(i + 1).padStart(2, "0");
    switch (id) {
      case "demo":
        return {
          id,
          index,
          title: "Explore the demo org",
          body: "A read-only organization with two weeks of simulated agent traffic, spend, and blocked calls.",
          cta: "Enter demo org",
          meta: "Read-only · simulated data",
          to: projectRoutes.exploreDemo.href(),
          recommended,
        };
      case "guide":
        return {
          id,
          index,
          title: "Project guide",
          body: "Get to one observable win: a governed MCP call, or a prompt you watch get blocked.",
          cta: "Open the guide",
          meta: "~5 min · guided tour",
          to: PROJECT_GUIDE_ENTRY_PATH,
          recommended,
        };
      case "enterprise":
        return {
          id,
          index,
          title: setupStarted
            ? "Continue enterprise rollout"
            : "Start enterprise rollout",
          body: "SSO, directory sync, agent platforms, and policies — the wizard walks the whole sequence.",
          cta: setupStarted ? "Resume rollout" : "Begin rollout",
          meta: "8 steps · resumable",
          to: orgRoutes.setup.href(),
          recommended,
          onClick: markSetupStarted,
        };
      case "platformMcp":
        return {
          id,
          index,
          title: "Platform MCP setup",
          body: "Install Speakeasy in Claude, Cursor, or Codex so the agent can work against this org.",
          cta: "Set up Platform MCP",
          meta: "5 agents supported",
          to: platformMcpHref,
          recommended,
          onClick: () => {
            recordCta.mutate({
              request: {
                recordDashboardCtaEventRequestBody: {
                  action: Action.Selected,
                  surface: Surface.OrganizationHome,
                },
              },
            });
          },
        };
      case "defaultProject":
        return {
          id,
          index,
          title: startProject ? startProject.name : "Your project",
          body: "Jump back into the project you were last working in.",
          cta: "Open project",
          to: startProject ? projectRoutes.home.href() : orgRoutes.home.href(),
          recommended,
        };
    }
  });

  const showAnnouncement =
    announcement !== null && !isTrial && !announcementDismissed;
  const recordedImpression = useRef(false);

  useEffect(() => {
    if (!visible || recordedImpression.current) return;
    if (!cardIds.includes("platformMcp")) return;
    recordedImpression.current = true;
    recordCta.mutate({
      request: {
        recordDashboardCtaEventRequestBody: {
          action: Action.Impression,
          surface: Surface.OrganizationHome,
        },
      },
    });
  }, [visible, cardIds, recordCta]);

  if (!visible) return null;

  const columnCount = cards.length + (showAnnouncement ? 1 : 0);
  const headlineLines = welcomeHeadline({ columnCount, isTrial, isZeroData });

  return (
    // Section runs the full width of the content area; the column class below
    // keeps its contents aligned with the rest of the page.
    <section className={cn(BRAND_MESH_SURFACE_CLASS, "w-full")}>
      {/* Mesh spans the whole banner; the gray band below paints over it. */}
      <BrandMeshLayers />
      <div
        className={cn(
          COLUMN_CLASS,
          "flex flex-col gap-4 pt-10 pb-10 lg:pt-12 lg:pb-28",
        )}
      >
        <span className="text-eyebrow">Welcome to Speakeasy</span>
        <h2 className="text-foreground font-display text-[40px] leading-[0.92] font-thin tracking-[-0.04em] lg:text-[60px]">
          {headlineLines.map((line, i) => (
            <Fragment key={line}>
              {i > 0 ? <br /> : null}
              {line}
            </Fragment>
          ))}
        </h2>
      </div>

      <div className="bg-background/50 flow-root w-full border-border border-y">
        <div className={cn(COLUMN_CLASS, "pt-8 lg:pt-0 pb-8")}>
          <div
            className={cn(
              "grid grid-cols-1 gap-4 lg:-mt-20",
              // One/two cards stay on 2-col so a lone card caps at 50%.
              columnCount > 2 ? "lg:grid-cols-3" : "lg:grid-cols-2",
            )}
          >
            {cards.map((card) => (
              <RouteCardLink key={card.id} card={card} />
            ))}
            {showAnnouncement && announcement ? (
              <AnnouncementCard
                onDismiss={dismissAnnouncement}
                to={announcement.to}
                title={announcement.title}
                body={announcement.body}
                cta={announcement.cta}
              />
            ) : null}
          </div>
        </div>
      </div>
    </section>
  );
}

function RouteCardLink({ card }: { card: RouteCard }): JSX.Element {
  return (
    <Link to={card.to} onClick={card.onClick} className={CARD_SHELL_CLASS}>
      {card.recommended && (
        <span
          aria-hidden="true"
          className="bg-gradient-primary absolute inset-x-0 top-0 h-1"
        />
      )}

      <span className="flex items-center gap-2.5">
        <span className="text-muted-foreground font-mono text-xs tracking-wider">
          {card.index}
        </span>
        {card.recommended && (
          <span className="border-foreground text-foreground border px-1.5 py-px font-mono text-[9px] tracking-[0.08em] uppercase">
            Recommended
          </span>
        )}
      </span>

      <span className="text-foreground max-w-[22ch] text-[23px] leading-[1.2]">
        {card.title}
      </span>
      <span className="text-muted-foreground text-[13px] leading-[1.6]">
        {card.body}
      </span>

      <span className="text-foreground mt-auto flex items-center gap-2.5 font-mono text-[13px]">
        {card.cta}
        <span
          aria-hidden="true"
          className="text-muted-foreground transition-transform group-hover:translate-x-0.5"
        >
          →
        </span>
      </span>
      {card.meta ? (
        <span className="text-muted-foreground font-mono text-[10.5px] tracking-[0.04em]">
          {card.meta}
        </span>
      ) : null}
    </Link>
  );
}

function AnnouncementCard({
  title,
  body,
  cta,
  to,
  onDismiss,
}: {
  title: string;
  body: string;
  cta: string;
  to: string;
  onDismiss: () => void;
}): JSX.Element {
  return (
    <div className="relative">
      <Link to={to} className={CARD_SHELL_CLASS}>
        <span className="text-eyebrow">Announcement</span>
        <span className="text-foreground max-w-[22ch] text-[23px] leading-[1.2]">
          {title}
        </span>
        <span className="text-muted-foreground text-[13px] leading-[1.6]">
          {body}
        </span>
        <span className="text-foreground mt-auto flex items-center gap-2.5 font-mono text-[13px]">
          {cta}
          <span
            aria-hidden="true"
            className="text-muted-foreground transition-transform group-hover:translate-x-0.5"
          >
            →
          </span>
        </span>
      </Link>
      <Button
        type="button"
        variant="tertiary"
        size="sm"
        icon="x"
        aria-label="Dismiss announcement"
        className="absolute top-3 right-3"
        onClick={(event) => {
          event.preventDefault();
          event.stopPropagation();
          onDismiss();
        }}
      />
    </div>
  );
}
