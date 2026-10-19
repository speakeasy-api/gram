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
import { DotRow } from "@/components/ui/DotRow";
import { DotTable } from "@/components/ui/DotTable";
import { Heading } from "@/components/ui/Heading";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { ScopeBadge } from "@/lib/remote-identity";
import { useOrgRoutes } from "@/routes";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useOrganizationUserSessionIssuer } from "@gram/client/react-query/organizationUserSessionIssuer.js";
import { useOrganizationUserSessionIssuerMcpServersInfinite } from "@gram/client/react-query/organizationUserSessionIssuerMcpServers.js";
import { useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router";
import { AuthenticationHostDialog } from "./AuthenticationHostDialog";
import { ConsolidateIssuerDialog } from "./ConsolidateIssuerDialog";
import {
  authorizationServerModeLabel,
  consolidationTargets,
  settingValue,
  unavailableReason,
} from "./issuerDisplay";
import { MakeOrganizationWideDialog } from "./MakeOrganizationWideDialog";
import { retryFailedTiers, useIssuerTier } from "./useIssuerTier";

export default function UserSessionIssuerDetail(): JSX.Element {
  // Gate the reads too, not only what renders.
  return (
    <RequireScope scope={["org:read", "org:admin"]} level="page">
      <UserSessionIssuerDetailContent />
    </RequireScope>
  );
}

function UserSessionIssuerDetailContent(): JSX.Element {
  const { issuerId = "" } = useParams<{ issuerId: string }>();
  const orgRoutes = useOrgRoutes();
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
              title: "Authorization server not found",
              description:
                "It may have been consolidated into another one or removed.",
              backTo: orgRoutes.userSessionIssuers.href(),
            }
          : query.isError && !issuer
            ? {
                title: "Couldn't load this authorization server",
                description: "Reload the page to try again.",
                backTo: orgRoutes.userSessionIssuers.href(),
              }
            : undefined
      }
      hero={issuer ? <IssuerHero issuer={issuer} /> : undefined}
      sections={
        issuer
          ? [
              {
                id: "sign-in",
                label: "Sign-in",
                content: <SignInSection issuer={issuer} />,
              },
              {
                id: "mcp-servers",
                label: "MCP servers",
                content: <McpServersSection issuer={issuer} />,
              },
              {
                id: "settings",
                label: "Settings",
                content: <SettingsSection issuer={issuer} />,
              },
              {
                id: "actions",
                label: "Actions",
                content: <ActionsSection issuer={issuer} />,
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
          Speakeasy authorization server
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

function SignInSection({ issuer }: { issuer: UserSessionIssuer }): JSX.Element {
  // The first section: the scroll layout leaves no room under the hero.
  return (
    <div className="pt-8">
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
          {issuer.projectName ? (
            <InfoField label="Project">
              <InfoText>{issuer.projectName}</InfoText>
            </InfoField>
          ) : null}
        </InfoFieldGrid>
        {issuer.authorizationServerMode === "shared" ? (
          issuer.issuerUrl ? (
            <Stack gap={4}>
              <LabeledValue label="Issuer URL" value={issuer.issuerUrl} />
              <LabeledValue
                label="Token endpoint"
                value={issuer.tokenEndpoint}
              />
            </Stack>
          ) : (
            <Text small className="text-destructive">
              {unavailableReason(issuer)}, so its MCP servers sign clients in
              through their own. Contact Speakeasy support to fix it.
            </Text>
          )
        ) : (
          <Text small muted>
            Each MCP server signs clients in at its own URL.
          </Text>
        )}
      </InfoSection>
    </div>
  );
}

function McpServersSection({
  issuer,
}: {
  issuer: UserSessionIssuer;
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
    body = (
      <Text small muted>
        Loading MCP servers…
      </Text>
    );
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
        heading="No MCP servers use this authorization server"
        description="Choose it in an MCP server's settings to have clients sign in here."
      />
    );
  } else {
    body = (
      <>
        <DotTable
          headers={[{ label: "Name" }, { label: "Project" }, { label: "Type" }]}
        >
          {servers.map((server) => (
            <DotRow key={server.id}>
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
                  {server.kind === "meta_mcp_server" ? "Gateway" : "MCP server"}
                </Text>
              </td>
            </DotRow>
          ))}
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

  return <InfoSection title="MCP servers">{body}</InfoSection>;
}

function SettingsSection({
  issuer,
}: {
  issuer: UserSessionIssuer;
}): JSX.Element {
  // Worded as the consolidation warnings word them, so the page and the
  // dialog describe a setting the same way.
  return (
    <InfoSection title="Settings">
      <InfoFieldGrid>
        <InfoField label="Session length">
          <InfoText>
            {settingValue(
              "session_duration_hours",
              String(issuer.sessionDurationHours),
            )}
          </InfoText>
        </InfoField>
        <InfoField label="Sign-in flow">
          <InfoText>
            {settingValue("authn_challenge_mode", issuer.authnChallengeMode)}
          </InfoText>
        </InfoField>
        <InfoField label="Client admission">
          <InfoText>
            {settingValue(
              "client_id_metadata_admission_mode",
              issuer.clientIdMetadataAdmissionMode,
            )}
          </InfoText>
        </InfoField>
      </InfoFieldGrid>
      <Text small muted>
        Change these in the settings of an MCP server that uses this
        authorization server.
      </Text>
    </InfoSection>
  );
}

function ActionsSection({
  issuer,
}: {
  issuer: UserSessionIssuer;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const navigate = useNavigate();
  const [promoting, setPromoting] = useState(false);
  const [movingHost, setMovingHost] = useState(false);
  const [consolidating, setConsolidating] = useState(false);

  return (
    <RequireScope scope="org:admin" level="section">
      <InfoSection title="Actions">
        <Stack direction="horizontal" gap={2}>
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
        {movingHost && (
          <AuthenticationHostDialog
            issuer={issuer}
            onClose={() => setMovingHost(false)}
          />
        )}
        {promoting && (
          <MakeOrganizationWideDialog
            issuer={issuer}
            onClose={() => setPromoting(false)}
          />
        )}
        {consolidating && (
          <ConsolidateFromDetail
            source={issuer}
            onClose={() => setConsolidating(false)}
            // The source is gone afterwards; show the one that took its place.
            onConsolidated={(target) => {
              void navigate(
                orgRoutes.userSessionIssuers.issuerDetail.href(target.id),
              );
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
