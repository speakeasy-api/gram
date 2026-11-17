import {
  DETAIL_HEADER_CONTROL_BUTTON,
  DetailFrame,
  DetailHeaderControls,
  DetailHeaderLayout,
  DetailHeaderTitle,
  DetailHeaderTitleSkeleton,
  type DetailFrameTab,
  type DetailHeaderControlRow,
} from "@/components/detail/detail-frame";
import { SettingsLayout } from "@/components/detail/settings-layout";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { RequireScope } from "@/components/require-scope";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useScrollToSettingsHash } from "@/hooks/useScrollToSettingsHash";
import { activeDetailTab } from "@/lib/detail-tabs";
import { splitIntoUnit } from "@/pages/mcp/x/tabs/settings/sections/authentication/sessionSettings";
import { ClientAccessSection } from "@/pages/user-session-issuers/ClientAccessSection";
import { EnterpriseManagedAuthSection } from "@/pages/user-session-issuers/EnterpriseManagedAuthSection";
import { SessionLengthSection } from "@/pages/user-session-issuers/SessionLengthSection";
import {
  ActionsSection,
  AdvancedSection,
  McpServersSection,
} from "@/pages/user-session-issuers/UserSessionIssuerDetail";
import { useRoutes } from "@/routes";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useOrganizationUserSessionIssuer } from "@gram/client/react-query/organizationUserSessionIssuer.js";
import { ChevronRight } from "lucide-react";
import { useEffect } from "react";
import {
  Link,
  Navigate,
  useLocation,
  useNavigate,
  useParams,
} from "react-router";
import { ConnectionsTab } from "./ConnectionsTab";
import { ProviderPicker } from "./ProviderPicker";
import {
  PROVIDER_TABS,
  providerScopeLabel,
  useLastProvider,
  useProjectProviders,
  type ProviderTab,
} from "./providers";

const TAB_TITLES: Record<ProviderTab, string> = {
  connections: "Connections",
  "mcp-servers": "MCP servers",
  settings: "Settings",
};

/** The provider's settings areas, in page order. */
const SETTINGS_AREAS = {
  sessions: { id: "provider-sessions", title: "Sessions" },
  clientAccess: { id: "provider-client-access", title: "Client access" },
  ema: {
    id: "provider-enterprise-managed-auth",
    title: "Enterprise Managed Auth",
  },
  advanced: { id: "provider-advanced", title: "Advanced" },
  danger: { id: "provider-danger-zone", title: "Danger zone" },
} as const;

/** Areas and the sections a link may name directly. */
const SCROLLABLE_SETTINGS_IDS = [
  ...Object.values(SETTINGS_AREAS).map((area) => area.id),
  "session-length",
  "client-access",
];

/**
 * `/mcp-sessions/:issuerId/*`: one MCP Authentication Provider, picked in
 * the header, with its connections, the MCP servers that use it, and how
 * sign-in through it works.
 */
export default function McpSessionsProvider(): JSX.Element {
  const routes = useRoutes();
  const project = useProject();
  const navigate = useNavigate();
  const { pathname, search } = useLocation();
  const { issuerId = "" } = useParams<{ issuerId: string }>();
  const { providers, isLoading } = useProjectProviders();
  const [, rememberProvider] = useLastProvider();
  const activeTab = activeDetailTab(pathname, PROVIDER_TABS);
  const provider = providers.find((p) => p.id === issuerId);

  const tabHref = (tab: ProviderTab, id: string = issuerId) => {
    switch (tab) {
      case "connections":
        return routes.mcpSessions.provider.connections.href(id);
      case "mcp-servers":
        return routes.mcpSessions.provider.mcpServers.href(id);
      case "settings":
        return routes.mcpSessions.provider.settings.href(id);
    }
  };

  // Opening a provider, by the picker or a link, makes it the one the page
  // opens on next time.
  useEffect(() => {
    if (provider) rememberProvider(provider.id);
  }, [provider, rememberProvider]);

  if (!activeTab)
    return <Navigate to={`${tabHref("connections")}${search}`} replace />;
  // A provider this project can't use, or one since consolidated away:
  // the index picks another.
  if (!isLoading && !provider) {
    return <Navigate to={routes.mcpSessions.href()} replace />;
  }

  const tabs: DetailFrameTab[] = PROVIDER_TABS.map((tab) => ({
    key: tab,
    title: TAB_TITLES[tab],
    href: tabHref(tab),
  }));

  return (
    <DetailFrame
      breadcrumbs={{ substitutions: { [issuerId]: provider?.slug } }}
      tabsLabel="MCP Authentication Provider sections"
      tabs={tabs}
      activeTab={activeTab}
      header={
        <DetailHeaderLayout
          title={
            provider ? (
              <DetailHeaderTitle
                eyebrow="MCP Authentication Provider"
                name={
                  <ProviderPicker
                    current={provider}
                    providers={providers}
                    // Same tab, other provider; the query's filters (a
                    // person, a status) still apply.
                    onSelect={(next) =>
                      void navigate(`${tabHref(activeTab, next.id)}${search}`)
                    }
                  />
                }
              />
            ) : (
              <DetailHeaderTitleSkeleton icon={false} />
            )
          }
          controls={
            provider ? (
              <ProviderHeaderControls
                provider={provider}
                settingsHref={tabHref("settings")}
              />
            ) : null
          }
        />
      }
    >
      {/* Keyed, so switching project or provider starts each tab afresh:
          a search for one project's connections never carries over. */}
      <div key={`${project.id}:${issuerId}`} className="contents">
        {activeTab === "connections" ? (
          <ConnectionsTab providerId={issuerId} />
        ) : activeTab === "mcp-servers" ? (
          <ProviderMcpServersTab issuerId={issuerId} />
        ) : (
          <ProviderSettingsTab issuerId={issuerId} />
        )}
      </div>
    </DetailFrame>
  );
}

