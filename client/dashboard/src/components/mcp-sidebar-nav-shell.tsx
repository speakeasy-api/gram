import { NavButton, NavGroupProvider } from "@/components/nav-menu";
import { SidebarFooterAction } from "@/components/sidebar-footer-action";
import { SidebarMenu, SidebarMenuItem } from "@/components/ui/sidebar";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { ArrowLeft } from "lucide-react";
import * as React from "react";

export function McpSidebarInfoLabel({
  children,
}: {
  children: React.ReactNode;
}): React.JSX.Element {
  return (
    <Type
      variant="small"
      muted
      className="font-mono text-xs tracking-wide uppercase"
    >
      {children}
    </Type>
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
      <McpSidebarInfoLabel>{children}</McpSidebarInfoLabel>
    </li>
  );
}

export type McpSidebarNavItem = {
  key: string;
  title: string;
  titleNode?: React.ReactNode;
  Icon: React.ComponentType<{ className?: string }>;
  href: string;
  active: boolean;
};

export function McpSidebarNavShell({
  backHref,
  cardContent,
  items,
}: {
  backHref: string;
  cardContent?: React.ReactNode;
  items: McpSidebarNavItem[];
}): React.JSX.Element {
  const activeItemTitle = items.find((item) => item.active)?.title;

  return (
    <NavGroupProvider activeItem={activeItemTitle}>
      <SidebarMenu className="gap-1 px-2 group-data-[collapsible=icon]:px-0">
        <SidebarMenuItem>
          <SidebarFooterAction
            to={backHref}
            icon={ArrowLeft}
            label="Back to all servers"
          />
        </SidebarMenuItem>

        <SidebarDivider className="my-2 px-1" />

        <SidebarEyebrow align="card">At a glance</SidebarEyebrow>

        {cardContent && (
          <li className="px-2 pt-2 pb-4 group-data-[collapsible=icon]:hidden">
            <div className="bg-card border-border flex flex-col gap-3 rounded-lg border p-3 shadow-md">
              {cardContent}
            </div>
          </li>
        )}

        <SidebarDivider className="mb-2 px-1" />

        <SidebarEyebrow align="items">Configuration</SidebarEyebrow>

        {items.map((item) => (
          <SidebarMenuItem key={item.key} className="pl-2">
            <NavButton
              title={item.title}
              titleNode={item.titleNode}
              href={item.href}
              active={item.active}
              Icon={item.Icon}
            />
          </SidebarMenuItem>
        ))}
      </SidebarMenu>
    </NavGroupProvider>
  );
}
