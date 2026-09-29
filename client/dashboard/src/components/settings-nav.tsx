import { AppRoute, useOrgRoutes } from "@/routes";
import { useIsPlatformAdmin, useOrganization } from "@/contexts/Auth";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { Scope } from "@gram/client/models/components/rolegrant.js";
import { SidebarNavSkeleton } from "./sidebar-nav-skeleton";
import { Link } from "react-router";
import { SettingsSearch } from "./settings-search";
import { SETTINGS_SEARCH_TERMS } from "./settings-search-index";
import { Wrench } from "lucide-react";
import { cn } from "@/lib/utils";
import { useCanSetUpOrg } from "@/hooks/useCanSetUpOrg";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { useRBAC } from "@/hooks/useRBAC";
import { useTelemetry } from "@/contexts/Telemetry";

/** Scopes that make an org-level nav item visible. */
const orgReadOrAdmin: Scope[] = ["org:read", "org:admin"];

type SettingsNavEntry = {
  item: AppRoute;
  /** Any one scope grants visibility. Omit for items gated by something else. */
  scope?: Scope | Scope[];
  label?: string;
  /** Overrides the route's own match, e.g. to cover sibling routes. */
  active?: boolean;
};

type SettingsNavSection = { label: string; entries: SettingsNavEntry[] };

const ROW_CLASS =
  "flex items-center gap-2.5 px-3 py-1 text-sm hover:no-underline transition-colors";

function SettingsNavLink({ entry }: { entry: SettingsNavEntry }) {
  const { item } = entry;
  const active = entry.active ?? item.active;
  return (
    <Link
      to={item.href()}
      aria-current={active ? "page" : undefined}
      className={cn(
        ROW_CLASS,
        active
          ? "bg-card text-foreground border-border border"
          : "text-muted-foreground hover:text-foreground border border-transparent",
      )}
    >
      <item.Icon className="size-4 shrink-0" />
      <span className="truncate">{entry.label ?? item.title}</span>
      {item.stage && <ReleaseStageBadge stage={item.stage} noTooltip />}
    </Link>
  );
}

/**
 * The left column of the global settings overlay: the organization, then its
 * settings as flat, labelled sections — all visible at once, unlike the app's
 * collapsible sidebar groups. Sections follow the question an admin arrives
 * with: who can get in, how the org is secured, what connects its agents, and
 * where its data goes.
 */
