import * as React from "react";
import {
  IconBlocks,
  IconCirclePlusFilled,
  IconDashboard,
  IconHelp,
  IconInnerShadowTop,
  IconMessageChatbot,
  IconSearch,
  IconSettings,
  IconSparkles,
  IconTools,
} from "@tabler/icons-react";

import { NavMain } from "@/components/nav-main";
import { NavSecondary } from "@/components/nav-secondary";
import { NavUser } from "@/components/nav-user";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import Home from "@/pages/home/Home";
import Integrations from "@/pages/integrations/Integrations";
import Toolsets from "@/pages/toolsets/Toolsets";
import Sandbox from "@/pages/sandbox/Sandbox";
import { Link } from "react-router-dom";
import Onboarding from "@/pages/onboarding/Onboarding";

export const NAV_ITEMS = {
  user: {
    name: "shadcn",
    email: "m@example.com",
    avatar: "/avatars/shadcn.jpg",
  },
  primaryCTA: {
    title: "Upload OpenAPI",
    url: "/upload",
    icon: IconCirclePlusFilled,
    component: Onboarding,
  },
  navMain: [
    {
      title: "Home",
      url: "/",
      icon: IconDashboard,
      component: Home,
    },
    {
      title: "Integrations",
      url: "/integrations",
      icon: IconBlocks,
      component: Integrations,
    },
    {
      title: "Toolsets",
      url: "/toolsets",
      icon: IconTools,
      component: Toolsets,
    },
    {
      title: "Sandbox",
      url: "/sandbox",
      icon: IconMessageChatbot,
      component: Sandbox,
    },
  ],
  navSecondary: [
    {
      title: "Settings",
      url: "#",
      icon: IconSettings,
    },
    {
      title: "Get Help",
      url: "#",
      icon: IconHelp,
    },
    {
      title: "Search",
      url: "#",
      icon: IconSearch,
    },
  ],
};

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const textGradient =
    "bg-linear-to-b from-stone-300 to-transparent inline-block text-transparent bg-clip-text";

  return (
    <Sidebar collapsible="offcanvas" {...props}>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem className="group/logo">
            <SidebarMenuButton
              asChild
              className="data-[slot=sidebar-menu-button]:!p-1.5 h-12"
            >
              <Link to="/">
                <span
                  className={`font-[Mona_Sans] tracking-wide font-[1] text-3xl ${textGradient} group-hover/logo:text-stone-300 transition-all duration-300 ease-linear`}
                >
                  Gram
                </span>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        <NavMain
          primaryCTA={NAV_ITEMS.primaryCTA}
          items={NAV_ITEMS.navMain}
        />
        <NavSecondary items={NAV_ITEMS.navSecondary} className="mt-auto" />
      </SidebarContent>
      <SidebarFooter>
        <NavUser user={NAV_ITEMS.user} />
      </SidebarFooter>
    </Sidebar>
  );
}
