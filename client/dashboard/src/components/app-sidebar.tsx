import { NavButton, NavMenu } from "@/components/nav-menu";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { useSlugs } from "@/contexts/Sdk";
import { useProductTier } from "@/hooks/useProductTier";
import { AppRoute, useOrgRoutes, useRoutes } from "@/routes";
import { useGetPeriodUsage } from "@gram/client/react-query";
import { cn, Stack } from "@speakeasy-api/moonshine";
import {
  BookOpen,
  ExternalLink,
  MessageCircle,
  MinusIcon,
  Newspaper,
  TestTube2Icon,
  Undo2,
} from "lucide-react";
import * as React from "react";
import { useState } from "react";
import { Link } from "react-router";
import { FeatureRequestModal } from "./FeatureRequestModal";
import { Button } from "./ui/button";
import { Type } from "./ui/type";

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const routes = useRoutes();
  const { orgSlug } = useSlugs();
  const [isUpgradeModalOpen, setIsUpgradeModalOpen] = useState(false);

  const settingsItems = [routes.settings] as AppRoute[];

  const navGroups = {
    connect: [routes.sources, routes.catalog, routes.playground] as AppRoute[],
    build: [routes.elements, routes.mcp, routes.slackApps, routes.clis],
    observe: [
      routes.observability,
      routes.logs,
      routes.chatSessions,
      routes.hooks,
    ],
    settings: settingsItems,
  };

  return (
    <Sidebar collapsible="offcanvas" {...props}>
      <SidebarContent className="pt-2">
        <SidebarGroup>
          <SidebarGroupLabel>project</SidebarGroupLabel>
          <SidebarGroupContent>
            <NavMenu items={[routes.home]} />
          </SidebarGroupContent>
        </SidebarGroup>
        {Object.entries(navGroups).map(([label, items]) => (
          <SidebarGroup key={label}>
            <SidebarGroupLabel>{label}</SidebarGroupLabel>
            <SidebarGroupContent>
              <NavMenu items={items} />
            </SidebarGroupContent>
          </SidebarGroup>
        ))}
        <SidebarGroup>
          <SidebarGroupLabel>get help</SidebarGroupLabel>
          <SidebarGroupContent>
            <NavMenu items={[]}>
              <SidebarMenuItem>
                <NavButton
                  title="Get Support"
                  Icon={(props) => <MessageCircle {...props} />}
                  onClick={() => window.Pylon?.("show")}
                />
              </SidebarMenuItem>
              <SidebarMenuItem>
                <NavButton
                  title="Docs"
                  titleNode={
                    <span className="flex items-center gap-1.5">
                      Docs
                      <ExternalLink className="w-3 h-3 text-muted-foreground" />
                    </span>
                  }
                  href="https://www.speakeasy.com/docs/mcp"
                  target="_blank"
                  Icon={(props) => <BookOpen {...props} />}
                />
              </SidebarMenuItem>
              <SidebarMenuItem>
                <NavButton
                  title="Changelog"
                  titleNode={
                    <span className="flex items-center gap-1.5">
                      Changelog
                      <ExternalLink className="w-3 h-3 text-muted-foreground" />
                    </span>
                  }
                  href="https://www.speakeasy.com/changelog?product=mcp-platform"
                  target="_blank"
                  Icon={(props) => <Newspaper {...props} />}
                />
              </SidebarMenuItem>
            </NavMenu>
          </SidebarGroupContent>
        </SidebarGroup>
        <div className="px-2 py-3">
          <Link
            to={`/${orgSlug}`}
            className="flex items-center gap-1.5 px-2 py-1 text-sm text-muted-foreground hover:text-foreground transition-colors hover:no-underline"
          >
            <Undo2 className="w-3.5 h-3.5" />
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
          <orgRoutes.billing.Link className="w-full mt-auto">
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
      <MinusIcon className="w-4 h-4" />
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
          className="flex items-center justify-center h-full w-full"
        >
          <Type>?</Type>
        </Button>
      )}
    </div>
  );
};
