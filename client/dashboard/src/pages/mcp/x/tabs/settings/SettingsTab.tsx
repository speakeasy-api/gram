import { useScrollToSettingsHash } from "@/hooks/useScrollToSettingsHash";
import { ReadinessIssueFrame } from "@/components/detail/readiness-issues";
import { SettingsLayout } from "@/components/detail/settings-layout";
import {
  SETTINGS_ENVIRONMENT_VARIABLES_ID,
  SETTINGS_GROUP_IDS,
  SETTINGS_GROUPS,
} from "@/components/detail/settings-groups";
import { useRBAC } from "@/hooks/useRBAC";
import {
  AuthenticationSection,
  MCP_AUTHENTICATION_SECTION_ID,
} from "./sections/authentication/AuthenticationSection";
import { SetupStepBanner } from "./sections/authentication/SetupStepBanner";
import {
  MCP_SERVER_URL_SECTION_ID,
  ServerUrlSection,
} from "./sections/ServerUrlSection";

import { SettingsSection } from "@/components/detail/settings-section";
import { FieldError } from "@/components/ui/Field";
import { Text } from "@/components/ui/Text";
import {
  type UpstreamAuthEnforcementStatus,
  useUpstreamAuthEnforcement,
} from "@/hooks/useUpstreamAuthEnforcement";
import { useResolvedMcpServerUrl } from "@/hooks/useToolsetUrl";
import type { Toolset } from "@/lib/toolTypes";
import {
  MCPAuthenticationTab,
  ToolsetOAuthSection,
} from "@/pages/mcp/MCPEnvironmentSettings";
import { MCPPerformanceTab } from "@/pages/mcp/MCPPerformanceTab";
import { ExportJsonSection } from "@/pages/mcp/x/hosted/ExportJsonSection";
import { ServerInstructionsSection } from "@/pages/mcp/x/hosted/ServerInstructionsSection";
import { isCanonicalHostedWrapper } from "@/pages/mcp/x/MCPServerDetailsRouting";
import { isOnOffServer } from "@/pages/mcp/x/mcpServerVisibility";
import { PluginStatusBanner } from "@/pages/mcp/overview/PluginStatusBanner";
import { pluginServerRef } from "@/pages/mcp/overview/serverMatchesRef";
import { usePluginWriteAccess } from "@/hooks/usePluginWriteAccess";
import { getTunneledMcpServerArgs } from "@/lib/sources";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useGetRemoteMcpServer } from "@gram/client/react-query/getRemoteMcpServer.js";
import { useGetTunneledMcpServer } from "@gram/client/react-query/getTunneledMcpServer.js";
import { useGetUnproxiedMcpServer } from "@gram/client/react-query/getUnproxiedMcpServer.js";
import { Fragment } from "react";
import {
  AgentSetupSection,
  MCP_AGENT_SETUP_SECTION_ID,
} from "./sections/AgentSetupSection";
import { CallerIdentitySection } from "./sections/CallerIdentitySection";
import {
  IDENTITY_CHAINING_SECTION_ID,
  IdentityChainingSection,
} from "./sections/authentication/IdentityChainingSection";
import { GeneralSection } from "./sections/GeneralSection";
import {
  UPSTREAM_CREDENTIALS_SECTION_ID,
  UpstreamCredentialsSection,
} from "./sections/authentication/UpstreamCredentialsSection";
import { DangerZoneSection } from "./sections/DangerZoneSection";
import { NetworkAccessSection } from "./sections/NetworkAccessSection";
import { MCP_PUBLIC_ACCESS_SECTION_ID } from "./sections/PublicAccessSection";
import { PublicRateLimitsSection } from "./sections/PublicRateLimitsSection";
import {
  MCP_RESOURCE_IDENTIFIER_SECTION_ID,
  ResourceIdentifierSection,
} from "./sections/ResourceIdentifierSection";
import type { SourceBackedDeleteTarget } from "./sections/sourceDelete";
import {
  MCP_SESSIONS_SECTION_ID,
  RemoteMcpSessionsSection,
} from "./sections/RemoteMcpSessionsSection";
import { ToolFilteringSection } from "./sections/ToolFilteringSection";
import {
  AvailabilitySection,
  MCP_VISIBILITY_SECTION_ID,
  VisibilitySection,
} from "./sections/VisibilitySection";
import {
  MCP_TUNNEL_KEY_SECTION_ID,
  TunnelKeySection,
} from "./sections/TunnelKeySection";

