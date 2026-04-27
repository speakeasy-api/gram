import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { cn } from "@/lib/utils";
import { AppRoute } from "@/routes";
import { Loader2 } from "lucide-react";
import React from "react";
import { ProductTierBadge } from "./product-tier-badge";
import { Type } from "./ui/type";

const NAV_LOADING_DURATION_MS = 600;

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
      target={item.external ? "_blank" : undefined}
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
  const [isLoading, setIsLoading] = React.useState(false);
  const timeoutRef = React.useRef<ReturnType<typeof setTimeout> | null>(null);

  React.useEffect(() => {
    return () => {
      if (timeoutRef.current) clearTimeout(timeoutRef.current);
    };
  }, []);

  const handleClick = () => {
    onClick?.();
    if (target === "_blank") return;
    setIsLoading(true);
    if (timeoutRef.current) clearTimeout(timeoutRef.current);
    timeoutRef.current = setTimeout(
      () => setIsLoading(false),
      NAV_LOADING_DURATION_MS,
    );
  };

  return (
    <SidebarMenuButton
      className="group/nav-button"
      tooltip={title}
      href={href}
      target={target}
      isActive={active}
      onClick={handleClick}
    >
      {isLoading ? (
        <Loader2 className="trans text-muted-foreground animate-spin" />
      ) : (
        Icon && <Icon className={cn("trans text-muted-foreground")} />
      )}
      <Type
        variant="small"
        className="transition-[opacity,transform] duration-150 ease-out group-data-[collapsible=icon]:-translate-x-2 group-data-[collapsible=icon]:opacity-0"
      >
        {titleNode ?? title}
      </Type>
      {title === "Billing" && <ProductTierBadge />}
    </SidebarMenuButton>
  );
}
