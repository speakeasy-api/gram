import { InlineEmptyState } from "@/components/inline-empty-state";
import { ResourceListPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { DotRow } from "@/components/ui/DotRow";
import { DotTable } from "@/components/ui/DotTable";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { Heading } from "@/components/ui/Heading";
import { SplashScreen } from "@/components/ui/LogoLoader";
import { SkeletonTable } from "@/components/ui/SkeletonTable";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useOrgRoutes } from "@/routes";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { MoreHorizontal } from "lucide-react";
import { useMemo, useState } from "react";
import { Navigate, Outlet } from "react-router";
import { ConsolidateIssuerDialog } from "./ConsolidateIssuerDialog";
import {
  authorizationServerModeLabel,
  consolidationTargets,
} from "./issuerDisplay";
import { MakeOrganizationWideDialog } from "./MakeOrganizationWideDialog";
import {
  retryFailedTiers,
  useIssuerTier,
  type IssuerTier,
} from "./useIssuerTier";

/** Route shell: the whole surface waits on its rollout flag. */
export function UserSessionIssuersRoot(): JSX.Element | null {
  const flag = useFeatureFlag(FEATURE_FLAGS.userSessionIssuerDashboard);
  // Hold the splash until flags resolve rather than flashing a redirect.
  if (flag.status === "loading") return <SplashScreen />;
  if (flag.status !== "enabled") return <Navigate to=".." replace />;
  return <Outlet />;
}

export function UserSessionIssuersPage(): JSX.Element {
  return (
    <RequireScope scope={["org:read", "org:admin"]} level="page">
      <UserSessionIssuersOverview />
    </RequireScope>
  );
}

function UserSessionIssuersOverview(): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const detailHref = orgRoutes.userSessionIssuers.issuerDetail.href;
  const organizationTier = useIssuerTier("organization");
  const projectTier = useIssuerTier("project");
  const [consolidateSource, setConsolidateSource] =
    useState<UserSessionIssuer | null>(null);
  const [promoteTarget, setPromoteTarget] = useState<UserSessionIssuer | null>(
    null,
  );

  const pool = useMemo(
    () => [...organizationTier.items, ...projectTier.items],
    [organizationTier.items, projectTier.items],
  );

  const actions = {
    detailHref,
    onMakeOrganizationWide: setPromoteTarget,
    onConsolidate: setConsolidateSource,
  };

  return (
    <>
      <ResourceListPage
        title="MCP Authentication"
        description="Where people sign in to your MCP servers. Each one decides how sign-in works for every server that uses it."
      >
        <Stack gap={4} className="mb-6">
          <div>
            <Heading variant="h4" className="mb-2">
              Organization-wide
            </Heading>
            <Text muted small className="max-w-2xl">
              Every project can use these.
            </Text>
          </div>
          <IssuerTable
            tier={organizationTier}
            showProject={false}
            emptyHeading="No organization-wide MCP Authentication Providers"
            emptyDescription="Make a project's MCP Authentication Provider organization-wide to share it with every project."
            {...actions}
          />
        </Stack>

        <Stack gap={4} className="mb-6">
          <div>
            <Heading variant="h4" className="mb-2">
              Project-specific
            </Heading>
            <Text muted small className="max-w-2xl">
              Each serves the MCP servers of one project.
            </Text>
          </div>
          <IssuerTable
            tier={projectTier}
            showProject
            emptyHeading="No project-specific MCP Authentication Providers"
            emptyDescription="Projects get one when they add sign-in to an MCP server."
            {...actions}
          />
        </Stack>
      </ResourceListPage>

      {consolidateSource && (
        <ConsolidateIssuerDialog
          source={consolidateSource}
          candidates={consolidationTargets(consolidateSource, pool)}
          candidatesLoading={
            organizationTier.isLoading || projectTier.isLoading
          }
          onRetryCandidates={retryFailedTiers([organizationTier, projectTier])}
          onClose={() => setConsolidateSource(null)}
        />
      )}

      {promoteTarget && (
        <MakeOrganizationWideDialog
          issuer={promoteTarget}
          onClose={() => setPromoteTarget(null)}
        />
      )}
    </>
  );
}

