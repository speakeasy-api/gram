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
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { useRBAC } from "@/hooks/useRBAC";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { oktaViewHref } from "@/pages/org/identity-provider/tabs";
import { OKTA_SIGN_IN_SECTION_ID } from "@/pages/org/identity-provider/tabs/setup/OktaSignInSection";
import { useOrgRoutes } from "@/routes";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { MoreHorizontal } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Navigate, Outlet, useNavigate, useSearchParams } from "react-router";
import { ConsolidateIssuerDialog } from "./ConsolidateIssuerDialog";
import { CreateOrganizationSignInSheet } from "./CreateOrganizationSignInSheet";
import {
  authorizationServerModeLabel,
  consolidationTargets,
} from "./issuerDisplay";
import { retryFailedTiers, useIssuerTier } from "./useIssuerTier";

/** Route shell: the whole surface waits on its rollout flag. */
export function UserSessionIssuersRoot(): JSX.Element | null {
  const flag = useFeatureFlag(FEATURE_FLAGS.userSessionIssuerDashboard);
  // Render nothing until flags resolve rather than flashing a redirect.
  if (flag.status === "loading") return null;
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
  const organizationTier = useIssuerTier("organization");
  const projectTier = useIssuerTier("project");
  const [consolidateSource, setConsolidateSource] =
    useState<UserSessionIssuer | null>(null);
  const orgRoutes = useOrgRoutes();
  const navigate = useNavigate();
  const organization = useOrganization();
  const { hasScope, isLoading: rbacLoading } = useRBAC();
  const isAdmin = !rbacLoading && hasScope("org:admin", organization.id);

  // ?create=1 links straight into creating an organization-wide sign-in.
  const [searchParams, setSearchParams] = useSearchParams();
  const creating = isAdmin && searchParams.get("create") === "1";
  const setCreating = (open: boolean) =>
    setSearchParams(
      (params) => {
        const next = new URLSearchParams(params);
        if (open) next.set("create", "1");
        else next.delete("create");
        return next;
      },
      { replace: true },
    );
  // Someone who can't create shouldn't keep a link that suggests they can.
  const strayCreate =
    !rbacLoading && !isAdmin && searchParams.get("create") === "1";
  useEffect(() => {
    if (!strayCreate) return;
    setSearchParams(
      (params) => {
        const next = new URLSearchParams(params);
        next.delete("create");
        return next;
      },
      { replace: true },
    );
  }, [strayCreate, setSearchParams]);

  const pool = useMemo(
    () => [...organizationTier.items, ...projectTier.items],
    [organizationTier.items, projectTier.items],
  );

  const actions = { onConsolidate: setConsolidateSource };

  return (
    <>
      <ResourceListPage
        title="Speakeasy sign-ins"
        description="Where people sign in to your MCP servers. Each one decides how sign-in works for every server that uses it."
        primaryAction={
          isAdmin ? (
            <Button onClick={() => setCreating(true)}>
              <Button.Text>Create organization-wide sign-in</Button.Text>
            </Button>
          ) : undefined
        }
      >
        <Stack gap={4} className="mb-6">
          <div>
            <Heading variant="h4" className="mb-2">
              Organization-wide sign-ins
            </Heading>
            <Text muted small className="max-w-2xl">
              Every project can use these.
            </Text>
          </div>
          <IssuerTable
            tier={organizationTier}
            showProject={false}
            emptyHeading="No organization-wide sign-ins yet"
            emptyDescription="Create one to share a sign-in across projects. Enterprise Managed Authorization needs one."
            {...actions}
          />
        </Stack>

        <Stack gap={4} className="mb-6">
          <div>
            <Heading variant="h4" className="mb-2">
              Project sign-ins
            </Heading>
            <Text muted small className="max-w-2xl">
              Each serves the MCP servers of one project.
            </Text>
          </div>
          <IssuerTable
            tier={projectTier}
            showProject
            emptyHeading="No project sign-ins"
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

      {isAdmin && (
        <CreateOrganizationSignInSheet
          open={creating}
          onOpenChange={setCreating}
          organizationTier={organizationTier}
          onCreated={(issuer) =>
            void navigate(
              orgRoutes.userSessionIssuers.issuerDetail.href(issuer.id),
            )
          }
          oktaSetupHref={`${orgRoutes.identity.href()}${oktaViewHref("setup", OKTA_SIGN_IN_SECTION_ID)}`}
        />
      )}
    </>
  );
}

function IssuerTable({
  tier,
  showProject,
  emptyHeading,
  emptyDescription,
  onConsolidate,
}: {
  tier: ReturnType<typeof useIssuerTier>;
  showProject: boolean;
  emptyHeading: string;
  emptyDescription: string;
  onConsolidate: (issuer: UserSessionIssuer) => void;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();

  if (tier.isLoading && tier.items.length === 0) {
    return <SkeletonTable />;
  }
  if (tier.items.length === 0) {
    return tier.isError ? (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn't load sign-ins"
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
    { label: "Mode" },
    { label: "" },
  ];

  return (
    <>
      <DotTable headers={headers}>
        {tier.items.map((issuer) => (
          <DotRow
            key={issuer.id}
            href={orgRoutes.userSessionIssuers.issuerDetail.href(issuer.id)}
            ariaLabel={`View sign-in ${issuer.slug}`}
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
            <td className="px-3 py-3">
              <Text small muted>
                {authorizationServerModeLabel(issuer)}
              </Text>
            </td>
            <td className="px-3 py-3 text-right">
              <RowActions
                issuer={issuer}
                onConsolidate={() => onConsolidate(issuer)}
              />
            </td>
          </DotRow>
        ))}
      </DotTable>
      {tier.isError ? (
        <Stack direction="horizontal" align="center" gap={3} className="py-2">
          <Text small className="text-destructive">
            Couldn&apos;t load every sign-in.
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
  onConsolidate,
}: {
  issuer: UserSessionIssuer;
  onConsolidate: () => void;
}): JSX.Element {
  return (
    <div className="relative z-20" onClick={(e) => e.stopPropagation()}>
      <RequireScope scope="org:admin" level="section">
        {/* Non-modal: consolidating removes the row and unmounts this menu
            mid-close, which would leave a modal menu's body pointer lock
            stuck. */}
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
            <DropdownMenuItem onClick={onConsolidate}>
              Consolidate into another
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </RequireScope>
    </div>
  );
}
