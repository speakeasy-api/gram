import { EnableLoggingOverlay } from "@/components/EnableLoggingOverlay";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { InsightsToolsContent } from "@/components/observe/InsightsTools";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { useOrganization } from "@/contexts/Auth";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useOrgRoutes, useRoutes } from "@/routes";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { Settings } from "lucide-react";
import type { JSX } from "react";
import { Link, useNavigate } from "react-router";
import { BuiltInDashboardPage } from "../explore/BuiltInDashboardPage";

/** The slug of the Speakeasy-built dashboard the MCP & Tools page is. */
export const MCP_TOOLS_SLUG = "mcp-tools";

/**
 * The MCP & Tools route's body. Behind the Explore flag the page is the
 * Speakeasy-built dashboard; without it, today's page, unchanged. While the
 * flag is still loading a skeleton holds the place, so nobody with the flag
 * sees the old page first and none of its queries run for them.
 */
export function McpToolsPage(): JSX.Element {
  const rollout = useFeatureFlag(FEATURE_FLAGS.explore);
  if (rollout.status === "loading") return <PageSkeleton />;
  if (rollout.status === "enabled") return <McpToolsDashboard />;
  return <InsightsToolsContent />;
}

/**
 * The MCP & Tools page as a Speakeasy-built dashboard: the layout the
 * dashboards service ships, drawn by the same grid and widget view as any
 * dashboard, under the shared filter bar. Open in Explore on a card shows
 * the query behind it, and Duplicate makes a project dashboard to change.
 * With logging off for the organization every card would be empty, so the
 * page shows how to turn it on instead, as the page before it did.
 */
export function McpToolsDashboard(): JSX.Element {
  const organization = useOrganization();
  const features = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );
  const orgRoutes = useOrgRoutes();
  const routes = useRoutes();
  const navigate = useNavigate();

  let body: JSX.Element;
  if (features.isPending) {
    body = <PageSkeleton />;
  } else if (features.data === undefined) {
    // Without the setting the page cannot tell an organization with
    // logging off from one with nothing to show yet, so it says so rather
    // than drawing a grid of empty cards.
    body = (
      <InlineEmptyState
        icon="triangle-alert"
        heading="This page did not load"
        description="Whether logging is on for this organization could not be read."
        action={
          <Button
            variant="secondary"
            size="sm"
            onClick={() => void features.refetch()}
          >
            Try again
          </Button>
        }
      />
    );
  } else if (!features.data.logsEnabled) {
    body = (
      <EnableLoggingOverlay
        onEnabled={() => void features.refetch()}
        screenshotSrc="/empty-states/mcp_insights_empty.png"
        screenshotAlt="MCP and Tools insights dashboard with usage data"
      />
    );
  } else {
    body = (
      <BuiltInDashboardPage
        slug={MCP_TOOLS_SLUG}
        heading="page"
        actions={
          <Button variant="secondary" size="sm" asChild>
            <Link to={orgRoutes.logs.href()}>
              <Settings className="h-4 w-4" />
              Configure settings
            </Link>
          </Button>
        }
        // The copy is a project dashboard, so it opens where those live.
        onOpen={(dashboard) =>
          void navigate(
            `${routes.explore.href()}?tab=dashboards&dashboard=${dashboard.id}`,
          )
        }
      />
    );
  }

  return (
    <div className="min-h-0 w-full flex-1 overflow-y-auto p-8 pb-24">
      <div className="flex flex-col gap-6">
        <Page.Eyebrow />
        {body}
      </div>
    </div>
  );
}

function PageSkeleton(): JSX.Element {
  return (
    <div
      className="flex flex-col gap-4 p-8"
      aria-busy="true"
      aria-label="Loading MCP & Tools"
    >
      <Skeleton className="h-8 w-64" />
      <Skeleton className="h-10 w-full max-w-3xl" />
      <Skeleton className="h-64 w-full" />
    </div>
  );
}
