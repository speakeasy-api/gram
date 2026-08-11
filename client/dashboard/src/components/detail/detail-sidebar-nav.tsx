import { NavButton, NavGroupProvider } from "@/components/nav-menu";
import { SidebarFooterAction } from "@/components/sidebar-footer-action";
import { SidebarMenu, SidebarMenuItem } from "@/components/ui/Sidebar";
import { useSidebar } from "@/components/ui/Sidebar/sidebar-context";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { ArrowLeft } from "lucide-react";
import * as React from "react";

export function DetailSidebarInfoLabel({
  children,
}: {
  children: React.ReactNode;
}): React.JSX.Element {
  return (
    <Text
      variant="small"
      muted
      className="font-mono text-xs tracking-wide uppercase"
    >
      {children}
    </Text>
  );
}

function SidebarDivider({ className }: { className: string }) {
  return (
    <li aria-hidden="true" className={className}>
      <div className="border-border border-t" />
    </li>
  );
}

// The card title aligns with its own inner p-3 padding (pl-5); the nav items
// align with NavButton's own px-2 padding stacked on SidebarMenu's px-2 (pl-4).
function SidebarEyebrow({
  children,
  align,
}: {
  children: React.ReactNode;
  align: "card" | "items";
}) {
  return (
    <li
      className={cn(
        "pt-1 pr-2 group-data-[collapsible=icon]:hidden",
        align === "card" ? "pb-1 pl-5" : "pb-2 pl-4",
      )}
    >
      <DetailSidebarInfoLabel>{children}</DetailSidebarInfoLabel>
    </li>
  );
}

export type DetailSidebarNavItem = {
  key: string;
  title: string;
  titleNode?: React.ReactNode;
  Icon: React.ComponentType<{ className?: string }>;
  href: string;
  active: boolean;
};

export function DetailSidebarNav({
  backHref,
  backLabel = "Back to all servers",
  topTitle,
  topContent,
  cardContent,
  items,
  itemsTitle = "Configuration",
}: {
  backHref: string;
  backLabel?: string;
  /** Eyebrow label for topContent, styled like "At a glance". */
  topTitle?: string;
  /** Rendered above the "At a glance" card, e.g. a readiness summary. */
  topContent?: React.ReactNode;
  cardContent?: React.ReactNode;
  items: DetailSidebarNavItem[];
  /** Eyebrow label above the nav items. */
  itemsTitle?: string;
}): React.JSX.Element {
  const activeItemTitle = items.find((item) => item.active)?.title;
  const { state } = useSidebar();
  const isCollapsed = state === "collapsed";

  return (
    <NavGroupProvider activeItem={activeItemTitle}>
      {/* min-h-0 + overflow-y-auto: with the at-a-glance card expanded the
          nav can exceed the viewport, so it must be allowed to scroll instead
          of clipping the last items below the fold. */}
      <SidebarMenu className="min-h-0 gap-1 overflow-y-auto px-2 group-data-[collapsible=icon]:px-0">
        <SidebarMenuItem>
          <SidebarFooterAction
            to={backHref}
            icon={ArrowLeft}
            label={backLabel}
          />
        </SidebarMenuItem>

        <SidebarDivider className="mt-3 mb-2 px-1" />

        {topContent && (
          <>
            {topTitle && (
              <SidebarEyebrow align="card">{topTitle}</SidebarEyebrow>
            )}
            <li className="pt-1 pb-3 group-data-[collapsible=icon]:hidden">
              {topContent}
            </li>
            <SidebarDivider className="mb-2 group-data-[collapsible=icon]:hidden" />
          </>
        )}

        <SidebarEyebrow align="card">At a glance</SidebarEyebrow>

        {cardContent && (
          <li className="pt-1 pb-3 group-data-[collapsible=icon]:hidden">
            <div className="bg-card border-border flex flex-col gap-2 border px-3 py-2.5">
              {cardContent}
            </div>
          </li>
        )}

        <SidebarDivider className="mb-2 group-data-[collapsible=icon]:hidden" />

        <SidebarEyebrow align="items">{itemsTitle}</SidebarEyebrow>

        {items.map((item) => (
          <SidebarMenuItem
            key={item.key}
            className="pl-2 group-data-[collapsible=icon]:pl-0"
          >
            <NavButton
              title={item.title}
              titleNode={item.titleNode}
              href={item.href}
              active={item.active}
              Icon={item.Icon}
              tooltip={isCollapsed ? item.title : undefined}
            />
          </SidebarMenuItem>
        ))}
      </SidebarMenu>
    </NavGroupProvider>
  );
}
