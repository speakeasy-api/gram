import {
  BRAND_MESH_SURFACE_CLASS,
  BrandMeshLayers,
} from "@/components/brand-mesh";
import { DEFAULT_DATE_RANGE_PRESET } from "@/components/observe/useDateRangeFilter";
import { buildProjectOverviewQuery } from "@/components/project/projectOverviewQuery";
import { PROJECT_GUIDE_ENTRY_PATH } from "@/components/project-guide/GuideEntryRedirect";
import { Button } from "@/components/ui/Button";
import { useOrganization, useSession, useUser } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { useSlugs } from "@/contexts/Sdk";
import { useCanSetUpOrg } from "@/hooks/useCanSetUpOrg";
import { createDismissedCtaStore } from "@/hooks/useDismissedCtaStore";
import { useOrganizationPlatformMCPOnboarding } from "@/hooks/useOrganizationPlatformMCPOnboarding";
import { useOrgSetupStarted } from "@/hooks/useOrgSetupStarted";
import { useOrgWelcomeBanner } from "@/hooks/useOrgWelcomeBanner";
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
const dismissedMemberCta = createDismissedCtaStore(
  "gram:platform-mcp-member-promotion:v1",
);

type RouteCard = {
  id: WelcomeCardId | "memberPlatformMcp";
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
  const user = useUser();
  const telemetry = useTelemetry();
  const { trial } = useSession();
  const { orgSlug } = useSlugs();
  const orgRoutes = useOrgRoutes();
  const { visible } = useOrgWelcomeBanner();
  const { hasScope, isLoading: grantsLoading, error: grantsError } = useRBAC();
  const { setupStarted, markSetupStarted } = useOrgSetupStarted(orgSlug);
  const canSetUpOrg = useCanSetUpOrg();
  const { data: featuresData } = useProductFeatures({
    organizationId: organization.id,
  });
  const logsEnabled = featuresData?.logsEnabled === true;
  const platformMcpEnabled = featuresData?.platformMcpEnabled === true;
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
  const memberEligible =
    !grantsLoading &&
    !grantsError &&
    !isAdmin &&
    (hasScope("project:read") ||
      hasScope("skill:read") ||
      hasScope("mcp:read"));
  const onboarding = useOrganizationPlatformMCPOnboarding(organization.id, {
    enabled: visible && memberEligible,
    throwOnError: false,
  });
  const memberKey =
    user.id && organization.id ? `${user.id}:${organization.id}` : undefined;
  const memberDismissed = dismissedMemberCta.useDismissed(memberKey);
  const showMemberCard =
    visible &&
    !!memberKey &&
    memberEligible &&
    !memberDismissed &&
    onboarding.data?.enabled &&
    !(
      onboarding.data.connectionAuthorized &&
      onboarding.data.connectionAuthState === "active"
    ) &&
    !onboarding.isError;
  const memberImpression = useRef<string | null>(null);
  useEffect(() => {
    if (!showMemberCard || memberImpression.current === memberKey) return;
    memberImpression.current = memberKey;
    telemetry.capture("platform_mcp_member_cta", {
      action: "impression",
      workflow: "organization_home",
    });
  }, [showMemberCard, memberKey, telemetry]);
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
  const platformMcpHref = `${orgRoutes.headless.href()}?entrySource=organization_home`;
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
          body: "SSO, directory sync, logging, agent platforms, and policies — the board tracks the whole sequence.",
          cta: setupStarted ? "Resume rollout" : "Begin rollout",
          meta: "Assignable · resumable",
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

  if (showMemberCard) {
    cards.push({
      id: "memberPlatformMcp",
      index: String(cards.length + 1).padStart(2, "0"),
      title: "Use Speakeasy from your agent",
      body: "Work with the projects and tools your role can access, right from your agent.",
      cta: "Connect your agent",
      to: platformMcpHref,
      onClick: () => {
        telemetry.capture("platform_mcp_member_cta", {
          action: "selected",
          workflow: "organization_home",
        });
      },
    });
  }

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
        <h2 className="text-foreground font-display text-[40px] leading-[0.98] font-thin tracking-[-0.04em] lg:text-[60px]">
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
            {cards.map((card) =>
              card.id === "memberPlatformMcp" ? (
                <div key={card.id} className="relative">
                  <RouteCardLink card={card} />
                  <Button
                    type="button"
                    variant="tertiary"
                    size="sm"
                    icon="x"
                    aria-label="Dismiss Platform MCP suggestion"
                    className="absolute top-3 right-3"
                    onClick={() => {
                      if (!memberKey) return;
                      dismissedMemberCta.write(memberKey, true);
                      telemetry.capture("platform_mcp_member_cta", {
                        action: "dismissed",
                        workflow: "organization_home",
                      });
                    }}
                  />
                </div>
              ) : (
                <RouteCardLink key={card.id} card={card} />
              ),
            )}
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
