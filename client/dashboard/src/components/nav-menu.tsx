import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { cn } from "@/lib/utils";
import { AppRoute } from "@/routes";
import { Type } from "./ui/type";
import React from "react";

export function NavMenu({
  items,
  className,
}: {
  items: AppRoute[];
  className?: string;
}) {
  return (
    <SidebarMenu className={className}>
      {items.map((item) => (
        <SidebarMenuItem key={item.title}>
          <NavMenuButton item={item} />
        </SidebarMenuItem>
      ))}
    </SidebarMenu>
  );
}

function NavMenuButton({ item }: { item: AppRoute }) {
  return (
    <NavButton
      title={item.title}
      titleNode={item.titleNode}
      href={item.href()}
      active={item.active}
      Icon={item.Icon}
    />
  );
}

export function NavButton({
  title,
  titleNode,
  href,
  active,
  Icon,
  onClick,
}: {
  title: string;
  titleNode?: React.ReactNode;
  href?: string;
  onClick?: () => void;
  active?: boolean;
  Icon?: React.ComponentType<{ className?: string }>;
}) {
  return (
    <SidebarMenuButton
      className="group/nav-button"
      tooltip={title}
      href={href}
      isActive={active}
      onClick={onClick}
    >
      {Icon && <Icon className={cn("trans text-muted-foreground")} />}
      <Type variant="small">{titleNode ?? title}</Type>
    </SidebarMenuButton>
  );
}
