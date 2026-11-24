import { DetailHero } from "@/components/detail-hero";
import {
  InfoField,
  InfoFieldGrid,
  InfoSection,
  InfoText,
} from "@/components/detail-fields";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { DetailPage } from "@/components/page-templates";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { LabeledValue } from "@/components/setup-steps/StepBlocks";
import { Button } from "@/components/ui/Button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { DotRow } from "@/components/ui/DotRow";
import { DotTable } from "@/components/ui/DotTable";
import { Heading } from "@/components/ui/Heading";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { ScopeBadge } from "@/lib/remote-identity";
import { mcpServerRouteParam } from "@/lib/sources";
import { cn } from "@/lib/utils";
import { ChevronRight } from "lucide-react";
import { useOrgRoutes, useRoutes } from "@/routes";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import type { UserSessionIssuerMcpServer } from "@gram/client/models/components/usersessionissuermcpserver.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useOrganizationUserSessionIssuer } from "@gram/client/react-query/organizationUserSessionIssuer.js";
import { useOrganizationUserSessionIssuerMcpServersInfinite } from "@gram/client/react-query/organizationUserSessionIssuerMcpServers.js";
import { useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router";
import { AuthenticationHostDialog } from "./AuthenticationHostDialog";
import { ConsolidateIssuerDialog } from "./ConsolidateIssuerDialog";
import { EnterpriseManagedAuthSection } from "./EnterpriseManagedAuthSection";
import { MakeOrganizationWideDialog } from "./MakeOrganizationWideDialog";
import { ClientAccessSection } from "./ClientAccessSection";
import { ClientDriftWarning } from "./ClientDriftWarning";
import { SessionLengthSection } from "./SessionLengthSection";
import {
  authorizationServerModeLabel,
  consolidationTargets,
  settingValue,
  unavailableReason,
} from "./issuerDisplay";
import { retryFailedTiers, useIssuerTier } from "./useIssuerTier";
import { SkeletonTable } from "@/components/ui/SkeletonTable";

export default function UserSessionIssuerDetail(): JSX.Element {
  const orgRoutes = useOrgRoutes();
  // Gate the reads too, not only what renders.
  return (
    <RequireScope scope={["org:read", "org:admin"]} level="page">
      <IssuerDetailContent
        listHref={orgRoutes.userSessionIssuers.href()}
        detailHref={orgRoutes.userSessionIssuers.issuerDetail.href}
      />
    </RequireScope>
  );
}

type IssuerDetailLinks = {
  /** Where "not found" sends people back to. */
  listHref: string;
  /** Another MCP sign-in's page, after consolidating into it. */
  detailHref: (issuerId: string) => string;
};

/**
 * One MCP sign-in, shared by the organization page and a
 * project's MCP Sign-in configuration. `editableProjectId` names the project
 * whose own providers this page may change; organization-wide ones are always
 * changed through the organization.
 */
function IssuerDetailContent({
  listHref,
  detailHref,
  editableProjectId,
}: IssuerDetailLinks & {
  editableProjectId?: string;
}): JSX.Element {
  const { issuerId = "" } = useParams<{ issuerId: string }>();
  const query = useOrganizationUserSessionIssuer({ id: issuerId }, undefined, {
    throwOnError: false,
    retry: false,
  });
  const issuer = query.data;
  // A removed authorization server replaces the page even after it loaded;
  // any other failed refresh keeps the page it already has.
  const missing =
    query.error instanceof GramError && query.error.statusCode === 404;

  return (
    <DetailPage
      breadcrumbSubstitutions={{ [issuerId]: issuer?.slug }}
      layout="scroll"
      loading={query.isPending}
      notFound={
        missing
          ? {
              title: "MCP sign-in not found",
              description:
                "It may have been consolidated into another one or removed.",
              backTo: listHref,
            }
          : query.isError && !issuer
            ? {
                title: "Couldn't load this MCP sign-in",
                description: "Reload the page to try again.",
                backTo: listHref,
              }
            : undefined
      }
      hero={issuer ? <IssuerHero issuer={issuer} /> : undefined}
      sections={
        issuer
          ? [
              {
                id: "session-length",
                label: "Session length",
                content: (
                  // The first section: the scroll layout leaves no room
                  // under the hero.
                  <div className="pt-8">
                    <SessionLengthSection
                      issuer={issuer}
                      editableProjectId={editableProjectId}
                    />
                  </div>
                ),
              },
              {
                id: "client-access",
                label: "Client access",
                content: (
                  <ClientAccessSection
                    issuer={issuer}
                    editableProjectId={editableProjectId}
                  />
                ),
              },
              {
                id: "enterprise-managed-auth",
                label: "Enterprise Managed Auth",
                content: <EnterpriseManagedAuthSection issuer={issuer} />,
              },
              {
                id: "mcp-servers",
                label: "MCP servers",
                content: <McpServersSection issuer={issuer} />,
              },
              {
                id: "advanced",
                label: "Advanced",
                content: <AdvancedSection issuer={issuer} />,
              },
              {
                id: "actions",
                label: "Actions",
                content: (
                  <ActionsSection issuer={issuer} detailHref={detailHref} />
                ),
              },
            ]
          : []
      }
    />
  );
}

function IssuerHero({ issuer }: { issuer: UserSessionIssuer }): JSX.Element {
  return (
    <DetailHero>
      <Page.Eyebrow />
      <div className="flex items-center gap-3">
        <Text small muted>
          MCP sign-in
        </Text>
        <ScopeBadge
          projectId={issuer.projectId}
          organizationId={issuer.organizationId}
        />
      </div>
      <Heading variant="h1" className="break-all normal-case">
        {issuer.slug}
      </Heading>
    </DetailHero>
  );
}

/**
 * How clients reach this MCP sign-in to sign in. Rarely needed, so it stays
 * closed; only a broken sign-in address shows without opening it.
 */
export function AdvancedSection({
  issuer,
}: {
  issuer: UserSessionIssuer;
}): JSX.Element {
  const [open, setOpen] = useState(false);
  const shared = issuer.authorizationServerMode === "shared";
  const broken = shared && !issuer.issuerUrl;
  return (
    <Stack gap={4}>
      {broken ? (
        <Text small className="text-destructive">
          {unavailableReason(issuer)}, so its MCP servers sign clients in
          through their own. Contact Speakeasy support to fix it.
        </Text>
      ) : null}
      <Collapsible open={open} onOpenChange={setOpen}>
        <CollapsibleTrigger asChild>
          <Button variant="tertiary" size="sm">
            <Button.LeftIcon>
              <ChevronRight
                aria-hidden="true"
                className={cn(
                  "size-4 transition-transform",
                  open && "rotate-90",
                )}
              />
            </Button.LeftIcon>
            <Button.Text>Advanced</Button.Text>
          </Button>
        </CollapsibleTrigger>
        <CollapsibleContent className="pt-4">
          <InfoSection title="Sign-in">
            <InfoFieldGrid>
              <InfoField label="Mode">
                <InfoText>{authorizationServerModeLabel(issuer)}</InfoText>
              </InfoField>
              <InfoField label="Authentication host">
                <InfoText>
                  {issuer.useAuthenticationHost ? "Opted in" : "Not opted in"}
                </InfoText>
              </InfoField>
              <InfoField label="Sign-in flow">
                <InfoText>
                  {settingValue(
                    "authn_challenge_mode",
                    issuer.authnChallengeMode,
                  )}
                </InfoText>
              </InfoField>
              {issuer.projectName ? (
                <InfoField label="Project">
                  <InfoText>{issuer.projectName}</InfoText>
                </InfoField>
              ) : null}
            </InfoFieldGrid>
            {shared ? (
              issuer.issuerUrl ? (
                <Stack gap={4}>
                  <LabeledValue label="Issuer URL" value={issuer.issuerUrl} />
                  <LabeledValue
                    label="Token endpoint"
                    value={issuer.tokenEndpoint}
                  />
                </Stack>
              ) : null
            ) : (
              <Text small muted>
                Each MCP server signs clients in at its own URL.
              </Text>
            )}
          </InfoSection>
        </CollapsibleContent>
      </Collapsible>
    </Stack>
  );
}

export function McpServersSection({
  issuer,
  titled = true,
  linkRows = false,
}: {
  issuer: UserSessionIssuer;
  /** Off where a tab already names the list. */
  titled?: boolean;
  /** Each row opens its server's Clients and Sessions tab. */
  linkRows?: boolean;
}): JSX.Element {
  const query = useOrganizationUserSessionIssuerMcpServersInfinite(
    { userSessionIssuerId: issuer.id },
    undefined,
    { throwOnError: false },
  );
  const servers = useMemo(
    () => query.data?.pages.flatMap((page) => page.result.items) ?? [],
    [query.data],
  );

  let body: JSX.Element;
  if (query.isPending) {
    body = <SkeletonTable label="Loading MCP servers" />;
  } else if (query.isError && servers.length === 0) {
    body = (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn't load MCP servers"
        action={
          <Button
            variant="secondary"
            size="sm"
            onClick={() => void query.refetch()}
          >
            <Button.Text>Retry</Button.Text>
          </Button>
        }
      />
    );
  } else if (servers.length === 0) {
    body = (
      <InlineEmptyState
        icon="server"
        heading="No MCP servers use this MCP sign-in"
        description="Choose it in an MCP server's settings to have clients sign in here."
      />
    );
  } else {
    body = (
      <>
        <DotTable
          headers={[{ label: "Name" }, { label: "Project" }, { label: "Type" }]}
        >
          {servers.map((server) =>
            linkRows ? (
              <LinkedMcpServerRow key={server.id} server={server} />
            ) : (
              <McpServerRow key={server.id} server={server} />
            ),
          )}
        </DotTable>
        {query.hasNextPage ? (
          <Stack direction="horizontal" justify="center" className="py-3">
            <Button
              variant="tertiary"
              size="sm"
              disabled={query.isFetchingNextPage}
              onClick={() => void query.fetchNextPage()}
            >
              <Button.Text>
                {query.isFetchingNextPage ? "Loading…" : "Load more"}
              </Button.Text>
            </Button>
          </Stack>
        ) : null}
      </>
    );
  }

  const drift = <ClientDriftWarning issuerId={issuer.id} />;
  if (!titled) {
    return (
      <div className="flex flex-col gap-2">
        <div className="flex justify-end">{drift}</div>
        {body}
      </div>
    );
  }
  return (
    <InfoSection title="MCP servers" adornment={drift}>
      {body}
    </InfoSection>
  );
}

function McpServerRow({
  server,
  href,
}: {
  server: UserSessionIssuerMcpServer;
  href?: string;
}): JSX.Element {
  const gateway = server.kind === "meta_mcp_server";
  return (
    <DotRow
      href={href}
      ariaLabel={
        href
          ? `Open ${gateway ? "gateway" : "MCP server"} ${server.name}`
          : undefined
      }
    >
      <td className="px-3 py-3">
        <Text small as="div" className="truncate">
          {server.name}
        </Text>
      </td>
      <td className="px-3 py-3">
        <Text small muted>
          {server.projectName}
        </Text>
      </td>
      <td className="px-3 py-3">
        <Text small muted>
          {gateway ? "Gateway" : "MCP server"}
        </Text>
      </td>
    </DotRow>
  );
}

/** Links through the server's own project, which need not be this one. */
function LinkedMcpServerRow({
  server,
}: {
  server: UserSessionIssuerMcpServer;
}): JSX.Element {
  const organization = useOrganization();
  const projectSlug = organization.projects.find(
    (project) => project.id === server.projectId,
  )?.slug;
  const routes = useRoutes({ projectSlug });
  return (
    <McpServerRow
      server={server}
      href={projectSlug ? mcpServerSessionsHref(routes, server) : undefined}
    />
  );
}

// A toolset's slug resolves on the server page too (ToolsetRedirectPage).
function mcpServerSessionsHref(
  routes: ReturnType<typeof useRoutes>,
  server: UserSessionIssuerMcpServer,
): string {
  return server.kind === "meta_mcp_server"
    ? routes.mcp.gateway.sessions.href(server.id)
    : routes.mcp.details.sessions.href(mcpServerRouteParam(server));
}

export function ActionsSection({
  issuer,
  detailHref,
}: {
  issuer: UserSessionIssuer;
  detailHref: (issuerId: string) => string;
}): JSX.Element {
  const navigate = useNavigate();
  const [movingHost, setMovingHost] = useState(false);
  const [consolidating, setConsolidating] = useState(false);
  const [promoting, setPromoting] = useState(false);

  return (
    <RequireScope scope="org:admin" level="section">
      <InfoSection title="Actions">
        <Stack direction="horizontal" gap={2} wrap="wrap">
          {issuer.projectId !== "" && (
            <Button
              variant="secondary"
              size="sm"
              onClick={() => setPromoting(true)}
            >
              <Button.Text>Make organization-wide</Button.Text>
            </Button>
          )}
          <Button
            variant="secondary"
            size="sm"
            onClick={() => setConsolidating(true)}
          >
            <Button.Text>Consolidate into another</Button.Text>
          </Button>
          {issuer.authenticationHostAvailable && (
            <Button
              variant="secondary"
              size="sm"
              onClick={() => setMovingHost(true)}
            >
              <Button.Text>
                {issuer.useAuthenticationHost
                  ? "Move off the authentication host"
                  : "Move to the authentication host"}
              </Button.Text>
            </Button>
          )}
        </Stack>
        {promoting && (
          <MakeOrganizationWideDialog
            issuer={issuer}
            onClose={() => setPromoting(false)}
          />
        )}
        {movingHost && (
          <AuthenticationHostDialog
            issuer={issuer}
            onClose={() => setMovingHost(false)}
          />
        )}
        {consolidating && (
          <ConsolidateFromDetail
            source={issuer}
            onClose={() => setConsolidating(false)}
            // The source is gone afterwards; show the one that took its place.
            onConsolidated={(target) => {
              void navigate(detailHref(target.id));
            }}
          />
        )}
      </InfoSection>
    </RequireScope>
  );
}

/** Loads every candidate to offer, so none is missed. */
function ConsolidateFromDetail({
  source,
  onClose,
  onConsolidated,
}: {
  source: UserSessionIssuer;
  onClose: () => void;
  onConsolidated: (target: UserSessionIssuer) => void;
}): JSX.Element {
  const organizationTier = useIssuerTier("organization");
  const projectTier = useIssuerTier("project");

  return (
    <ConsolidateIssuerDialog
      source={source}
      candidates={consolidationTargets(source, [
        ...organizationTier.items,
        ...projectTier.items,
      ])}
      candidatesLoading={organizationTier.isLoading || projectTier.isLoading}
      onRetryCandidates={retryFailedTiers([organizationTier, projectTier])}
      onClose={onClose}
      onConsolidated={onConsolidated}
    />
  );
}
