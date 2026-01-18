import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { cn } from "@/lib/utils";
import { AppRoute } from "@/routes";
import React from "react";
import { ProductTierBadge } from "./product-tier-badge";
import { Type } from "./ui/type";

export function NavMenu({
  items,
  className,
  children,
}: {
  items: AppRoute[];
  className?: string;
  children?: React.ReactNode;
}) {
  return (
    <SidebarMenu className={className}>
      {items.map((item) => (
        <SidebarMenuItem key={item.title}>
          <NavMenuButton item={item} />
        </SidebarMenuItem>
      ))}
      {children}
    </SidebarMenu>
  );
}

function NavMenuButton({ item }: { item: AppRoute }) {
  return (
    <NavButton
      title={item.title}
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
  target,
  active,
  Icon,
  onClick,
}: {
  title: string;
  titleNode?: React.ReactNode;
  href?: string;
  target?: string;
  onClick?: () => void;
  active?: boolean;
  Icon?: React.ComponentType<{ className?: string }>;
}) {
  return (
    <SidebarMenuButton
      className="group/nav-button"
      tooltip={title}
      href={href}
      target={target}
      isActive={active}
      onClick={onClick}
    >
      {Icon && <Icon className={cn("trans text-muted-foreground")} />}
      <Type variant="small">{titleNode ?? title}</Type>
      {title === "Billing" && <ProductTierBadge />}
    </SidebarMenuButton>
  );
}