// Every section that can be deep-linked from elsewhere in the dashboard
// (readiness bar, visibility picker, rate-limit hint, connections panel).
const MCP_PERFORMANCE_SECTION_ID = "performance";
const MCP_ENVIRONMENT_SECTION_ID = "environment";

const SCROLLABLE_SECTION_IDS: readonly string[] = [
  MCP_SERVER_URL_SECTION_ID,
  MCP_PERFORMANCE_SECTION_ID,
  MCP_ENVIRONMENT_SECTION_ID,
  SETTINGS_ENVIRONMENT_VARIABLES_ID,
  MCP_AUTHENTICATION_SECTION_ID,
  MCP_RESOURCE_IDENTIFIER_SECTION_ID,
  MCP_PUBLIC_ACCESS_SECTION_ID,
  MCP_TUNNEL_KEY_SECTION_ID,
  MCP_AGENT_SETUP_SECTION_ID,
  IDENTITY_CHAINING_SECTION_ID,
  MCP_SESSIONS_SECTION_ID,
  UPSTREAM_CREDENTIALS_SECTION_ID,
  MCP_VISIBILITY_SECTION_ID,
  ...SETTINGS_GROUP_IDS,
];

export function SettingsTab({
  mcpServer,
  endpoints,
  isLoadingEndpoints,
  hostedToolset,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
  /** The toolset a hosted server serves; it adds the hosted-only sections. */
  hostedToolset?: Toolset;
}): JSX.Element {
  const isUnproxied = !!mcpServer.unproxiedMcpServerId;
  const onOff = isOnOffServer(mcpServer);
  // Only remote and tunneled sources add sections to the page; the unproxied
  // row feeds the danger zone alone, so a deep link never waits on it.
  const hasSourceSections =
    !!mcpServer.remoteMcpServerId || !!mcpServer.tunneledMcpServerId;
  const remoteMcpServerId = mcpServer.remoteMcpServerId;
  const { mcpUrl } = useResolvedMcpServerUrl(endpoints, isLoadingEndpoints);
  // Enforcing organizations configure upstream identity through credentials;
  // the others keep the legacy Identity panel. Only one renders.
  const upstreamAuth = useUpstreamAuthEnforcement();
  const { hasScope } = useRBAC();
  const canWritePlugins = usePluginWriteAccess();
  const canWriteToolset =
    !!hostedToolset && hasScope("mcp:write", hostedToolset.id);

  // The source rows behind this server. Each section edits the source
  // directly, so they are fetched once here rather than per section.
  const remoteQuery = useGetRemoteMcpServer(
    { id: mcpServer.remoteMcpServerId ?? "" },
    undefined,
    { enabled: !!mcpServer.remoteMcpServerId },
  );
  const tunneledQuery = useGetTunneledMcpServer(
    getTunneledMcpServerArgs(mcpServer.tunneledMcpServerId ?? ""),
    undefined,
    { enabled: !!mcpServer.tunneledMcpServerId },
  );
  const unproxiedQuery = useGetUnproxiedMcpServer(
    { id: mcpServer.unproxiedMcpServerId ?? "" },
    undefined,
    { enabled: isUnproxied },
  );
  const remoteMcpServer = remoteQuery.data;
  const tunneledMcpServer = tunneledQuery.data;
  const unproxiedMcpServer = unproxiedQuery.data;
  const sourceUnavailable =
    remoteQuery.isError || tunneledQuery.isError || unproxiedQuery.isError;
  const sourceSettled =
    !hasSourceSections ||
    remoteQuery.isError ||
    tunneledQuery.isError ||
    !!remoteMcpServer ||
    !!tunneledMcpServer;

  // `ready` flips once the source-backed sections can be on the page: they
  // mount after the source row loads, so a cold-load deep link would otherwise
  // look for an element that isn't there yet.
  useScrollToSettingsHash(
    SCROLLABLE_SECTION_IDS,
    sourceSettled && upstreamAuth !== "loading",
  );

  let deleteTarget: SourceBackedDeleteTarget | undefined;
  if (remoteMcpServer) {
    deleteTarget = { kind: "remote", source: remoteMcpServer };
  } else if (tunneledMcpServer) {
    deleteTarget = { kind: "tunneled", source: tunneledMcpServer };
  } else if (unproxiedMcpServer) {
    deleteTarget = { kind: "unproxied", source: unproxiedMcpServer };
  }

  const G = SETTINGS_GROUPS;
  // The same plugin picker as the header's plugin card, as a settings
  // section; only people who can edit plugins get it.
  const publishing = canWritePlugins && {
    ...G.publishing,
    content: (
      <PluginStatusBanner
        variant="settings"
        server={pluginServerRef(mcpServer, hostedToolset)}
      />
    ),
  };
  const danger = (
    <DangerZoneSection
      mcpServer={mcpServer}
      endpoints={endpoints}
      deleteTarget={deleteTarget}
      sourceUnavailable={sourceUnavailable}
      hostedToolset={
        hostedToolset && isCanonicalHostedWrapper(mcpServer)
          ? hostedToolset
          : undefined
      }
    />
  );

  if (hostedToolset) {
    // Toolset columns sync onto the canonical server only, and only people
    // who can edit the toolset see them.
    const canonical = isCanonicalHostedWrapper(mcpServer);
    const toolsetSections = canonical && canWriteToolset;
    return (
      <SettingsLayout
        groups={[
          {
            ...G.general,
            content: (
              <>
                <AvailabilitySection mcpServer={mcpServer} />
                <GeneralSection mcpServer={mcpServer} />
              </>
            ),
          },
          {
            ...G.access,
            content: <VisibilitySection mcpServer={mcpServer} />,
          },
          publishing,
          canonical && !toolsetSections
            ? {
                // The header's readiness links land here too, so the anchors
                // stay for people who can't edit them.
                ...G.identity,
                content: (
                  <div
                    id={SETTINGS_ENVIRONMENT_VARIABLES_ID}
                    className="scroll-mt-8"
                  >
                    <div id={MCP_AUTHENTICATION_SECTION_ID}>
                      <Text muted small className="block max-w-2xl">
                        Only people who can edit this server&apos;s tools can
                        see or change its sign-in and environment variables.
                      </Text>
                    </div>
                  </div>
                ),
              }
            : canonical
              ? toolsetSections && {
                  ...G.identity,
                  content: (
                    <>
                      <div id={MCP_AUTHENTICATION_SECTION_ID}>
                        <ReadinessIssueFrame
                          anchorId={MCP_AUTHENTICATION_SECTION_ID}
                        >
                          <ToolsetOAuthSection toolset={hostedToolset} />
                        </ReadinessIssueFrame>
                      </div>
                      <div id={MCP_ENVIRONMENT_SECTION_ID}>
                        {/* A stable anchor for the readiness checklist. */}
                        <div
                          id={SETTINGS_ENVIRONMENT_VARIABLES_ID}
                          className="scroll-mt-8"
                        >
                          <ReadinessIssueFrame
                            anchorId={SETTINGS_ENVIRONMENT_VARIABLES_ID}
                          >
                            <MCPAuthenticationTab
                              toolset={hostedToolset}
                              showOAuth={false}
                            />
                          </ReadinessIssueFrame>
                        </div>
                      </div>
                    </>
                  ),
                }
              : {
                  ...G.identity,
                  content: (
                    <AuthenticationSection
                      mcpServer={mcpServer}
                      mcpUrl={mcpUrl}
                    />
                  ),
                },
          (!canonical || toolsetSections) && {
            ...G.connection,
            content: (
              <>
                <ServerUrlSection
                  backend={{ mcpServerId: mcpServer.id }}
                  endpoints={endpoints}
                  isLoadingEndpoints={isLoadingEndpoints}
                  mcpServer={mcpServer}
                />
                {canonical ? (
                  <NetworkAccessSection
                    mcpServer={mcpServer}
                    endpoints={endpoints}
                    toolset={hostedToolset}
                  />
                ) : (
                  <NetworkAccessSection
                    mcpServer={mcpServer}
                    endpoints={endpoints}
                  />
                )}
              </>
            ),
          },
          {
            ...G.tools,
            content: (
              <>
                {canWriteToolset ? (
                  <div id={MCP_PERFORMANCE_SECTION_ID}>
                    <MCPPerformanceTab toolset={hostedToolset} />
                  </div>
                ) : null}
                <ToolFilteringSection mcpServer={mcpServer} />
                {/* Export reads the toolset's canonical server only. */}
                {canonical && <ExportJsonSection mcpServer={mcpServer} />}
              </>
            ),
          },
          {
            ...G.instructions,
            content: (
              <SettingsSection>
                <SettingsSection.Header>
                  <SettingsSection.Title>
                    Server instructions
                  </SettingsSection.Title>
                  <SettingsSection.Description>
                    Returned to agents when they connect. Describe how your
                    tools work together, required workflows, and any
                    constraints.
                  </SettingsSection.Description>
                </SettingsSection.Header>
                <ServerInstructionsSection
                  mcpServerId={mcpServer.id}
                  writeResourceId={hostedToolset.id}
                  name={mcpServer.name || hostedToolset.name}
                  tools={hostedToolset.tools}
                />
              </SettingsSection>
            ),
          },
          { ...G.danger, content: danger },
        ]}
      />
    );
  }

  if (remoteMcpServerId) {
    return (
      <SettingsLayout
        groups={[
          // Held until the source settles: arriving later would remount the
          // form around its Remote URL and drop anything already typed.
          (remoteMcpServer || remoteQuery.isError) && {
            ...G.general,
            content: (
              <>
                {/* Remote servers only switch on and off, so there is no
                    Access area. */}
                <AvailabilityAnchor>
                  <AvailabilitySection mcpServer={mcpServer} />
                </AvailabilityAnchor>
                <GeneralSection
                  mcpServer={mcpServer}
                  remoteMcpServer={remoteMcpServer}
                />
              </>
            ),
          },
          publishing,
          {
            ...G.identity,
            // Upstream headers live inside the Identity panel's Custom
            // Headers disclosure; the identity choice governs them.
            content: (
              <>
                {upstreamAuth === "legacy" ? (
                  <AuthenticationSection
                    mcpServer={mcpServer}
                    mcpUrl={mcpUrl}
                  />
                ) : upstreamAuth === "enforced" ? (
                  // Links to the legacy Identity anchor land on its replacement.
                  <div id={MCP_AUTHENTICATION_SECTION_ID} className="space-y-4">
                    <ReadinessIssueFrame
                      anchorId={MCP_AUTHENTICATION_SECTION_ID}
                    >
                      <SetupStepBanner mcpServer={mcpServer} />
                      <UpstreamCredentialsSection mcpServer={mcpServer} />
                    </ReadinessIssueFrame>
                  </div>
                ) : (
                  <UpstreamIdentityPendingSection
                    id={MCP_AUTHENTICATION_SECTION_ID}
                    status={upstreamAuth}
                  />
                )}
                <IdentityChainingSection mcpServer={mcpServer} />
              </>
            ),
          },
          {
            ...G.connection,
            content: (
              <>
                <ServerUrlSection
                  backend={{ mcpServerId: mcpServer.id }}
                  endpoints={endpoints}
                  isLoadingEndpoints={isLoadingEndpoints}
                  mcpServer={mcpServer}
                />
                <NetworkAccessSection
                  mcpServer={mcpServer}
                  endpoints={endpoints}
                />
                <RemoteMcpSessionsSection
                  mcpServer={mcpServer}
                  mcpUrl={mcpUrl}
                />
              </>
            ),
          },
          {
            ...G.tools,
            content: <ToolFilteringSection mcpServer={mcpServer} />,
          },
          { ...G.danger, content: danger },
        ]}
      />
    );
  }

  // The source-backed sections are keyed by the source row so their drafts
  // remount when the route moves to a server backed by a different source;
  // this tree stays mounted across that navigation, and a draft or late save
  // result from the previous source must not land on the next one.
  return (
    <SettingsLayout
      groups={[
        {
          ...G.general,
          content: (
            <>
              {/* Every server leads with on or off. Tunneled servers also
                  pick who may connect, which is Access; the rest have no
                  Access area, so its old links land here. */}
              {onOff ? (
                <AvailabilityAnchor>
                  <AvailabilitySection mcpServer={mcpServer} />
                </AvailabilityAnchor>
              ) : (
                <AvailabilitySection mcpServer={mcpServer} />
              )}
              <GeneralSection mcpServer={mcpServer} />
            </>
          ),
        },
        !onOff && {
          ...G.access,
          content: (
            <>
              <VisibilitySection mcpServer={mcpServer} />
              {tunneledMcpServer ? (
                <PublicRateLimitsSection
                  key={tunneledMcpServer.id}
                  tunneledMcpServerId={tunneledMcpServer.id}
                  projectId={mcpServer.projectId}
                />
              ) : null}
            </>
          ),
        },
        publishing,
        {
          ...G.identity,
          content: (
            <>
              <AuthenticationSection mcpServer={mcpServer} mcpUrl={mcpUrl} />
              {/* Hosted toolsets keep their own authentication, and unproxied
                  servers reach the upstream without Speakeasy in between. */}
              {isUnproxied ||
              mcpServer.toolsetId ||
              upstreamAuth === "legacy" ? null : upstreamAuth === "enforced" ? (
                <UpstreamCredentialsSection mcpServer={mcpServer} />
              ) : (
                <UpstreamIdentityPendingSection
                  id={UPSTREAM_CREDENTIALS_SECTION_ID}
                  status={upstreamAuth}
                />
              )}
              {tunneledMcpServer ? (
                <Fragment key={tunneledMcpServer.id}>
                  <ResourceIdentifierSection
                    tunneledMcpServer={tunneledMcpServer}
                  />
                  <CallerIdentitySection />
                </Fragment>
              ) : null}
            </>
          ),
        },
        !isUnproxied && {
          ...G.connection,
          content: (
            <>
              <ServerUrlSection
                backend={{ mcpServerId: mcpServer.id }}
                endpoints={endpoints}
                isLoadingEndpoints={isLoadingEndpoints}
                mcpServer={mcpServer}
              />
              <NetworkAccessSection
                mcpServer={mcpServer}
                endpoints={endpoints}
              />
              {tunneledMcpServer ? (
                <Fragment key={tunneledMcpServer.id}>
                  <TunnelKeySection tunneledMcpServer={tunneledMcpServer} />
                  <AgentSetupSection tunneledMcpServer={tunneledMcpServer} />
                </Fragment>
              ) : null}
            </>
          ),
        },
        !isUnproxied && {
          ...G.tools,
          content: <ToolFilteringSection mcpServer={mcpServer} />,
        },
        { ...G.danger, content: danger },
      ]}
    />
  );
}

/**
 * Keeps old links to the Access area working on servers that no longer have
 * one: they land on Availability, which used to be all that area held.
 */
function AvailabilityAnchor({
  children,
}: {
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div id={SETTINGS_GROUPS.access.id} className="scroll-mt-8">
      {children}
    </div>
  );
}

/**
 * Placeholder for the Identity section while the enforcement state that picks
 * its contents is loading or unavailable. It renders no controls, so no write
 * surface appears for the wrong mode.
 */
function UpstreamIdentityPendingSection({
  id,
  status,
}: {
  id: string;
  status: Extract<UpstreamAuthEnforcementStatus, "loading" | "error">;
}): JSX.Element {
  return (
    <SettingsSection id={id}>
      <SettingsSection.Header>
        <SettingsSection.Title>Identity</SettingsSection.Title>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {status === "loading" ? (
            <Text muted small>
              Loading…
            </Text>
          ) : (
            <FieldError>
              The identity settings could not be loaded. Refresh the page to try
              again.
            </FieldError>
          )}
        </SettingsSection.Body>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
