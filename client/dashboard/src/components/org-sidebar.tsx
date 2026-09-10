import * as React from "react";

import { AppRoute, useOrgRoutes } from "@/routes";
import { NavButton, NavGroupProvider } from "@/components/nav-menu";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuItem,
  SidebarTrigger,
} from "@/components/ui/Sidebar";
import { useIsPlatformAdmin, useOrganization } from "@/contexts/Auth";

import { GramLogo } from "./gram-logo";
import { HatchRule } from "./hatch-rule";
import { Icon } from "@/components/ui/Icon";
import { Link } from "react-router";
import { RequireScope } from "@/components/require-scope";
import { Scope } from "@gram/client/models/components/rolegrant.js";
import { ScopeGatedNavGroup } from "@/components/scope-gated-nav-group";
import { SidebarFooterAction } from "./sidebar-footer-action";
import { SidebarNavSkeleton } from "./sidebar-nav-skeleton";
import { SidebarUserMenu } from "./sidebar-user-menu";
import { TrialStatusCard } from "./trial-status-card";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { useRBAC } from "@/hooks/useRBAC";
import { useTelemetry } from "@/contexts/Telemetry";
import { useCanSetUpOrg } from "@/hooks/useCanSetUpOrg";
import { Wrench } from "lucide-react";

/** Scopes that make an org-level nav item visible. */
const orgReadOrAdmin: Scope[] = ["org:read", "org:admin"];

function ScopeGatedTopLevelItem({
  item,
  scope,
}: {
  item: AppRoute;
  scope: Scope | Scope[];
}) {
  return (
    <RequireScope scope={scope} level="section">
      <SidebarMenuItem>
        <NavButton
          title={item.title}
          href={item.href()}
          active={item.active}
          Icon={item.Icon}
          stage={item.stage}
        />
      </SidebarMenuItem>
    </RequireScope>
  );
}

