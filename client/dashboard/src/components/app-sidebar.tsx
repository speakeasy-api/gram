import { NavButton } from "@/components/nav-menu";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { useSlugs } from "@/contexts/Sdk";
import { Scope } from "@/hooks/useRBAC";
import { useProductTier } from "@/hooks/useProductTier";
import { AppRoute, useOrgRoutes, useRoutes } from "@/routes";
import { useGetPeriodUsage } from "@gram/client/react-query";
import { cn, Stack } from "@speakeasy-api/moonshine";
import { MinusIcon, TestTube2Icon, Undo2 } from "lucide-react";
import * as React from "react";
import { useState } from "react";
import { Link } from "react-router";
import { RequireScope } from "./require-scope";
import { FeatureRequestModal } from "./FeatureRequestModal";
import { Button } from "./ui/button";
import { Type } from "./ui/type";

function ScopeGatedNavItem({
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
        />
      </SidebarMenuItem>
    </RequireScope>
  );
}

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const routes = useRoutes();
  const { orgSlug } = useSlugs();
  const [isUpgradeModalOpen, setIsUpgradeModalOpen] = useState(false);

  return (
    <Sidebar collapsible="offcanvas" {...props}>
      <SidebarContent className="pt-2">
        <SidebarGroup>
          <SidebarGroupLabel>project</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              <ScopeGatedNavItem item={routes.home} scope="project:read" />
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
        <SidebarGroup>
          <SidebarGroupLabel>connect</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              <ScopeGatedNavItem
                item={routes.sources}
                scope={["project:read", "project:write"]}
              />
              <ScopeGatedNavItem
                item={routes.catalog}
                scope={["project:read", "mcp:write"]}
              />
              <ScopeGatedNavItem
                item={routes.playground}
                scope={["mcp:read", "mcp:write", "mcp:connect"]}
              />
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
        <SidebarGroup>
          <SidebarGroupLabel>build</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              <ScopeGatedNavItem item={routes.elements} scope="project:read" />
              <ScopeGatedNavItem
                item={routes.mcp}
                scope={["mcp:read", "mcp:write"]}
              />
              <ScopeGatedNavItem
                item={routes.slackApps}
                scope={["mcp:read", "mcp:write"]}
              />
              <ScopeGatedNavItem item={routes.clis} scope="project:read" />
              <ScopeGatedNavItem
                item={routes.plugins}
                scope={["project:read", "project:write"]}
              />
              <ScopeGatedNavItem
                item={routes.environments}
                scope={["project:read", "project:write"]}
              />
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
        <SidebarGroup>
          <SidebarGroupLabel>observe</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              <ScopeGatedNavItem
                item={routes.observability}
                scope="project:read"
              />
              <ScopeGatedNavItem item={routes.logs} scope="project:read" />
              <ScopeGatedNavItem
                item={routes.chatSessions}
                scope="project:read"
              />
              <ScopeGatedNavItem
                item={routes.hooks}
                scope={["project:read", "project:write"]}
              />
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
        <SidebarGroup>
          <SidebarGroupLabel>security</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              <ScopeGatedNavItem
                item={routes.riskOverview}
                scope="project:read"
              />
              <ScopeGatedNavItem
                item={routes.policyCenter}
                scope={["project:read", "project:write"]}
              />
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
        <SidebarGroup>
          <SidebarGroupLabel>settings</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              <ScopeGatedNavItem item={routes.settings} scope="project:write" />
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
        <div className="mt-auto px-2 py-3">
          <Link
            to={`/${orgSlug}`}
            className="text-muted-foreground hover:text-foreground flex items-center gap-1.5 px-2 py-1 text-sm transition-colors hover:no-underline"
          >
            <Undo2 className="h-3.5 w-3.5" />
            <span>Back to org</span>
          </Link>
        </div>
      </SidebarContent>
      <SidebarFooter>
        <FreeTierExceededNotification />
      </SidebarFooter>
      <FeatureRequestModal
        isOpen={isUpgradeModalOpen}
        onClose={() => setIsUpgradeModalOpen(false)}
        title="Access Logs"
        description="Logs are available for Pro and Enterprise customers. Upgrade your account to access detailed logging and analytics for your tools."
        actionType="logs_page_access"
        icon={TestTube2Icon}
        accountUpgrade={true}
      />
    </Sidebar>
  );
}

const FreeTierExceededNotification = () => {
  const productTier = useProductTier();
  // Only fetch usage data for free-tier users — this notification is
  // irrelevant for paid/enterprise tiers and the request takes ~3s.
  const { data: usage } = useGetPeriodUsage(undefined, undefined, {
    throwOnError: false,
    enabled: productTier === "base",
  });
  const orgRoutes = useOrgRoutes();

  if (!usage || productTier !== "base") {
    return null;
  }

  if (
    usage.toolCalls > usage.includedToolCalls ||
    usage.servers > usage.includedServers
  ) {
    return (
      <PersistentNotification variant="error">
        <Stack direction="vertical" gap={3} className="h-full">
          <Type variant="subheading">Limits exceeded</Type>
          <Type small>
            Free tier limits exceeded. Upgrade to continue using Gram.
          </Type>
          <orgRoutes.billing.Link className="mt-auto w-full">
            <Button size="sm" className="w-full">
              Billing →
            </Button>
          </orgRoutes.billing.Link>
        </Stack>
      </PersistentNotification>
    );
  }

  return null;
};

const PersistentNotification = ({
  variant = "default",
  className,
  children,
}: {
  variant?: "default" | "warning" | "error";
  className?: string;
  children: React.ReactNode;
}) => {
  const [isMinimized, setIsMinimized] = React.useState(false);

  const variantClass = {
    default: "bg-card border-border",
    warning:
      "bg-warning-softest border-warning-foreground text-warning-foreground",
    error: "bg-destructive-softest border-destructive text-destructive",
  }[variant];

  const closeButton = (
    <Button
      variant="ghost"
      size="icon"
      className="absolute top-0 right-0 hover:bg-transparent"
      onClick={() => setIsMinimized(true)}
    >
      <MinusIcon className="h-4 w-4" />
    </Button>
  );

  let classes =
    "absolute bottom-2 left-1/2 h-[180px] w-[180px] -translate-x-1/2 rounded-lg p-4 border trans overflow-clip ";
  if (isMinimized) {
    classes +=
      "h-[12px] w-[12px] left-2 translate-x-0 cursor-pointer hover:scale-110";
  }

  return (
    <div
      className={cn(classes, variantClass, className)}
      onClick={() => setIsMinimized(false)}
    >
      {!isMinimized && children}
      {!isMinimized && closeButton}
      {isMinimized && (
        <Button
          variant="ghost"
          size="icon"
          className="flex h-full w-full items-center justify-center"
        >
          <Type>?</Type>
        </Button>
      )}
    </div>
  );
};