/** How sign-in through the provider is set up, each linking to its setting. */
function ProviderHeaderControls({
  provider,
  settingsHref,
}: {
  provider: UserSessionIssuer;
  settingsHref: string;
}): JSX.Element {
  const duration = splitIntoUnit(provider.sessionDurationHours);
  const rows: DetailHeaderControlRow[] = [
    {
      key: "scope",
      label: "Scope",
      control: (
        <span className="text-foreground flex h-8 items-center text-sm font-medium">
          {providerScopeLabel(provider)}
        </span>
      ),
    },
    {
      key: "session-length",
      label: "Session length",
      control: (
        <HeaderSettingLink
          href={`${settingsHref}#session-length`}
          label={`${duration.number} ${duration.unit}${duration.number === 1 ? "" : "s"}`}
          setting="session length"
        />
      ),
    },
    {
      key: "client-access",
      label: "Client access",
      control: (
        <HeaderSettingLink
          href={`${settingsHref}#client-access`}
          label={clientAccessLabel(provider.clientIdMetadataAdmissionMode)}
          setting="client access"
        />
      ),
    },
  ];
  return <DetailHeaderControls rows={rows} />;
}

function HeaderSettingLink({
  href,
  label,
  setting,
}: {
  href: string;
  label: string;
  setting: string;
}): JSX.Element {
  return (
    <Link
      to={href}
      aria-label={`${label}. Change ${setting} in Settings`}
      className={DETAIL_HEADER_CONTROL_BUTTON}
    >
      <span className="min-w-0 flex-1 truncate text-left">{label}</span>
      <ChevronRight className="text-muted-foreground size-3.5 shrink-0" />
    </Link>
  );
}

/** The client access mode as Settings words it. */
function clientAccessLabel(mode: string): string {
  if (mode === "presets") return "Known clients";
  // "reporting" is a legacy value that admits what "open" admits.
  if (mode === "open" || mode === "reporting") return "Any client";
  return "Off";
}

/**
 * The organization's view of the provider, which Settings and MCP servers
 * read. Gated on the organization's scopes, as the provider page was.
 */
function useProviderDetail(issuerId: string): {
  issuer: UserSessionIssuer | undefined;
  isPending: boolean;
  missing: boolean;
  error: unknown;
} {
  const query = useOrganizationUserSessionIssuer({ id: issuerId }, undefined, {
    throwOnError: false,
    retry: false,
  });
  return {
    issuer: query.data,
    isPending: query.isPending,
    missing: query.isError && !query.data,
    error: query.error,
  };
}

function ProviderUnavailable({ error }: { error: unknown }): JSX.Element {
  const notFound = error instanceof GramError && error.statusCode === 404;
  return (
    <InlineEmptyState
      icon="triangle-alert"
      heading={
        notFound
          ? "MCP Authentication Provider not found"
          : "Couldn't load this MCP Authentication Provider"
      }
      description={
        notFound
          ? "It may have been consolidated into another one or removed."
          : "Reload the page to try again."
      }
    />
  );
}

function ProviderMcpServersTab({
  issuerId,
}: {
  issuerId: string;
}): JSX.Element {
  return (
    <RequireScope scope={["org:read", "org:admin"]} level="section">
      <ProviderMcpServers issuerId={issuerId} />
    </RequireScope>
  );
}

function ProviderMcpServers({ issuerId }: { issuerId: string }): JSX.Element {
  const { issuer, isPending, missing, error } = useProviderDetail(issuerId);
  if (isPending) return <SkeletonTable label="Loading MCP servers" />;
  if (missing || !issuer) return <ProviderUnavailable error={error} />;
  return (
    <div className="space-y-4">
      <Text muted small className="max-w-2xl">
        Every MCP server and gateway that signs people in through this provider.
        Changes to its settings apply to all of them.
      </Text>
      <McpServersSection issuer={issuer} titled={false} />
    </div>
  );
}

function ProviderSettingsTab({ issuerId }: { issuerId: string }): JSX.Element {
  return (
    <RequireScope scope={["org:read", "org:admin"]} level="section">
      <ProviderSettings issuerId={issuerId} />
    </RequireScope>
  );
}

function ProviderSettings({ issuerId }: { issuerId: string }): JSX.Element {
  const project = useProject();
  const routes = useRoutes();
  const { issuer, isPending, missing, error } = useProviderDetail(issuerId);
  // Header links and old bookmarks land on a section or an area.
  useScrollToSettingsHash(SCROLLABLE_SETTINGS_IDS, !!issuer);
  if (isPending) return <SkeletonTable label="Loading settings" />;
  if (missing || !issuer) return <ProviderUnavailable error={error} />;

  return (
    <SettingsLayout
      groups={[
        {
          ...SETTINGS_AREAS.sessions,
          content: (
            <SessionLengthSection
              issuer={issuer}
              editableProjectId={project.id}
            />
          ),
        },
        {
          ...SETTINGS_AREAS.clientAccess,
          content: (
            <ClientAccessSection
              issuer={issuer}
              editableProjectId={project.id}
            />
          ),
        },
        {
          ...SETTINGS_AREAS.ema,
          content: <EnterpriseManagedAuthSection issuer={issuer} />,
        },
        {
          ...SETTINGS_AREAS.advanced,
          content: <AdvancedSection issuer={issuer} />,
        },
        {
          ...SETTINGS_AREAS.danger,
          content: (
            <ActionsSection
              issuer={issuer}
              // Consolidating removes this provider; open the one that took
              // its place.
              detailHref={routes.mcpSessions.provider.settings.href}
            />
          ),
        },
      ]}
    />
  );
}
