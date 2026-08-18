import {
  BRAND_MESH_SURFACE_CLASS,
  BrandMeshLayers,
} from "@/components/brand-mesh";
import { useOrgRoutes, useRoutes } from "@/routes";
import {
  usePlatformMcpCta,
  usePlatformMcpCtaImpression,
} from "@/hooks/usePlatformMcpCta";

import { Link } from "react-router";
import { X } from "lucide-react";
import { cn } from "@/lib/utils";
import { getPreferredProject } from "@/lib/preferredProject";
import { useOnboardingCta } from "@/hooks/useOnboardingCta";
import { useOrgSetupStarted } from "@/hooks/useOrgSetupStarted";
import { useOrgWelcomeBanner } from "@/hooks/useOrgWelcomeBanner";
import { useOrganization } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";

// Matches the page column OrgHome applies to everything below the banner.
const COLUMN_CLASS = "mx-auto w-full max-w-7xl px-8";

type RouteCard = {
  index: string;
  title: string;
  body: string;
  cta: string;
  meta: string;
  to: string;
  recommended?: boolean;
  onClick?: () => void;
};

/**
 * Zero-data welcome banner for org home: a hero over three first moves — demo
 * org, start in a project, org setup wizard.
 */
export function OrgWelcomeBanner(): JSX.Element | null {
  const organization = useOrganization();
  const { orgSlug } = useSlugs();
  const orgRoutes = useOrgRoutes();
  const { visible } = useOrgWelcomeBanner();
  const { setupStarted, markSetupStarted } = useOrgSetupStarted(orgSlug);
  // Same gate as the header's "Finish setup" banner: the wizard is an
  // enterprise org-admin surface.
  const { eligible: canSetUpOrg } = useOnboardingCta();

  // Same preference order as the overview prefetch on org home.
  const startProject =
    getPreferredProject(organization.projects) ??
    organization.projects.find((project) => project.slug === "default") ??
    organization.projects[0];

  // Org home has no project slug of its own, so the project routes resolve
  // against the project card 02 points at.
  const projectRoutes = useRoutes({ projectSlug: startProject?.slug });
  const {
    dismiss: dismissPlatformMcp,
    href: platformMcpHref,
    label: platformMcpLabel,
    recordImpression: recordPlatformMcpImpression,
    recordSelected: recordPlatformMcpSelected,
    visible: platformMcpVisible,
  } = usePlatformMcpCta({
    surface: "organization_home",
    projectSlug: startProject?.slug,
  });

  const platformMcpImpressionRef = usePlatformMcpCtaImpression(
    visible && platformMcpVisible,
    recordPlatformMcpImpression,
  );

  const cards: RouteCard[] = [
    {
      index: "01",
      title: "Explore the demo org",
      body: "A read-only organization with two weeks of simulated agent traffic, spend, and blocked calls.",
      cta: "Enter demo org",
      meta: "Read-only · simulated data",
      to: projectRoutes.exploreDemo.href(),
    },
    ...(platformMcpVisible
      ? []
      : [
          {
            index: "02",
            title: "Get started",
            body: "Start getting your own data into the dashboard. Connect an MCP server, or set a policy and watch it block a call.",
            cta: "Start using Speakeasy",
            meta: "~5 minutes · your data",
            to: startProject
              ? projectRoutes.home.href()
              : orgRoutes.home.href(),
            recommended: true,
          },
        ]),
  ];

  if (canSetUpOrg) {
    cards.push({
      index: "03",
      title: setupStarted
        ? "Continue enterprise rollout"
        : "Start enterprise rollout",
      body: "SSO, directory sync, agent platforms, and policies — the wizard walks the whole sequence.",
      cta: setupStarted ? "Resume rollout" : "Begin rollout",
      meta: "5 steps · resumable",
      to: orgRoutes.setup.href(),
      onClick: markSetupStarted,
    });
  }

  if (!visible) return null;

  return (
    // Section runs the full width of the content area; the column class below
    // keeps its contents aligned with the rest of the page.
    <section className={cn(BRAND_MESH_SURFACE_CLASS, "w-full")}>
      {/* Mesh spans the whole banner; the gray band below paints over it. */}
      <BrandMeshLayers />
      {/* Deep bottom padding leaves room for the cards to overlap the hero. */}
      <div
        className={cn(
          COLUMN_CLASS,
          "flex flex-col gap-4 pt-10 pb-10 lg:pt-12 lg:pb-28",
        )}
      >
        <span className="text-eyebrow">Welcome to Speakeasy</span>
        <h2 className="text-foreground font-display text-[40px] leading-[0.92] font-thin tracking-[-0.04em] lg:text-[60px]">
          Choose your
          <br />
          first move
        </h2>
      </div>

      {/* flow-root keeps the cards' negative margin from collapsing out
          through the band, which would drag the band up over the hero. */}
      <div className="bg-background/50 flow-root w-full border-border border-y">
        <div className={cn(COLUMN_CLASS, "pt-8 lg:pt-0 pb-8")}>
          {/* Stacked below lg, side by side above — 2 or 3 across depending
              on whether the setup card is present. */}
          <div
            className={cn(
              "grid grid-cols-1 gap-4 lg:-mt-20",
              cards.length + (platformMcpVisible ? 1 : 0) === 3
                ? "lg:grid-cols-3"
                : "lg:grid-cols-2",
            )}
          >
            <RouteCardLink card={cards[0]!} />
            {platformMcpVisible && (
              <PlatformMcpRouteCard
                href={platformMcpHref}
                label={platformMcpLabel}
                impressionRef={platformMcpImpressionRef}
                onDismiss={dismissPlatformMcp}
                onSelect={recordPlatformMcpSelected}
              />
            )}
            {cards.slice(1).map((card) => (
              <RouteCardLink key={card.index} card={card} />
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}

function PlatformMcpRouteCard({
  href,
  label,
  impressionRef,
  onDismiss,
  onSelect,
}: {
  href: string;
  label: string;
  impressionRef: (node: HTMLDivElement | null) => void;
  onDismiss: () => void;
  onSelect: () => void;
}): JSX.Element {
  return (
    <div
      ref={impressionRef}
      className="group bg-card border-border hover:border-foreground relative flex min-h-[250px] flex-col gap-3 border px-6.5 pt-7.5 pb-6.5 transition-colors"
    >
      <Link
        to={href}
        onClick={onSelect}
        aria-label={`Platform MCP: Bring setup into your agent. ${label}`}
        className="absolute inset-0 z-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
      />
      <span
        aria-hidden="true"
        className="bg-gradient-primary absolute inset-x-0 top-0 z-10 h-1 pointer-events-none"
      />
      <button
        type="button"
        aria-label="Dismiss Platform MCP recommendation"
        title="Dismiss Platform MCP recommendation"
        onClick={onDismiss}
        className="text-muted-foreground hover:text-foreground absolute top-4 right-4 z-20 p-1"
      >
        <X className="size-4" />
      </button>
      <span className="relative z-10 flex items-center gap-2.5 pointer-events-none">
        <span className="text-muted-foreground font-mono text-xs tracking-wider">
          02
        </span>
        <span className="border-foreground text-foreground border px-1.5 py-px font-mono text-[9px] tracking-[0.08em] uppercase">
          Recommended
        </span>
      </span>
      <span className="text-foreground relative z-10 max-w-[22ch] text-[23px] leading-[1.2] pointer-events-none">
        Bring setup into your agent
      </span>
      <span className="text-muted-foreground relative z-10 text-[13px] leading-[1.6] pointer-events-none">
        Connect Platform MCP to choose a reviewed MCP catalogue server and add
        it to this project&apos;s Default plugin.
      </span>
      <span className="text-foreground relative z-10 mt-auto flex items-center gap-2.5 font-mono text-[13px] pointer-events-none">
        {label}
        <span
          aria-hidden="true"
          className="text-muted-foreground transition-transform group-hover:translate-x-0.5"
        >
          →
        </span>
      </span>
      <span className="text-muted-foreground relative z-10 font-mono text-[10.5px] tracking-[0.04em] pointer-events-none">
        Reviewed catalogue · resumable
      </span>
    </div>
  );
}

function RouteCardLink({ card }: { card: RouteCard }): JSX.Element {
  return (
    <Link
      to={card.to}
      onClick={card.onClick}
      className="group bg-card border-border hover:border-foreground relative flex min-h-[250px] flex-col gap-3 overflow-hidden border px-6.5 pt-7.5 pb-6.5 no-underline transition-colors hover:no-underline"
    >
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
      <span className="text-muted-foreground font-mono text-[10.5px] tracking-[0.04em]">
        {card.meta}
      </span>
    </Link>
  );
}
