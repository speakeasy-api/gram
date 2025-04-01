import { IconCirclePlusFilled, type Icon } from "@tabler/icons-react";
import {
  SidebarGroup,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { useLocation, useNavigate } from "react-router-dom";
import { cn } from "@/lib/utils";

type NavItem = {
  title: string;
  url: string;
  icon?: Icon;
};

export function NavMain({
  primaryCTA,
  items,
}: {
  primaryCTA: NavItem & { icon: Icon };
  items: NavItem[];
}) {
  const navigate = useNavigate();
  const location = useLocation();

  return (
    <SidebarGroup>
      <SidebarGroupContent className="flex flex-col gap-6">
        <SidebarMenu>
          <SidebarMenuItem className="flex items-center gap-2">
            <SidebarMenuButton
              tooltip={primaryCTA.title}
              className="bg-primary text-primary-foreground hover:bg-primary/90 hover:text-primary-foreground active:bg-primary/90 active:text-primary-foreground min-w-8 transition"
              onClick={() => {
                navigate(primaryCTA.url);
              }}
            >
              <primaryCTA.icon />
              <span>{primaryCTA.title}</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
        <SidebarMenu>
          {items.map((item) => (
            <SidebarMenuItem key={item.title}>
              <SidebarMenuButton
                className="group/nav-button"
                tooltip={item.title}
                onClick={() => {
                  navigate(item.url);
                }}
                isActive={location.pathname === item.url}
              >
                {item.icon && (
                  <item.icon
                    className={cn(
                      "trans text-muted-foreground group-hover/nav-button:text-primary",
                      location.pathname === item.url && "text-primary"
                    )}
                  />
                )}
                <span>{item.title}</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
          ))}
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  );
}
