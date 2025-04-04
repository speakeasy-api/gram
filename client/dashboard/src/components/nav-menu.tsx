import { type Icon } from "@tabler/icons-react";
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { useNavigate } from "react-router-dom";
import { cn } from "@/lib/utils";
import { AppRoute } from "@/routes";

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
          <NavButton item={item} />
        </SidebarMenuItem>
      ))}
    </SidebarMenu>
  );
}

export function NavButton({
  item,
}: {
  item: {
    title: string;
    url?: string;
    external?: boolean;
    icon?: Icon;
    active?: boolean;
    onClick?: () => void;
  };
}) {
  const navigate = useNavigate();

  const onClick =
    item.onClick ??
    (() => {
      if (item.external) {
        window.open(item.url, "_blank");
      } else if (item.url) {
        navigate(item.url);
      }
    });

  return (
    <SidebarMenuButton
      className="group/nav-button"
      tooltip={item.title}
      onClick={onClick}
      isActive={item.active}
    >
      {item.icon && (
        <item.icon
          className={cn(
            "trans text-muted-foreground group-hover/nav-button:text-primary",
            item.active && "text-primary"
          )}
        />
      )}
      <span>{item.title}</span>
    </SidebarMenuButton>
  );
}
