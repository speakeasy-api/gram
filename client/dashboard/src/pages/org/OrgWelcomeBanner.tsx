import {
  BRAND_MESH_SURFACE_CLASS,
  BrandMeshLayers,
} from "@/components/brand-mesh";
import { useOrganization } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { getPreferredProject } from "@/lib/preferredProject";
import { cn } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import { Link } from "react-router";

type RouteCard = {
  index: string;
  title: string;
  body: string;
  cta: string;
  meta: string;
  to: string;
  /** Draws the brand gradient edge and the "Recommended" tag. */
  recommended?: boolean;
};

/**
 * Zero-data welcome banner for org home: one hero and three mutually exclusive
 * first moves (demo org / start in a project / org setup wizard).
 *
 * Renders unconditionally for now — the visibility rule (which orgs count as
 * "new", and how the banner is dismissed) is still open, so nothing here reads
 * onboarding state yet.
 */
export function OrgWelcomeBanner(): JSX.Element {
  const organization = useOrganization();
  const { orgSlug } = useSlugs();
  const orgRoutes = useOrgRoutes();

  // Same preference order as the overview prefetch below it on org home: last
  // visited, else `default`, else whatever exists. A brand-new org always has
  // `default`; the org-home fallback only covers the degenerate no-project case.
  const startProject =
    getPreferredProject(organization.projects) ??
    organization.projects.find((project) => project.slug === "default") ??
    organization.projects[0];

  const cards: RouteCard[] = [
    {
      index: "01",
      title: "Explore the demo org",
      body: "A read-only organization with two weeks of simulated agent traffic, spend, and blocked calls.",
      cta: "Enter demo org",
      meta: "Read-only · simulated data",
      to: "/explore-demo",
    },
    {
      index: "02",
      title: "Get started",
      body: "Start getting your own data into the dashboard. Connect an MCP server, or set a policy and watch it block a call.",
      cta: "Start using Speakeasy",
      meta: "~5 minutes · your data",
      to: startProject
        ? `/${orgSlug}/projects/${startProject.slug}`
        : `/${orgSlug}`,
      recommended: true,
    },
    {
      index: "03",
      title: "Set up the organization",
      body: "SSO, directory sync, agent platforms, and policies — the wizard walks the whole sequence.",
      cta: "Start setup wizard",
      meta: "5 steps · resumable",
      to: orgRoutes.setup.href(),
    },
  ];

  return (
    <section className="border-border border">
      {/* Deep bottom padding leaves room for the cards to pull up into the
          hero — the overlap is the point, so the mesh reads behind them. */}
      <div
        className={cn(
          BRAND_MESH_SURFACE_CLASS,
          "flex flex-col gap-4 px-6 pt-10 pb-10 md:px-11 md:pt-11 md:pb-28",
        )}
      >
        <BrandMeshLayers />
        <span className="text-eyebrow">Welcome to Speakeasy</span>
        <h2 className="text-foreground font-display text-[40px] leading-[0.92] font-thin tracking-[-0.04em] md:text-[60px]">
          Choose your
          <br />
          first move
        </h2>
      </div>

      <div className="grid grid-cols-1 gap-4 px-6 pb-6 md:-mt-20 md:grid-cols-3 md:px-11 md:pb-10">
        {cards.map((card) => (
          <RouteCardLink key={card.index} card={card} />
        ))}
      </div>
    </section>
  );
}

function RouteCardLink({ card }: { card: RouteCard }): JSX.Element {
  return (
    <Link
      to={card.to}
      className="group bg-card border-border hover:border-foreground relative flex min-h-[250px] flex-col gap-3 border px-6.5 pt-7.5 pb-6.5 no-underline transition-colors hover:no-underline"
    >
      {card.recommended && (
        // Pulled a pixel past each edge so the strip covers the card border
        // rather than sitting inside it.
        <span
          aria-hidden="true"
          className="bg-gradient-primary absolute -top-px -right-px -left-px h-[3px]"
        />
      )}

      <span className="flex items-center gap-2.5">
        <span className="text-muted-foreground font-mono text-[11px] tracking-[0.06em]">
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
