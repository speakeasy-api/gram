import * as React from "react";

import { AppRoute, useOrgRoutes } from "@/routes";
import { NavButton, NavGroupProvider } from "@/components/nav-menu";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarMenu,
  SidebarMenuItem,
} from "@/components/ui/Sidebar";
import { useIsPlatformAdmin, useOrganization } from "@/contexts/Auth";

import { DevSidebarSlot } from "@/dev/sidebar-slot";
import { Icon } from "@/components/ui/Icon";
import { RequireScope } from "@/components/require-scope";
import { Scope } from "@gram/client/models/components/rolegrant.js";
import { ScopeGatedNavGroup } from "@/components/scope-gated-nav-group";
import { SidebarBrandHeader } from "./sidebar-brand-header";
import { SidebarFooterAction } from "./sidebar-footer-action";
import { SidebarNavSkeleton } from "./sidebar-nav-skeleton";
import { SidebarUserMenu } from "./sidebar-user-menu";
import { TrialStatusCard } from "./trial-status-card";
import { Wrench } from "lucide-react";
import { useCanSetUpOrg } from "@/hooks/useCanSetUpOrg";
import { useNewOnboardingEnabled } from "@/hooks/useNewOnboarding";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { useRBAC } from "@/hooks/useRBAC";
import { useTelemetry } from "@/contexts/Telemetry";

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
  const newOnboarding = useNewOnboardingEnabled();
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

  const settingsActive = [
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

  const teamActive = [
    orgRoutes.team,
    orgRoutes.access,
    // The role editor is a sibling route, so the group would otherwise lose
    // its highlight while a role is open.
    orgRoutes.createRole,
    orgRoutes.editRole,
    orgRoutes.identity,
  ].some((route) => route.active);

  const dataActive = [orgRoutes.data, orgRoutes.dataExports].some(
    (route) => route.active,
  );

  const secureActive = [orgRoutes.auditLogs, orgRoutes.deviceAgent].some(
    (r) => r.active,
  );

  const platformAdminActive = [
    orgRoutes.platformAdminOverview,
    orgRoutes.platformAdminRbac,
    orgRoutes.platformAdminOnboarding,
    orgRoutes.platformAdminOpenRouterKeys,
  ].some((r) => r.active);

  const groupActivations: Array<[string, boolean]> = [
    ["Settings", settingsActive],
    ["Team", teamActive],
    ["Data", dataActive],
    ["Secure", secureActive],
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
    orgRoutes.access,
    orgRoutes.identity,
    orgRoutes.platformAdminOverview,
    orgRoutes.platformAdminRbac,
    orgRoutes.platformAdminOnboarding,
    orgRoutes.platformAdminOpenRouterKeys,
  ];
  const activeRoute = allOrgNavRoutes.find((r) => r.active);
  const activeItem = activeRoute?.title;

  return (
    <Sidebar collapsible="icon" {...props}>
      <SidebarBrandHeader homeHref={orgRoutes.home.href()} />
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
                  {
                    item: orgRoutes.domains,
                    scope: orgReadOrAdmin,
                    label: "Network Access",
                  },
                  { item: orgRoutes.logs, scope: orgReadOrAdmin },
                  { item: orgRoutes.skills, scope: "org:admin" },
                  { item: orgRoutes.aiIntegrations, scope: orgReadOrAdmin },
                  { item: orgRoutes.webhooks, scope: orgReadOrAdmin },
                ]}
              />

              {/* Team group */}
              <ScopeGatedNavGroup
                label="Team"
                Icon={(p) => <Icon {...p} name="users" />}
                items={[
                  {
                    item: orgRoutes.team,
                    scope: orgReadOrAdmin,
                    label: "Members",
                  },
                  { item: orgRoutes.access, scope: orgReadOrAdmin },
                  { item: orgRoutes.identity, scope: orgReadOrAdmin },
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
            to={
              newOnboarding
                ? orgRoutes.onboarding.href()
                : orgRoutes.setup.href()
            }
            icon={Wrench}
            label={
              newOnboarding ? "Finish onboarding" : "Finish organization setup"
            }
            labelClassName="mode-shimmer"
          />
        )}
        {DevSidebarSlot && <DevSidebarSlot />}
        <SidebarUserMenu />
      </SidebarFooter>
    </Sidebar>
  );
}