export function SettingsNav(): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const organization = useOrganization();
  const { isLoading: rbacLoading, hasScope, hasAnyScope } = useRBAC();
  const canReadFeatures = !rbacLoading && hasScope("org:read", organization.id);
  const canSetUpOrg = useCanSetUpOrg();
  const telemetry = useTelemetry();
  const { data: featuresData } = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    {
      enabled: canReadFeatures,
      staleTime: 30_000,
      throwOnError: false,
    },
  );
  const productFeatures = canReadFeatures ? featuresData : undefined;
  const isPlatformAdmin = useIsPlatformAdmin();
  const isDeviceAgentEnabled =
    telemetry.isFeatureEnabled("gram-device-agent") ?? false;
  const encryptionKeysEnabled =
    productFeatures?.customerManagedEncryptionKeysEnabled === true;

  const sections: SettingsNavSection[] = [
    {
      label: "Workspace",
      entries: [
        {
          item: orgRoutes.projects,
          scope: ["org:read", "project:read", "org:admin"],
        },
        { item: orgRoutes.billing, scope: orgReadOrAdmin },
      ],
    },
    {
      label: "People",
      entries: [
        { item: orgRoutes.team, scope: orgReadOrAdmin, label: "Members" },
        {
          item: orgRoutes.access,
          scope: orgReadOrAdmin,
          // The role editor is a sibling route, not a subpage.
          active:
            orgRoutes.access.active ||
            orgRoutes.createRole.active ||
            orgRoutes.editRole.active,
        },
        {
          item: orgRoutes.identity,
          scope: orgReadOrAdmin,
          label: "SSO & Directory",
        },
      ],
    },
    {
      label: "Security",
      entries: [
        {
          item: orgRoutes.domains,
          scope: orgReadOrAdmin,
          label: "Network Access",
        },
        { item: orgRoutes.apiKeys, scope: "org:admin" },
        ...(encryptionKeysEnabled
          ? [
              { item: orgRoutes.encryptionKeys, scope: orgReadOrAdmin },
              { item: orgRoutes.externalServices, scope: orgReadOrAdmin },
            ]
          : []),
        { item: orgRoutes.auditLogs, scope: orgReadOrAdmin },
      ],
    },
    {
      label: "AI & Agents",
      entries: [
        { item: orgRoutes.aiIntegrations, scope: orgReadOrAdmin },
        { item: orgRoutes.skills, scope: "org:admin" },
        ...(isDeviceAgentEnabled
          ? [{ item: orgRoutes.deviceAgent, scope: orgReadOrAdmin }]
          : []),
      ],
    },
    {
      label: "Data",
      entries: [
        { item: orgRoutes.logs, scope: orgReadOrAdmin },
        { item: orgRoutes.data, scope: orgReadOrAdmin },
        { item: orgRoutes.dataExports, scope: orgReadOrAdmin },
        { item: orgRoutes.webhooks, scope: orgReadOrAdmin },
      ],
    },
    // Speakeasy staff only (plus local dev, where the Overview page holds the
    // impersonation toggle developers need to become platform admin). No
    // RBAC scope: the platform-admin flag is not a grant, and staff viewing a
    // customer org usually hold no org grants at all.
    {
      label: "Platform Admin",
      entries: [
        ...(isPlatformAdmin || import.meta.env.DEV
          ? [
              { item: orgRoutes.platformAdminOverview, label: "Overview" },
              { item: orgRoutes.platformAdminRbac, label: "RBAC Override" },
              { item: orgRoutes.platformAdminOnboarding, label: "Onboarding" },
            ]
          : []),
        // Manages live upstream credentials, so admin-only even in local dev.
        ...(isPlatformAdmin
          ? [
              {
                item: orgRoutes.platformAdminOpenRouterKeys,
                label: "OpenRouter Keys",
              },
            ]
          : []),
      ],
    },
  ];

  const visibleSections = sections
    .map((section) => ({
      ...section,
      entries: section.entries.filter((entry) => {
        if (entry.scope === undefined) return true;
        const scopes = Array.isArray(entry.scope) ? entry.scope : [entry.scope];
        return hasAnyScope(scopes);
      }),
    }))
    .filter((section) => section.entries.length > 0);

  const searchable = visibleSections.flatMap((section) =>
    section.entries.map((entry) => ({
      item: entry.item,
      title: entry.label ?? entry.item.title,
      section: section.label,
      terms: SETTINGS_SEARCH_TERMS[entry.item.url] ?? [],
    })),
  );

  return (
    <nav
      aria-label="Global settings"
      className="flex min-h-full flex-col gap-4 px-4 py-6"
    >
      {/* Names the settings dialog for screen readers; the chrome itself
          needs no title. */}
      <h2 id="global-settings-title" className="sr-only">
        Global settings
      </h2>
      <SettingsSearch settings={searchable} />

      {rbacLoading && <SidebarNavSkeleton />}
      {!rbacLoading &&
        visibleSections.map((section) => (
          <div key={section.label} className="flex flex-col">
            <div className="text-eyebrow px-3 pb-1">{section.label}</div>
            {section.entries.map((entry) => (
              <SettingsNavLink key={entry.item.url} entry={entry} />
            ))}
          </div>
        ))}

      {canSetUpOrg && (
        <Link
          to={orgRoutes.setup.href()}
          className={cn(
            ROW_CLASS,
            "text-foreground bg-card border-border mt-auto border",
          )}
        >
          <Wrench className="size-4 shrink-0" />
          <span className="mode-shimmer truncate">
            Finish organization setup
          </span>
        </Link>
      )}
    </nav>
  );
}
