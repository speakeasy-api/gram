import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { cn } from "@/lib/utils";
import { AppRoute } from "@/routes";
import { Type } from "./ui/type";

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
  const onClick = () => {
    if (item.external) {
      window.open(item.url, "_blank");
    } else {
      item.goTo();
    }
  };

  return (
    <NavButton
      title={item.title}
      onClick={onClick}
      active={item.active}
      Icon={item.Icon}
    />
  );
}

export function NavButton({
  title,
  onClick,
  active,
  Icon,
}: {
  title: string;
  onClick?: () => void;
  active?: boolean;
  Icon?: React.ComponentType<{ className?: string }>;
}) {
  return (
    <SidebarMenuButton
      className="group/nav-button"
      tooltip={title}
      onClick={onClick}
      isActive={active}
    >
      {Icon && (
        <Icon
          className={cn(
            "trans text-muted-foreground group-hover/nav-button:text-primary",
            active && "text-primary"
          )}
        />
      )}
      <Type variant="small">{title}</Type>
    </SidebarMenuButton>
  );
}