export function IssuerTable({
  tier,
  showProject,
  scopeLabel,
  emptyHeading,
  emptyDescription,
  detailHref,
  onMakeOrganizationWide,
  onConsolidate,
}: {
  tier: IssuerTier;
  showProject: boolean;
  /** Adds a Scope column, for a list that mixes organization-wide and project ones. */
  scopeLabel?: (issuer: UserSessionIssuer) => string;
  emptyHeading: string;
  emptyDescription: string;
  detailHref: (issuerId: string) => string;
  onMakeOrganizationWide: (issuer: UserSessionIssuer) => void;
  onConsolidate: (issuer: UserSessionIssuer) => void;
}): JSX.Element {
  if (tier.isLoading && tier.items.length === 0) {
    return <SkeletonTable />;
  }
  if (tier.items.length === 0) {
    return tier.isError ? (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn't load MCP Authentication Providers"
        action={
          <Button variant="secondary" size="sm" onClick={tier.retry}>
            <Button.Text>Retry</Button.Text>
          </Button>
        }
      />
    ) : (
      <InlineEmptyState
        icon="key-round"
        heading={emptyHeading}
        description={emptyDescription}
      />
    );
  }

  const headers = [
    { label: "Name" },
    ...(showProject ? [{ label: "Project" }] : []),
    ...(scopeLabel ? [{ label: "Scope" }] : []),
    { label: "Mode" },
    { label: "" },
  ];

  return (
    <>
      <DotTable headers={headers}>
        {tier.items.map((issuer) => (
          <DotRow
            key={issuer.id}
            href={detailHref(issuer.id)}
            ariaLabel={`View MCP Authentication Provider ${issuer.slug}`}
          >
            <td className="px-3 py-3">
              <Text
                variant="subheading"
                as="div"
                className="group-hover:text-primary truncate text-sm transition-colors group-hover:underline"
              >
                {issuer.slug}
              </Text>
              {issuer.issuerUrl ? (
                <Text small muted as="div" className="truncate">
                  {issuer.issuerUrl}
                </Text>
              ) : null}
            </td>
            {showProject && (
              <td className="px-3 py-3">
                <Text small muted>
                  {issuer.projectName || "—"}
                </Text>
              </td>
            )}
            {scopeLabel && (
              <td className="px-3 py-3">
                <Text small muted>
                  {scopeLabel(issuer)}
                </Text>
              </td>
            )}
            <td className="px-3 py-3">
              <Text small muted>
                {authorizationServerModeLabel(issuer)}
              </Text>
            </td>
            <td className="px-3 py-3 text-right">
              <RowActions
                issuer={issuer}
                onMakeOrganizationWide={() => onMakeOrganizationWide(issuer)}
                onConsolidate={() => onConsolidate(issuer)}
              />
            </td>
          </DotRow>
        ))}
      </DotTable>
      {tier.isError ? (
        <Stack direction="horizontal" align="center" gap={3} className="py-2">
          <Text small className="text-destructive">
            Couldn&apos;t load every MCP Authentication Provider.
          </Text>
          <Button variant="tertiary" size="sm" onClick={tier.retry}>
            <Button.Text>Retry</Button.Text>
          </Button>
        </Stack>
      ) : null}
    </>
  );
}

function RowActions({
  issuer,
  onMakeOrganizationWide,
  onConsolidate,
}: {
  issuer: UserSessionIssuer;
  onMakeOrganizationWide: () => void;
  onConsolidate: () => void;
}): JSX.Element {
  return (
    <div className="relative z-20" onClick={(e) => e.stopPropagation()}>
      <RequireScope scope="org:admin" level="section">
        {/* Non-modal: making a row organization-wide moves it to the other
            table and unmounts this menu mid-close, which would leave a modal
            menu's body pointer lock stuck. */}
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger asChild>
            <Button
              variant="tertiary"
              size="sm"
              aria-label={`Actions for ${issuer.slug}`}
            >
              <Button.LeftIcon>
                <MoreHorizontal className="h-4 w-4" />
              </Button.LeftIcon>
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            {issuer.projectId !== "" && (
              <DropdownMenuItem onClick={onMakeOrganizationWide}>
                Make organization-wide
              </DropdownMenuItem>
            )}
            <DropdownMenuItem onClick={onConsolidate}>
              Consolidate into another
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </RequireScope>
    </div>
  );
}