export function OrgSidebar({
  ...props
}: React.ComponentProps<typeof Sidebar>): React.JSX.Element {
  const orgRoutes = useOrgRoutes();
  const organization = useOrganization();
  const { isLoading: rbacLoading } = useRBAC();
  const canSetUpOrg = useCanSetUpOrg();
  const telemetry = useTelemetry();
  const { data: productFeatures } = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    {
      staleTime: 30_000,
      throwOnError: false,
    },
  );
  const isPlatformAdmin = useIsPlatformAdmin();
  const isDeviceAgentEnabled =
    telemetry.isFeatureEnabled("gram-device-agent") ?? false;
  const isUserSessionsEnabled =
    telemetry.isFeatureEnabled("user-sessions-dashboard") ?? false;

  const settingsActive = [
    orgRoutes.team,
    orgRoutes.access,
    // The role editor is a sibling route, so the group would otherwise lose
    // its highlight while a role is open.
    orgRoutes.createRole,
    orgRoutes.editRole,
    orgRoutes.billing,
    orgRoutes.apiKeys,
    orgRoutes.domains,
    orgRoutes.logs,
    orgRoutes.skills,
    orgRoutes.aiIntegrations,
    orgRoutes.webhooks,
    orgRoutes.externalServices,
    orgRoutes.encryptionKeys,
  ].some((r) => r.active);

  const dataActive = [orgRoutes.data, orgRoutes.dataExports].some(
    (route) => route.active,
  );

  const secureActive = [orgRoutes.auditLogs, orgRoutes.deviceAgent].some(
    (r) => r.active,
  );

  const identityActive = [
    orgRoutes.agents,
    orgRoutes.mcpSessions,
    orgRoutes.identity,
    orgRoutes.remoteIdentityProviders,
  ].some((r) => r.active);

  const platformAdminActive = [
    orgRoutes.platformAdminOverview,
    orgRoutes.platformAdminRbac,
    orgRoutes.platformAdminOnboarding,
    orgRoutes.platformAdminOpenRouterKeys,
  ].some((r) => r.active);

  const groupActivations: Array<[string, boolean]> = [
    ["Settings", settingsActive],
    ["Data", dataActive],
    ["Secure", secureActive],
    ["Identity", identityActive],
    ["Platform Admin", platformAdminActive],
  ];
  const activeGroup = groupActivations.find(([, active]) => active)?.[0];

  const allOrgNavRoutes = [
    orgRoutes.home,
    orgRoutes.team,
    orgRoutes.billing,
    orgRoutes.apiKeys,
    orgRoutes.domains,
    orgRoutes.logs,
    orgRoutes.skills,
    orgRoutes.aiIntegrations,
    orgRoutes.webhooks,
    orgRoutes.externalServices,
    orgRoutes.encryptionKeys,
    orgRoutes.data,
    orgRoutes.dataExports,
    orgRoutes.auditLogs,
    orgRoutes.deviceAgent,
    orgRoutes.agents,
    orgRoutes.access,
    orgRoutes.mcpSessions,
    orgRoutes.identity,
    orgRoutes.remoteIdentityProviders,
    orgRoutes.platformAdminOverview,
    orgRoutes.platformAdminRbac,
    orgRoutes.platformAdminOnboarding,
    orgRoutes.platformAdminOpenRouterKeys,
  ];
  const activeRoute = allOrgNavRoutes.find((r) => r.active);
  const activeItem = activeRoute?.title;

  return (
    <Sidebar collapsible="icon" {...props}>
      {/* Matches AppSidebar: logo + collapse control on one --header-height row,
          closed by the crosshatch rule so it lines up with the page header. */}
      <SidebarHeader className="gap-0 p-0">
        <div className="flex h-(--header-height) items-center justify-between gap-2 px-2 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0">
          <Link
            to={orgRoutes.home.href()}
            className="flex h-full items-center px-1 hover:no-underline group-data-[collapsible=icon]:hidden"
          >
            <GramLogo className="w-28" />
          </Link>
          <SidebarTrigger />
        </div>
        <HatchRule />
      </SidebarHeader>
      <SidebarContent className="pt-2">
        {rbacLoading ? (
          <SidebarNavSkeleton />
        ) : (
          <NavGroupProvider activeGroup={activeGroup} activeItem={activeItem}>
            <SidebarMenu className="gap-1 px-2">
              {/* Home — top-level */}
              <ScopeGatedTopLevelItem
                item={orgRoutes.home}
                scope={["org:read", "project:read", "org:admin"]}
              />

              {/* Settings group */}
              <ScopeGatedNavGroup
                label="Settings"
                Icon={(p) => <Icon {...p} name="settings" />}
                items={[
                  { item: orgRoutes.billing, scope: orgReadOrAdmin },
                  // Who is in the organization, and what they can do: the two
                  // halves of one question, so they sit together.
                  { item: orgRoutes.team, scope: orgReadOrAdmin },
                  { item: orgRoutes.access, scope: orgReadOrAdmin },
                  { item: orgRoutes.apiKeys, scope: "org:admin" },
                  ...(productFeatures?.customerManagedEncryptionKeysEnabled ===
                  true
                    ? [
                        {
                          item: orgRoutes.externalServices,
                          scope: orgReadOrAdmin,
                        },
                        {
                          item: orgRoutes.encryptionKeys,
                          scope: orgReadOrAdmin,
                        },
                      ]
                    : []),
                  { item: orgRoutes.domains, scope: orgReadOrAdmin },
                  { item: orgRoutes.logs, scope: orgReadOrAdmin },
                  { item: orgRoutes.skills, scope: "org:admin" },
                  { item: orgRoutes.aiIntegrations, scope: orgReadOrAdmin },
                  { item: orgRoutes.webhooks, scope: orgReadOrAdmin },
                ]}
              />

              {/* Data group — org-level access to ingested events and
                  project-scoped export configuration. */}
              <ScopeGatedNavGroup
                label="Data"
                Icon={(p) => <Icon {...p} name="database" />}
                items={[
                  { item: orgRoutes.data, scope: orgReadOrAdmin },
                  { item: orgRoutes.dataExports, scope: orgReadOrAdmin },
                ]}
              />

              {/* Secure group */}
              <ScopeGatedNavGroup
                label="Secure"
                Icon={(p) => <Icon {...p} name="shield-check" />}
                items={[
                  { item: orgRoutes.auditLogs, scope: orgReadOrAdmin },
                  ...(isDeviceAgentEnabled
                    ? [{ item: orgRoutes.deviceAgent, scope: orgReadOrAdmin }]
                    : []),
                ]}
              />

              {/* Identity group */}
              <ScopeGatedNavGroup
                label="Identity"
                Icon={(p) => <Icon {...p} name="fingerprint" />}
                items={[
                  // Owners can manage their agents without an RBAC agent grant.
                  // The API limits the inventory to readable agents.
                  { item: orgRoutes.agents },
                  ...(isUserSessionsEnabled
                    ? [{ item: orgRoutes.mcpSessions, scope: orgReadOrAdmin }]
                    : []),
                  { item: orgRoutes.identity, scope: orgReadOrAdmin },
                  {
                    item: orgRoutes.remoteIdentityProviders,
                    scope: orgReadOrAdmin,
                  },
                ]}
              />

              {/* Platform Admin group — Speakeasy staff only.
                  These surfaces act on the platform itself rather than on the
                  organization being viewed, so they sit in their own section
                  at the bottom instead of among the org's own settings. They
                  carry no RBAC scope: the platform-admin flag is not a grant,
                  and staff viewing a customer org usually hold no org grants
                  at all, so gating on org scopes would hide them from exactly
                  the people they exist for. Passing an empty item list makes
                  ScopeGatedNavGroup render nothing, so non-admins get no
                  header, no group, and no items. */}
              <ScopeGatedNavGroup
                label="Platform Admin"
                Icon={(p) => <Icon {...p} name="crown" />}
                items={[
                  // The admin pages also show in local dev regardless of the
                  // admin flag, like the old floating Developer Toolkit: the
                  // Overview page holds the impersonation toggle non-admin
                  // developers need to turn platform admin on in the first
                  // place. Remote Identity Providers stays strictly
                  // admin-gated — it is real catalog management, not a local
                  // developer aid.
                  ...(isPlatformAdmin || import.meta.env.DEV
                    ? [
                        {
                          // The group header already says "Platform Admin"; the
                          // route titles keep the prefix for Recents and the
                          // command palette, which have no header to lean on.
                          item: orgRoutes.platformAdminOverview,
                          label: "Overview",
                        },
                        {
                          item: orgRoutes.platformAdminRbac,
                          label: "RBAC Override",
                        },
                        {
                          item: orgRoutes.platformAdminOnboarding,
                          label: "Onboarding",
                        },
                      ]
                    : []),
                  ...(isPlatformAdmin
                    ? [
                        // OpenRouter Keys stays strictly admin-gated even in
                        // local dev: it manages live upstream credentials,
                        // not local developer aids.
                        {
                          item: orgRoutes.platformAdminOpenRouterKeys,
                          label: "OpenRouter Keys",
                        },
                      ]
                    : []),
                ]}
              />
            </SidebarMenu>
          </NavGroupProvider>
        )}
      </SidebarContent>
      <SidebarFooter className="border-t">
        <TrialStatusCard />
        {/* One-time org setup: a raised card just above the user bar, out of
            the standing nav but always reachable while it still applies. */}
        {canSetUpOrg && (
          <SidebarFooterAction
            to={orgRoutes.setup.href()}
            icon={Wrench}
            label="Finish organization setup"
            labelClassName="mode-shimmer"
          />
        )}
        <SidebarUserMenu />
      </SidebarFooter>
    </Sidebar>
  );
}
