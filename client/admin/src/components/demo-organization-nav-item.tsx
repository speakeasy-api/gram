import type { JSX } from "react";
import { CompassIcon } from "lucide-react";

import { SidebarMenuButton, SidebarMenuItem } from "@/components/ui/sidebar";

export const DEMO_ORGANIZATION_URL = "https://app.getgram.ai/explore-demo";

export function DemoOrganizationNavItem(): JSX.Element {
  return (
    <SidebarMenuItem>
      <SidebarMenuButton asChild tooltip="Demo organization">
        <a
          href={DEMO_ORGANIZATION_URL}
          target="_blank"
          rel="noopener noreferrer"
        >
          <CompassIcon />
          <span>Demo organization</span>
        </a>
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}
