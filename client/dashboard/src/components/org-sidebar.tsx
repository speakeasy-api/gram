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
import { OnboardingResumeButton } from "./onboarding-resume-button";
import { RequireScope } from "@/components/require-scope";
import { Scope } from "@gram/client/models/components/rolegrant.js";
import { ScopeGatedNavGroup } from "@/components/scope-gated-nav-group";
import { SidebarNavSkeleton } from "./sidebar-nav-skeleton";
import { SidebarUserMenu } from "./sidebar-user-menu";
import { TrialStatusCard } from "./trial-status-card";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { useRBAC } from "@/hooks/useRBAC";
import { useTelemetry } from "@/contexts/Telemetry";
import { useKillswitchAccess } from "@/hooks/useKillswitchAccess";

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
  const killswitchAccess = useKillswitchAccess();
  const isDeviceAgentEnabled =
    telemetry.isFeatureEnabled("gram-device-agent") ?? false;
  const isUserSessionsEnabled =
    telemetry.isFeatureEnabled("user-sessions-dashboard") ?? false;

  const settingsActive = [
    orgRoutes.billing,
    orgRoutes.apiKeys,
    orgRoutes.domains,
    orgRoutes.logs,
    orgRoutes.skills,
    orgRoutes.platformMcp,
    orgRoutes.aiIntegrations,
    orgRoutes.webhooks,
    orgRoutes.externalServices,
    orgRoutes.encryptionKeys,
  ].some((r) => r.active);

  const dataActive = [orgRoutes.data].some((r) => r.active);

  const secureActive = [
    orgRoutes.auditLogs,
    orgRoutes.killswitch,
    orgRoutes.deviceAgent,
    orgRoutes.access,
  ].some((r) => r.active);

  const identityActive = [
    orgRoutes.mcpSessions,
    orgRoutes.identity,
    orgRoutes.remoteIdentityProviders,
  ].some((r) => r.active);

  const platformAdminActive = [
    orgRoutes.platformAdminOverview,
    orgRoutes.platformAdminRbac,
    orgRoutes.platformAdminFeatures,
    orgRoutes.platformAdminOnboarding,
    orgRoutes.platformAdminOpenRouterKeys,
    orgRoutes.platformRemoteIdentityProviders,
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
    orgRoutes.platformMcp,
    orgRoutes.aiIntegrations,
    orgRoutes.webhooks,
    orgRoutes.externalServices,
    orgRoutes.encryptionKeys,
    orgRoutes.data,
    orgRoutes.auditLogs,
    orgRoutes.killswitch,
    orgRoutes.deviceAgent,
    orgRoutes.access,
    orgRoutes.mcpSessions,
    orgRoutes.identity,
    orgRoutes.remoteIdentityProviders,
    orgRoutes.platformAdminOverview,
    orgRoutes.platformAdminRbac,
    orgRoutes.platformAdminFeatures,
    orgRoutes.platformAdminOnboarding,
    orgRoutes.platformAdminOpenRouterKeys,
    orgRoutes.platformRemoteIdentityProviders,
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
          <NavGroupProvider
            activeGroup={activeGroup}
            defaultOpenGroups={["Settings", "Data", "Secure", "Identity"]}
            activeItem={activeItem}
          >
            <SidebarMenu className="gap-1 px-2">
              {/* Home — top-level */}
              <ScopeGatedTopLevelItem
                item={orgRoutes.home}
                scope={["org:read", "project:read", "org:admin"]}
              />

              {/* Team — top-level */}
              <ScopeGatedTopLevelItem
                item={orgRoutes.team}
                scope={["org:read", "org:admin"]}
              />

              {/* Settings group */}
              <ScopeGatedNavGroup
                label="Settings"
                Icon={(p) => <Icon {...p} name="settings" />}
                items={[
                  { item: orgRoutes.billing, scope: orgReadOrAdmin },
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
                  { item: orgRoutes.platformMcp, scope: "org:admin" },
                  { item: orgRoutes.aiIntegrations, scope: orgReadOrAdmin },
                  { item: orgRoutes.webhooks, scope: orgReadOrAdmin },
                ]}
              />

              {/* Data group — org-wide ingested telemetry surfaces. The Event
                  Feed item carries a Preview badge via its route's `stage`. */}
              <ScopeGatedNavGroup
                label="Data"
                Icon={(p) => <Icon {...p} name="database" />}
                items={[{ item: orgRoutes.data, scope: orgReadOrAdmin }]}
              />

              {/* Secure group */}
              <ScopeGatedNavGroup
                label="Secure"
                Icon={(p) => <Icon {...p} name="shield-check" />}
                items={[
                  { item: orgRoutes.auditLogs, scope: orgReadOrAdmin },
                  ...(killswitchAccess.canAccess
                    ? [
                        {
                          item: orgRoutes.killswitch,
                          scope: "org:admin" as const,
                        },
                      ]
                    : []),
                  ...(isDeviceAgentEnabled
                    ? [{ item: orgRoutes.deviceAgent, scope: orgReadOrAdmin }]
                    : []),
                  { item: orgRoutes.access, scope: orgReadOrAdmin },
                ]}
              />

              {/* Identity group */}
              <ScopeGatedNavGroup
                label="Identity"
                Icon={(p) => <Icon {...p} name="fingerprint" />}
                items={[
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
                          item: orgRoutes.platformAdminFeatures,
                          label: "Features",
                        },
                        {
                          item: orgRoutes.platformAdminOnboarding,
                          label: "Onboarding",
                        },
                      ]
                    : []),
                  ...(isPlatformAdmin
                    ? [
                        // OpenRouter Keys and Remote Identity Providers stay
                        // strictly admin-gated even in local dev: both manage
                        // real platform state (live upstream credentials, the
                        // shared issuer catalog), not local developer aids.
                        {
                          item: orgRoutes.platformAdminOpenRouterKeys,
                          label: "OpenRouter Keys",
                        },
                        {
                          item: orgRoutes.platformRemoteIdentityProviders,
                          label: "Remote Identity Providers",
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
        <OnboardingResumeButton />
        <SidebarUserMenu />
      </SidebarFooter>
    </Sidebar>
  );
}
